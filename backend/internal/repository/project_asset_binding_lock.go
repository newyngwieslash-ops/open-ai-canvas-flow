package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/kernel"
	assetrefs "yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

// WithStoryboardAssetOwners starts a fresh write transaction. Callers discover
// owners outside it, then recheck scope and authorization in the callback before
// acquiring object locks. Never enter this protocol from a row-lock transaction.
func (r *Repository) WithStoryboardAssetOwners(ctx context.Context, ownerIDs []string, fn func(*Repository) error) error {
	if _, nested := r.db.Statement.ConnPool.(gorm.TxCommitter); nested {
		return errors.New("storyboard owner locks require a new outer transaction")
	}
	owners := append([]string(nil), ownerIDs...)
	if len(owners) == 0 {
		return errors.New("storyboard owner scope is empty")
	}
	for _, owner := range owners {
		if strings.TrimSpace(owner) == "" {
			return errors.New("storyboard owner scope contains an empty owner")
		}
	}
	sort.Strings(owners)
	owners = compactStoryboardOwners(owners)
	// Bound lock waits even when an older caller has no request deadline.
	callerContext := ctx
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = r.WithContext(ctx).Transaction(func(owned *Repository) error {
			tx := owned.db
			if r.Dialect() == "sqlite" {
				if err := tx.Exec("UPDATE assets SET id = id WHERE 1 = 0").Error; err != nil {
					return err
				}
			} else if r.Dialect() == "postgres" {
				if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
					return err
				}
				for _, owner := range owners {
					sum := sha256.Sum256([]byte("storyboard-assets:" + owner))
					key := int64(binary.BigEndian.Uint64(sum[:8]))
					if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error; err != nil {
						return err
					}
				}
			} else {
				return errors.New("unsupported storyboard owner lock dialect")
			}
			return fn(owned.WithContext(context.WithValue(ctx, storyboardOwnersContextKey{}, owners)))
		})
		var expanded *storyboardOwnersExpanded
		if errors.As(err, &expanded) {
			if attempt == 2 {
				return kernel.NewAppError(409, "资产归属持续变化，请刷新后重试")
			}
			owners = append(owners, expanded.owners...)
			sort.Strings(owners)
			owners = compactStoryboardOwners(owners)
			continue
		}
		if !storyboardWriteRetryable(err) || attempt == 2 || ctx.Err() != nil {
			break
		}
		// Retry only after the complete owner transaction has rolled back, so
		// reads, lifecycle locks and receipts all use a fresh transaction.
		timer := time.NewTimer(time.Duration(attempt+1) * 15 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	if err == nil {
		return nil
	}
	if callerContext.Err() != nil {
		return callerContext.Err()
	}
	if ctx.Err() != nil {
		busy := kernel.WrapAppError(503, "资产正在更新，请稍后重试", err)
		busy.Retryable = true
		return busy
	}
	if storyboardWriteRetryable(err) {
		busy := kernel.WrapAppError(503, "资产正在更新，请稍后重试", err)
		busy.Retryable = true
		return busy
	}
	return err
}

func storyboardWriteRetryable(err error) bool {
	var pg *pgconn.PgError
	var sqlite sqlite3.Error
	return (errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "40P01" || pg.Code == "40001")) ||
		(errors.As(err, &sqlite) && (sqlite.Code == sqlite3.ErrBusy || sqlite.Code == sqlite3.ErrLocked))
}

// WithStoryboardAssetWrite coordinates a shared asset's real owners before the
// existing version/receipt transaction. Network and model work stays outside.
func (r *Repository) WithStoryboardAssetWrite(projectID, assetID string, fn func(*Repository) error) error {
	discover := func(repo *Repository) ([]string, error) {
		var asset model.Asset
		if err := repo.db.First(&asset, "id = ?", assetID).Error; err != nil {
			return nil, err
		}
		owners := []string{asset.UserID}
		var projects []model.Project
		links := repo.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id = ?", assetID)
		if err := repo.db.Where("id = ? OR id IN (?)", projectID, links).Order("id").Find(&projects).Error; err != nil {
			return nil, err
		}
		for _, project := range projects {
			owners = append(owners, project.UserID)
		}
		return owners, nil
	}
	owners, err := discover(r)
	if err != nil {
		return err
	}
	apply := func(tx *Repository) error {
		current, err := discover(tx)
		if err != nil {
			return err
		}
		if err := tx.RequireStoryboardAssetOwners(current); err != nil {
			return err
		}
		if err := lockResourceLifecycle(tx.db); err != nil { return err }
		if err := tx.lockStoryboardAssetProjects(projectID, assetID); err != nil { return err }
		return fn(tx)
	}
	if _, ok := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string); ok {
		return apply(r)
	}
	return r.WithStoryboardAssetOwners(r.db.Statement.Context, owners, apply)
}

// withStoryboardResourceWrite fences deletion admission before the existing
// lifecycle transaction. Resource ownership is read from the database, never
// inferred from the administrator or a stale resource supplied by a caller.
func (r *Repository) withStoryboardResourceWrite(assetIDs, resourceIDs []string, fn func(*Repository) error) error {
	discover := func(repo *Repository) ([]string, error) {
		wanted := map[string]struct{}{}
		for _, id := range resourceIDs { wanted[id] = struct{}{} }
		if len(assetIDs) > 0 {
			var assets []model.Asset
			if err := repo.db.Where("id IN ?", assetIDs).Find(&assets).Error; err != nil { return nil, err }
			for _, asset := range assets {
				if err := assetrefs.CollectOwnedDocumentReferences(asset.PayloadJSON, wanted); err != nil { return nil, err }
			}
			versions, representations, err := repo.AssetsResourceRecords(assetIDs)
			if err != nil { return nil, err }
			for _, version := range versions {
				if err := assetrefs.CollectOwnedDocumentReferences(version.DefinitionJSON, wanted); err != nil { return nil, err }
			}
			for _, representation := range representations {
				if representation.ResourceID != "" { wanted[representation.ResourceID] = struct{}{} }
				if err := assetrefs.CollectOwnedDocumentReferences(representation.MetadataJSON, wanted); err != nil { return nil, err }
			}
		}
		var resources []model.Resource
		if len(wanted) > 0 {
			if err := repo.db.Where("id IN ?", assetrefs.SortedIDs(wanted)).Find(&resources).Error; err != nil {
				return nil, err
			}
		}
		owners := []string{}
		for _, resource := range resources {
			owners = append(owners, resource.UserID)
			// Historical resource rows can share one physical object across owners.
			keys := []string{}
			if resource.ObjectKey != "" {
				keys = append(keys, resource.ObjectKey)
			}
			if resource.PlaybackObjectKey != "" {
				keys = append(keys, resource.PlaybackObjectKey)
			}
			if len(keys) > 0 {
				var shared []model.Resource
				if err := repo.db.Where("endpoint = ? AND bucket = ? AND (object_key IN ? OR playback_object_key IN ?)", resource.Endpoint, resource.Bucket, keys, keys).Find(&shared).Error; err != nil {
					return nil, err
				}
				for _, row := range shared {
					if deletionProvider(row.Provider) == deletionProvider(resource.Provider) {
						owners = append(owners, row.UserID)
					}
				}
			}
		}
		var assets []model.Asset
		if len(assetIDs) > 0 {
			if err := repo.db.Where("id IN ?", assetIDs).Find(&assets).Error; err != nil {
				return nil, err
			}
			for _, asset := range assets {
				owners = append(owners, asset.UserID)
			}
		}
		if len(owners) == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		// Resource references also live inside definitions/metadata. Coordinate all
		// linked projects of affected resource owners, not a guessed JSON subset.
		var projects []model.Project
		affectedAssets := repo.db.Model(&model.Asset{}).Select("id").Where("user_id IN ?", owners)
		links := repo.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id IN (?)", affectedAssets)
		if err := repo.db.Where("id IN (?)", links).Find(&projects).Error; err != nil {
			return nil, err
		}
		for _, project := range projects {
			owners = append(owners, project.UserID)
		}
		return owners, nil
	}
	if len(assetIDs) == 0 && len(resourceIDs) == 0 {
		return fn(r)
	}
	owners, err := discover(r)
	if err != nil {
		return err
	}
	apply := func(tx *Repository) error {
		current, err := discover(tx)
		if err != nil {
			return err
		}
		if err := tx.RequireStoryboardAssetOwners(current); err != nil {
			return err
		}
		return fn(tx)
	}
	if _, ok := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string); ok {
		return apply(r)
	}
	return r.WithStoryboardAssetOwners(r.db.Statement.Context, owners, apply)
}

// WithStoryboardUserAssetsWrite coordinates owner locks before transactions that
// register or replace user assets and optional project links.
func (r *Repository) WithStoryboardUserAssetsWrite(userID string, fn func(*Repository) error) error {
	return r.withStoryboardUserAssetsWrite(userID, fn)
}

// withStoryboardUserAssetsWrite covers collection replacement and new assets,
// including links that appear after the initial owner discovery.
func (r *Repository) withStoryboardUserAssetsWrite(userID string, fn func(*Repository) error) error {
	discover := func(repo *Repository) ([]string, error) {
		owners := []string{userID}
		var projects []model.Project
		assets := repo.db.Model(&model.Asset{}).Select("id").Where("user_id = ?", userID)
		links := repo.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id IN (?)", assets)
		if err := repo.db.Where("id IN (?)", links).Find(&projects).Error; err != nil {
			return nil, err
		}
		for _, project := range projects {
			owners = append(owners, project.UserID)
		}
		return owners, nil
	}
	owners, err := discover(r)
	if err != nil {
		return err
	}
	apply := func(tx *Repository) error {
		current, err := discover(tx)
		if err != nil {
			return err
		}
		if err := tx.RequireStoryboardAssetOwners(current); err != nil {
			return err
		}
		return fn(tx)
	}
	if _, ok := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string); ok {
		return apply(r)
	}
	return r.WithStoryboardAssetOwners(r.db.Statement.Context, owners, apply)
}

// withStoryboardDeletionJobWrite discovers the retained intent before taking
// lifecycle/lease locks. The worker's physical I/O remains outside this callback.
func (r *Repository) withStoryboardDeletionJobWrite(job *model.ResourceDeletionJob, fn func(*Repository) error) error {
	discover := func(repo *Repository) (*model.ResourceDeletionIntent, error) {
		var object model.ResourceDeletionObject
		if err := repo.db.First(&object, "id = ?", job.ID).Error; err != nil {
			return nil, err
		}
		var intent model.ResourceDeletionIntent
		if err := repo.db.First(&intent, "id = ?", object.IntentID).Error; err != nil {
			return nil, err
		}
		return &intent, nil
	}
	intent, err := discover(r)
	if err != nil {
		return err
	}
	assetIDs, err := deletionIDs(intent.AssetIDsJSON)
	if err != nil {
		return err
	}
	resourceIDs, err := deletionIDs(intent.ResourceIDsJSON)
	if err != nil {
		return err
	}
	return r.withStoryboardResourceWrite(assetIDs, resourceIDs, func(owned *Repository) error {
		current, err := discover(owned)
		if err != nil {
			return err
		}
		if current.ID != intent.ID || current.UserID != intent.UserID || current.AssetIDsJSON != intent.AssetIDsJSON || current.ResourceIDsJSON != intent.ResourceIDsJSON {
			return ErrResourceDeletionIdentityChanged
		}
		return fn(owned)
	})
}

// WithStoryboardProjectWrite keeps service authorization and read/modify/write
// inside the project owner's transaction, preserving the repository context.
func (r *Repository) WithStoryboardProjectWrite(projectID string, fn func(*Repository) error) error {
	return r.withStoryboardProjectWrite(projectID, func(db *gorm.DB) error { return fn(New(db)) })
}

// withStoryboardProjectWrite discovers ownership without locks, then rechecks it
// inside the owner transaction. It also joins an already coordinated caller.
func (r *Repository) withStoryboardProjectWrite(projectID string, fn func(*gorm.DB) error) error {
	var before model.Project
	if err := r.db.First(&before, "id = ?", projectID).Error; err != nil {
		return err
	}
	apply := func(tx *Repository) error {
		var current model.Project
		if err := tx.db.First(&current, "id = ?", projectID).Error; err != nil {
			return err
		}
		if err := tx.RequireStoryboardAssetOwners([]string{current.UserID}); err != nil {
			return err
		}
		if current.UserID != before.UserID {
			return kernel.NewAppError(409, "项目归属已变化，请刷新后重试")
		}
		return fn(tx.db)
	}
	if _, ok := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string); ok {
		return apply(r)
	}
	return r.WithStoryboardAssetOwners(r.db.Statement.Context, []string{before.UserID}, apply)
}

func (r *Repository) lockStoryboardAssetProjects(projectID, assetID string) error {
	var projects []model.Project
	links := r.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id = ?", assetID)
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? OR id IN (?)", projectID, links).Order("id").Find(&projects).Error; err != nil { return err }
	var lockedLinks []model.ProjectAssetLink
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("asset_id = ?", assetID).Order("id").Find(&lockedLinks).Error; err != nil { return err }
	var assets []model.Asset
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", assetID).Order("id").Find(&assets).Error; err != nil { return err }
	var versions []model.AssetVersion
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("asset_id = ?", assetID).Order("id").Find(&versions).Error; err != nil { return err }
	ids := []string{}
	for _, version := range versions { ids = append(ids, version.ID) }
	var representations []model.AssetRepresentation
	return r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("asset_version_id IN ?", ids).Order("id").Find(&representations).Error
}

// lockStoryboardShotRows reserves only the affected unit/shot set and its
// referenced asset/media closure. Owner expansion always escapes to the outer
// transaction before lifecycle or row locks are acquired.
func (r *Repository) lockStoryboardShotRows(projectID, unitID string, shotIDs, versionIDs, resourceIDs []string, incoming ...string) error {
	var shots []model.Shot
	query := r.db.Where("project_id = ?", projectID)
	wholeProject := shotIDs == nil && unitID == ""
	if shotIDs == nil { if unitID != "" { query = query.Where("unit_id = ?", unitID) } } else { query = query.Where("id IN ?", shotIDs) }
	if err := query.Order("id").Find(&shots).Error; err != nil { return err }
	units := []string{}
	if unitID != "" { units = append(units, unitID) }
	if wholeProject {
		if err := r.db.Model(&model.ProjectUnit{}).Where("project_id = ?", projectID).Pluck("id", &units).Error; err != nil { return err }
		linkedAssets := r.db.Model(&model.ProjectAssetLink{}).Select("asset_id").Where("project_id = ?", projectID)
		if err := r.db.Model(&model.AssetVersion{}).Where("asset_id IN (?)", linkedAssets).Pluck("id", &versionIDs).Error; err != nil { return err }
	}
	for _, shot := range shots { shotIDs = append(shotIDs, shot.ID); units = append(units, shot.UnitID) }
	var refs []model.ShotAssetReference
	if err := r.db.Where("shot_id IN ?", shotIDs).Find(&refs).Error; err != nil { return err }
	for _, ref := range refs { versionIDs = append(versionIDs, ref.AssetVersionID) }
	var versions []model.AssetVersion
	if err := r.db.Where("id IN ?", versionIDs).Find(&versions).Error; err != nil { return err }
	assetIDs := []string{}
	if wholeProject { if err := r.db.Model(&model.ProjectAssetLink{}).Where("project_id = ?", projectID).Pluck("asset_id", &assetIDs).Error; err != nil { return err } }
	wanted := map[string]struct{}{}
	for _, document := range incoming { if err := assetrefs.CollectOwnedDocumentReferences(document, wanted); err != nil { return err } }
	for _, id := range resourceIDs { if id != "" { wanted[id] = struct{}{} } }
	for _, version := range versions {
		assetIDs = append(assetIDs, version.AssetID)
		if err := assetrefs.CollectOwnedDocumentReferences(version.DefinitionJSON, wanted); err != nil { return err }
	}
	var representations []model.AssetRepresentation
	if err := r.db.Where("asset_version_id IN ?", versionIDs).Find(&representations).Error; err != nil { return err }
	for _, rep := range representations {
		if rep.ResourceID != "" { wanted[rep.ResourceID] = struct{}{} }
		if err := assetrefs.CollectOwnedDocumentReferences(rep.MetadataJSON, wanted); err != nil { return err }
	}
	var artifacts []model.ShotArtifact
	if err := r.db.Where("shot_id IN ?", shotIDs).Find(&artifacts).Error; err != nil { return err }
	for _, artifact := range artifacts {
		if artifact.ResourceID != "" { wanted[artifact.ResourceID] = struct{}{} }
		if err := assetrefs.CollectOwnedDocumentReferences(artifact.MetadataJSON, wanted); err != nil { return err }
	}
	var assets []model.Asset
	if err := r.db.Where("id IN ?", assetIDs).Find(&assets).Error; err != nil { return err }
	owners := []string{}
	for _, asset := range assets {
		owners = append(owners, asset.UserID)
		if err := assetrefs.CollectOwnedDocumentReferences(asset.PayloadJSON, wanted); err != nil { return err }
	}
	resourceIDs = assetrefs.SortedIDs(wanted)
	var resources []model.Resource
	if err := r.db.Where("id IN ?", resourceIDs).Find(&resources).Error; err != nil { return err }
	for _, resource := range resources { owners = append(owners, resource.UserID) }
	var projects []model.Project
	linkedProjects := r.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id IN ?", assetIDs)
	if err := r.db.Where("id = ? OR id IN (?)", projectID, linkedProjects).Order("id").Find(&projects).Error; err != nil { return err }
	projectIDs := []string{}
	for _, project := range projects { projectIDs = append(projectIDs, project.ID); owners = append(owners, project.UserID) }
	if len(owners)>0 { if err := r.RequireStoryboardAssetOwners(owners); err != nil { return err } }
	if err := lockResourceLifecycle(r.db); err != nil { return err }
	lock := func(rows any, condition string, ids any) error {
		return r.db.Clauses(clause.Locking{Strength:"UPDATE"}).Where(condition,ids).Order("id").Find(rows).Error
	}
	if err:=lock(&projects,"id IN ?",projectIDs);err!=nil{return err}
	if err:=lock(&[]model.ProjectUnit{},"id IN ?",units);err!=nil{return err}
	if err:=lock(&[]model.Shot{},"id IN ?",shotIDs);err!=nil{return err}
	if err:=r.db.Clauses(clause.Locking{Strength:"UPDATE"}).Where("project_id IN ? AND asset_id IN ?",projectIDs,assetIDs).Order("id").Find(&[]model.ProjectAssetLink{}).Error;err!=nil{return err}
	if err:=lock(&assets,"id IN ?",assetIDs);err!=nil{return err}
	if err:=lock(&versions,"id IN ?",versionIDs);err!=nil{return err}
	if err:=lock(&[]model.ShotRevision{},"shot_id IN ?",shotIDs);err!=nil{return err}
	if err:=lock(&representations,"asset_version_id IN ?",versionIDs);err!=nil{return err}
	if err:=lock(&resources,"id IN ?",resourceIDs);err!=nil{return err}
	if err:=lock(&refs,"shot_id IN ?",shotIDs);err!=nil{return err}
	return lock(&artifacts,"shot_id IN ?",shotIDs)
}

func (r *Repository) storyboardShotProject(shotID string) (string, error) {
	var shot model.Shot
	if err := r.db.Select("project_id").First(&shot, "id = ?", shotID).Error; err != nil {
		return "", err
	}
	return shot.ProjectID, nil
}

type storyboardOwnersContextKey struct{}
type storyboardOwnersExpanded struct{ owners []string }

func (*storyboardOwnersExpanded) Error() string { return "storyboard owner scope expanded" }

// RequireStoryboardAssetOwners is called after re-discovering database owners,
// before object locks or writes. Expansion aborts the entire transaction; it
// never acquires additional owner locks while row locks might be held.
func (r *Repository) RequireStoryboardAssetOwners(ownerIDs []string) error {
	held, ok := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string)
	if !ok {
		return errors.New("storyboard owner recheck requires an owner transaction")
	}
	if len(ownerIDs) == 0 {
		return errors.New("storyboard owner recheck is empty")
	}
	missing := []string{}
	for _, owner := range ownerIDs {
		if strings.TrimSpace(owner) == "" {
			return errors.New("storyboard owner recheck contains an empty owner")
		}
		i := sort.SearchStrings(held, owner)
		if i == len(held) || held[i] != owner {
			missing = append(missing, owner)
		}
	}
	if len(missing) > 0 {
		return &storyboardOwnersExpanded{owners: missing}
	}
	return nil
}

func compactStoryboardOwners(owners []string) []string {
	out := owners[:0]
	for _, owner := range owners {
		if len(out) == 0 || owner != out[len(out)-1] {
			out = append(out, owner)
		}
	}
	return out
}
