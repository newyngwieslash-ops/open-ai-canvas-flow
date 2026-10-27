package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	assetrefs "yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

// WithStoryboardAssetSnapshot never enters the owner/write transaction protocol.
func (r *Repository) WithStoryboardAssetSnapshot(ctx context.Context, fn func(*Repository) error) error {
	if r.Dialect() != "sqlite" {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(New(tx)) }, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	}
	// go-sqlite3 ignores TxOptions.ReadOnly and may configure BEGIN IMMEDIATE.
	// Pin one connection, enforce query_only and explicitly begin a deferred read.
	return r.db.WithContext(ctx).Connection(func(db *gorm.DB) (err error) {
		conn, ok := db.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return errors.New("storyboard snapshot requires a dedicated SQLite connection")
		}
		var previous int
		if err = conn.QueryRowContext(ctx, "PRAGMA query_only").Scan(&previous); err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
			return err
		}
		active := false
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var cleanupErr error
			if active {
				_, cleanupErr = conn.ExecContext(cleanup, "ROLLBACK")
			}
			restore := "PRAGMA query_only = OFF"
			if previous != 0 {
				restore = "PRAGMA query_only = ON"
			}
			_, restoreErr := conn.ExecContext(cleanup, restore)
			if cleanupErr != nil || restoreErr != nil {
				// Do not return a connection with a leaked transaction/pragma to the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				err = errors.Join(err, cleanupErr, restoreErr)
			}
		}()
		if _, err = conn.ExecContext(ctx, "BEGIN DEFERRED"); err != nil {
			return err
		}
		active = true
		if err = fn(New(db.Session(&gorm.Session{NewDB: true}))); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "COMMIT"); err == nil {
			active = false
		}
		return err
	})
}

type StoryboardAssetSnapshot struct {
	Shots           []model.Shot
	Revisions       []model.ShotRevision
	References      []model.ShotAssetReference
	Artifacts       []model.ShotArtifact
	Links           []model.ProjectAssetLink
	Assets          []model.Asset
	Versions        []model.AssetVersion
	Representations []model.AssetRepresentation
	Resources       []model.Resource
}

// ReadStoryboardAssetSnapshot must be called on the read-transaction repository.
// Keep all versions and referenced assets, not just matched/current candidates.
func (r *Repository) ReadStoryboardAssetSnapshot(projectID, unitID string, shotIDs []string) (StoryboardAssetSnapshot, error) {
	var out StoryboardAssetSnapshot
	if err := r.db.Where("project_id = ? AND unit_id = ? AND id IN ?", projectID, unitID, shotIDs).Order("id").Find(&out.Shots).Error; err != nil {
		return out, err
	}
	if len(out.Shots) != len(shotIDs) {
		return out, gorm.ErrRecordNotFound
	}
	if err := r.db.Where("shot_id IN ?", shotIDs).Order("id").Find(&out.References).Error; err != nil {
		return out, err
	}
	if err := r.db.Where("shot_id IN ?", shotIDs).Order("id").Find(&out.Artifacts).Error; err != nil {
		return out, err
	}
	revisionIDs := []string{}
	for _, shot := range out.Shots {
		if shot.CurrentRevisionID != "" {
			revisionIDs = append(revisionIDs, shot.CurrentRevisionID)
		}
	}
	if err := r.db.Where("id IN ?", revisionIDs).Order("id").Find(&out.Revisions).Error; err != nil {
		return out, err
	}
	if err := r.db.Where("project_id = ?", projectID).Order("id").Find(&out.Links).Error; err != nil {
		return out, err
	}
	assetIDs := []string{}
	for _, link := range out.Links {
		assetIDs = append(assetIDs, link.AssetID)
	}
	versionIDs := []string{}
	for _, ref := range out.References {
		versionIDs = append(versionIDs, ref.AssetVersionID)
	}
	// Existing references may outlive a project link; their versions/media still
	// participate in the baseline but are never promoted into the allowed catalog.
	referencedAssets := r.db.Model(&model.AssetVersion{}).Select("asset_id").Where("id IN ?", versionIDs)
	if err := r.db.Where("id IN ? OR id IN (?)", assetIDs, referencedAssets).Order("id").Find(&out.Assets).Error; err != nil {
		return out, err
	}
	for _, asset := range out.Assets {
		assetIDs = append(assetIDs, asset.ID)
		if asset.PrimaryVersionID != "" {
			versionIDs = append(versionIDs, asset.PrimaryVersionID)
		}
	}
	if err := r.db.Where("asset_id IN ? OR id IN ?", assetIDs, versionIDs).Order("id").Find(&out.Versions).Error; err != nil {
		return out, err
	}
	versionIDs = []string{}
	resourceIDs := []string{}
	for _, version := range out.Versions {
		versionIDs = append(versionIDs, version.ID)
		var definition struct {
			ReferenceImage *struct {
				ResourceID string `json:"resourceId"`
			} `json:"referenceImage"`
		}
		if json.Unmarshal([]byte(version.DefinitionJSON), &definition) == nil && definition.ReferenceImage != nil {
			resourceIDs = append(resourceIDs, definition.ReferenceImage.ResourceID)
		}
	}
	if err := r.db.Where("asset_version_id IN ?", versionIDs).Order("id").Find(&out.Representations).Error; err != nil {
		return out, err
	}
	for _, representation := range out.Representations {
		resourceIDs = append(resourceIDs, representation.ResourceID)
	}
	if err := r.db.Where("id IN ?", resourceIDs).Order("id").Find(&out.Resources).Error; err != nil {
		return out, err
	}
	return out, nil
}

type storyboardBindingAppendClosure struct {
	owners          []string
	projectIDs      []string
	units           []string
	shotIDs         []string
	assetIDs        []string
	versionIDs      []string
	resourceIDs     []string
	projects        []model.Project
	assets          []model.Asset
	versions        []model.AssetVersion
	representations []model.AssetRepresentation
	resources       []model.Resource
	refs            []model.ShotAssetReference
	artifacts       []model.ShotArtifact
}

func (r *Repository) discoverStoryboardBindingAppendClosure(projectID, unitID string, shotIDs []string) (storyboardBindingAppendClosure, error) {
	var out storyboardBindingAppendClosure
	if unitID != "" {
		out.units = append(out.units, unitID)
	}
	if len(shotIDs) > 0 {
		var shots []model.Shot
		if err := r.db.Where("project_id = ? AND id IN ?", projectID, shotIDs).Order("id").Find(&shots).Error; err != nil {
			return out, err
		}
		for _, shot := range shots {
			out.shotIDs = append(out.shotIDs, shot.ID)
			if shot.UnitID != "" {
				out.units = append(out.units, shot.UnitID)
			}
		}
	}
	var linkedAssetIDs []string
	if err := r.db.Model(&model.ProjectAssetLink{}).Where("project_id = ?", projectID).Order("id").Pluck("asset_id", &linkedAssetIDs).Error; err != nil {
		return out, err
	}
	out.assetIDs = append(out.assetIDs, linkedAssetIDs...)
	refVersionIDs := []string{}
	if len(out.shotIDs) > 0 {
		if err := r.db.Where("shot_id IN ?", out.shotIDs).Order("id").Find(&out.refs).Error; err != nil {
			return out, err
		}
		for _, ref := range out.refs {
			if ref.AssetVersionID != "" {
				refVersionIDs = append(refVersionIDs, ref.AssetVersionID)
			}
		}
	}
	if len(linkedAssetIDs) > 0 || len(refVersionIDs) > 0 {
		if err := r.db.Where("asset_id IN ? OR id IN ?", linkedAssetIDs, refVersionIDs).Order("id").Find(&out.versions).Error; err != nil {
			return out, err
		}
	}
	wanted := map[string]struct{}{}
	for _, version := range out.versions {
		out.versionIDs = append(out.versionIDs, version.ID)
		out.assetIDs = append(out.assetIDs, version.AssetID)
		if err := assetrefs.CollectOwnedDocumentReferences(version.DefinitionJSON, wanted); err != nil {
			return out, err
		}
	}
	if len(out.versionIDs) > 0 {
		if err := r.db.Where("asset_version_id IN ?", out.versionIDs).Order("id").Find(&out.representations).Error; err != nil {
			return out, err
		}
	}
	for _, rep := range out.representations {
		if rep.ResourceID != "" {
			wanted[rep.ResourceID] = struct{}{}
		}
		if err := assetrefs.CollectOwnedDocumentReferences(rep.MetadataJSON, wanted); err != nil {
			return out, err
		}
	}
	if len(out.shotIDs) > 0 {
		if err := r.db.Where("shot_id IN ?", out.shotIDs).Order("id").Find(&out.artifacts).Error; err != nil {
			return out, err
		}
	}
	for _, artifact := range out.artifacts {
		if artifact.ResourceID != "" {
			wanted[artifact.ResourceID] = struct{}{}
		}
		if err := assetrefs.CollectOwnedDocumentReferences(artifact.MetadataJSON, wanted); err != nil {
			return out, err
		}
	}
	if len(out.assetIDs) > 0 {
		if err := r.db.Where("id IN ?", out.assetIDs).Order("id").Find(&out.assets).Error; err != nil {
			return out, err
		}
	}
	for _, asset := range out.assets {
		out.owners = append(out.owners, asset.UserID)
		if err := assetrefs.CollectOwnedDocumentReferences(asset.PayloadJSON, wanted); err != nil {
			return out, err
		}
	}
	out.resourceIDs = assetrefs.SortedIDs(wanted)
	if len(out.resourceIDs) > 0 {
		if err := r.db.Where("id IN ?", out.resourceIDs).Order("id").Find(&out.resources).Error; err != nil {
			return out, err
		}
	}
	for _, resource := range out.resources {
		out.owners = append(out.owners, resource.UserID)
	}
	linkedProjects := r.db.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id IN ?", out.assetIDs)
	if err := r.db.Where("id = ? OR id IN (?)", projectID, linkedProjects).Order("id").Find(&out.projects).Error; err != nil {
		return out, err
	}
	for _, project := range out.projects {
		out.projectIDs = append(out.projectIDs, project.ID)
		if project.UserID != "" {
			out.owners = append(out.owners, project.UserID)
		}
	}
	return out, nil
}

func (r *Repository) DiscoverStoryboardBindingAppendOwners(projectID string, shotIDs, _ []string) ([]string, error) {
	closure, err := r.discoverStoryboardBindingAppendClosure(projectID, "", shotIDs)
	if err != nil {
		return nil, err
	}
	return closure.owners, nil
}

func (r *Repository) LockStoryboardBindingAppendScope(projectID, unitID string, shotIDs, _ []string) error {
	closure, err := r.discoverStoryboardBindingAppendClosure(projectID, unitID, shotIDs)
	if err != nil {
		return err
	}
	if len(closure.owners) > 0 {
		if err := r.RequireStoryboardAssetOwners(closure.owners); err != nil {
			return err
		}
	}
	if err := lockResourceLifecycle(r.db); err != nil {
		return err
	}
	lock := func(rows any, condition string, ids any) error {
		return r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where(condition, ids).Order("id").Find(rows).Error
	}
	if err := lock(&closure.projects, "id IN ?", closure.projectIDs); err != nil {
		return err
	}
	if err := lock(&[]model.ProjectUnit{}, "id IN ?", closure.units); err != nil {
		return err
	}
	if err := lock(&[]model.Shot{}, "id IN ?", closure.shotIDs); err != nil {
		return err
	}
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id IN ? AND asset_id IN ?", closure.projectIDs, closure.assetIDs).Order("id").Find(&[]model.ProjectAssetLink{}).Error; err != nil {
		return err
	}
	if err := lock(&closure.assets, "id IN ?", closure.assetIDs); err != nil {
		return err
	}
	if err := lock(&closure.versions, "id IN ?", closure.versionIDs); err != nil {
		return err
	}
	if err := lock(&[]model.ShotRevision{}, "shot_id IN ?", closure.shotIDs); err != nil {
		return err
	}
	if err := lock(&closure.representations, "asset_version_id IN ?", closure.versionIDs); err != nil {
		return err
	}
	if err := lock(&closure.resources, "id IN ?", closure.resourceIDs); err != nil {
		return err
	}
	if err := lock(&closure.refs, "shot_id IN ?", closure.shotIDs); err != nil {
		return err
	}
	return lock(&closure.artifacts, "shot_id IN ?", closure.shotIDs)
}

func (r *Repository) FindShotsByIDs(shotIDs []string) ([]model.Shot, error) {
	if len(shotIDs) == 0 {
		return nil, nil
	}
	var shots []model.Shot
	err := r.db.Where("id IN ?", shotIDs).Order("id").Find(&shots).Error
	return shots, err
}

func (r *Repository) FindAssetVersionsByIDs(versionIDs []string) ([]model.AssetVersion, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var versions []model.AssetVersion
	err := r.db.Where("id IN ?", versionIDs).Order("id").Find(&versions).Error
	return versions, err
}

func (r *Repository) FindAssetsByIDs(assetIDs []string) ([]model.Asset, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	var assets []model.Asset
	err := r.db.Where("id IN ?", assetIDs).Order("id").Find(&assets).Error
	return assets, err
}

func (r *Repository) RequireAssetVersionReferences(version *model.AssetVersion) error {
	return r.requireAssetVersionReferences(version)
}

func (r *Repository) ApplyStoryboardShotAssetBindingsAppend(projectID, unitID string, expectedRevision int64, references []model.ShotAssetReference, affectedShotIDs []string, updatedAt time.Time) (bool, error) {
	for i := range references {
		if err := r.db.Create(&references[i]).Error; err != nil {
			return false, err
		}
	}
	if len(affectedShotIDs) > 0 {
		if err := r.db.Model(&model.ShotArtifact{}).
			Where("shot_id IN ? AND status != ?", affectedShotIDs, "failed").
			Updates(map[string]any{"status": "stale", "selected": false, "updated_at": updatedAt}).Error; err != nil {
			return false, err
		}
		if err := r.db.Model(&model.ShotArtifact{}).
			Where("shot_id IN ? AND status = ? AND selected = ?", affectedShotIDs, "failed", true).
			Updates(map[string]any{"selected": false, "updated_at": updatedAt}).Error; err != nil {
			return false, err
		}
	}
	if err := invalidateUnitWorkflowTx(r.db, projectID, unitID, "storyboard", updatedAt); err != nil {
		return false, err
	}
	res := r.db.Model(&model.Project{}).
		Where("id = ? AND revision = ?", projectID, expectedRevision).
		Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": updatedAt})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
