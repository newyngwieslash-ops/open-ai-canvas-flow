package repository

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	assetrefs "yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

var ErrResourceDeleting = errors.New("resource deletion is in progress")
var ErrResourceDeletionLeaseLost = errors.New("resource deletion lease lost")
var ErrResourceDeletionIdentityChanged = errors.New("resource deletion identity changed")

// ResourceDeletionReferencedError retains sources for every entry point, including
// a worker discovering a reference after admission. It never clears that reference.
type ResourceDeletionReferencedError struct{ References []ResourceDirectReference }

func (e *ResourceDeletionReferencedError) Error() string {
	labels := make([]string, 0, len(e.References))
	for _, ref := range e.References {
		labels = append(labels, fmt.Sprintf("%s「%s」", ref.Kind, ref.Title))
	}
	return "资源仍被引用：" + strings.Join(labels, "、")
}

func deletionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func deletionJSON(ids []string) string { b, _ := json.Marshal(ids); return string(b) }
func deletionIDs(raw string) ([]string, error) {
	var ids []string
	err := json.Unmarshal([]byte(raw), &ids)
	return ids, err
}
func deletionProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "local"
	}
	return provider
}
func deletionIdentity(provider, endpoint, bucket, key string) string {
	return strings.Join([]string{deletionProvider(provider), endpoint, bucket, key}, "\x00")
}

// All lifecycle writers take this short database lock before reading. The durable
// object fence, rather than this transaction, protects the subsequent network I/O.
func lockResourceLifecycle(tx *gorm.DB) error {
	if tx.Dialector.Name() == "sqlite" {
		return tx.Exec("UPDATE resources SET status = status WHERE 1 = 0").Error
	}
	if tx.Dialector.Name() == "postgres" {
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", "resource-lifecycle").Error
	}
	return gorm.ErrInvalidData
}
func (r *Repository) ResourceLifecycleTransaction(fn func(*Repository) error) error {
	return r.resourceLifecycleTransaction(fn)
}

func (r *Repository) resourceLifecycleTransaction(fn func(*Repository) error) error {
	// A nested savepoint cannot refresh the owning transaction's snapshot or
	// recover an aborted PostgreSQL transaction. Let that owner retry everything.
	if _, nested := r.db.Statement.ConnPool.(gorm.TxCommitter); nested {
		if err := lockResourceLifecycle(r.db); err != nil {
			return err
		}
		return fn(r)
	}
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		err = r.Transaction(func(tx *Repository) error {
			if err := lockResourceLifecycle(tx.db); err != nil {
				return err
			}
			return fn(tx)
		})
		if err == nil {
			return nil
		}
		text := strings.ToLower(err.Error())
		if !strings.Contains(text, "database is locked") && !strings.Contains(text, "database table is locked") && !strings.Contains(text, "busy_snapshot") && !strings.Contains(text, "sqlstate 40001") && !strings.Contains(text, "sqlstate 40p01") {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

func (r *Repository) deletionReferences(userID string, assetIDs, resourceIDs []string) error {
	if len(assetIDs) == 0 {
		if err := r.RequireNoOriginalImageReferences(resourceIDs); err != nil {
			return err
		}
	}
	refs := []ResourceDirectReference{}
	for _, id := range assetIDs {
		found, err := r.AssetBusinessReferences(userID, id)
		if err != nil {
			return err
		}
		refs = append(refs, found...)
	}
	snapshot, err := r.ResourceReferenceSnapshotExcludingAssets(userID, assetIDs, resourceIDs)
	if err != nil {
		return err
	}
	wanted := map[string]struct{}{}
	for _, id := range resourceIDs {
		wanted[id] = struct{}{}
	}
	for _, ref := range snapshot.Direct {
		if _, ok := wanted[ref.ResourceID]; ok {
			refs = append(refs, ref)
		}
	}
	for _, doc := range snapshot.Documents {
		if assetrefs.DocumentReferences(doc.PrimaryJSON, wanted) || assetrefs.DocumentReferences(doc.SecondaryJSON, wanted) {
			refs = append(refs, ResourceDirectReference{Kind: doc.Kind, ID: doc.ID, Title: doc.Title})
		}
	}
	// Site configuration is global, unlike the user's document snapshot.
	var settings []model.SystemSetting
	if err := r.db.Find(&settings).Error; err != nil {
		return err
	}
	for _, setting := range settings {
		if setting.Key == "appearance" {
			ids, err := AppearanceResourceIDs(setting.ValueJSON)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if _, ok := wanted[id]; ok {
					refs = append(refs, ResourceDirectReference{Kind: "外观", ID: setting.Key, Title: "外观配置", ResourceID: id})
				}
			}
		}
		if assetrefs.DocumentReferences(setting.ValueJSON, wanted) {
			refs = append(refs, ResourceDirectReference{Kind: "系统设置", ID: setting.Key, Title: setting.Key})
		}
	}
	if len(refs) > 0 {
		return &ResourceDeletionReferencedError{References: refs}
	}
	return nil
}

// stageResourceDeletion is called only inside resourceLifecycleTransaction.
// Caller-supplied jobs are deliberately not trusted: identities come from locked rows.
func (r *Repository) stageResourceDeletion(userID string, assetIDs, resourceIDs []string) error {
	var owned []model.Asset
	if len(assetIDs) > 0 {
		if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id IN ?", userID, assetIDs).Order("id").Find(&owned).Error; err != nil {
			return err
		}
		if len(owned) != len(assetIDs) {
			return gorm.ErrRecordNotFound
		}
		for _, asset := range owned {
			if asset.Status == model.AssetVersionStatusDeleting {
				return ErrResourceDeleting
			}
		}
	}
	if len(assetIDs) > 0 {
		// Rebuild the complete owned set under the asset lock; never trust a stale
		// service snapshot or a direct caller omitting its historical resources.
		wanted := map[string]struct{}{}
		for _, asset := range owned {
			if err := assetrefs.CollectOwnedDocumentReferences(asset.PayloadJSON, wanted); err != nil {
				return err
			}
		}
		var versions []model.AssetVersion
		if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("asset_id IN ?", assetIDs).Order("id").Find(&versions).Error; err != nil {
			return err
		}
		versionIDs := []string{}
		for _, version := range versions {
			versionIDs = append(versionIDs, version.ID)
		}
		var representations []model.AssetRepresentation
		if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("asset_version_id IN ?", versionIDs).Order("id").Find(&representations).Error; err != nil {
			return err
		}
		for _, version := range versions {
			if err := assetrefs.CollectOwnedDocumentReferences(version.DefinitionJSON, wanted); err != nil {
				return err
			}
		}
		for _, representation := range representations {
			if representation.ResourceID != "" {
				wanted[representation.ResourceID] = struct{}{}
			}
			if err := assetrefs.CollectOwnedDocumentReferences(representation.MetadataJSON, wanted); err != nil {
				return err
			}
		}
		shared, err := r.OtherAssetResourceReferences(userID, assetIDs)
		if err != nil {
			return err
		}
		for _, ref := range shared.Direct {
			delete(wanted, ref.ResourceID)
		}
		for _, doc := range shared.Documents {
			for id := range assetrefs.DocumentReferencedIDs(doc.PrimaryJSON, wanted) {
				delete(wanted, id)
			}
			for id := range assetrefs.DocumentReferencedIDs(doc.SecondaryJSON, wanted) {
				delete(wanted, id)
			}
		}
		ids := assetrefs.SortedIDs(wanted)
		ownedResources, err := r.ResourcesForUserIDs(userID, ids)
		if err != nil {
			return err
		}
		resourceIDs = nil
		for _, resource := range ownedResources {
			resourceIDs = append(resourceIDs, resource.ID)
		}
	}
	// A derived resource set can reveal an earlier physical-object owner. Abort
	// the whole outer transaction rather than adding owner locks after row locks.
	if err := r.withStoryboardResourceWrite(assetIDs, resourceIDs, func(*Repository) error { return nil }); err != nil {
		return err
	}
	var resources []model.Resource
	if len(resourceIDs) > 0 {
		if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id IN ?", userID, resourceIDs).Order("id").Find(&resources).Error; err != nil {
			return err
		}
		if len(resources) != len(resourceIDs) {
			return ErrResourceCleanupSetChanged
		}
		if err := r.recoverLocalResourceProducers(resources); err != nil {
			return err
		}
		for index := range resources {
			resource := &resources[index]
			// Legacy producers may predate claim-time reservation. Freeze the actual
			// derived output before constructing the object ledger or releasing locks.
			if deletionProvider(resource.Provider) == "local" && resource.PlaybackStatus == model.PlaybackStatusProcessing {
				key := resource.ID + ".mp4"
				if resource.PlaybackObjectKey != "" && resource.PlaybackObjectKey != key {
					return ErrResourceDeletionIdentityChanged
				}
				resource.PlaybackObjectKey = key
				if err := r.db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("playback_object_key", key).Error; err != nil {
					return err
				}
			}
			if resource.Status == model.ResourceStatusPending {
				return errors.New("upload producer has not settled; retry deletion after upload completes")
			}
			if resource.Status == model.ResourceStatusDeleting {
				return ErrResourceDeleting
			}
			selected := map[string]bool{}
			for _, id := range resourceIDs {
				selected[id] = true
			}
			if err := r.requireResourceProducers(resource, selected, userID); err != nil {
				return err
			}
		}
	}
	if err := r.deletionReferences(userID, assetIDs, resourceIDs); err != nil {
		return err
	}
	intent := model.ResourceDeletionIntent{ID: deletionID(), UserID: userID, AssetIDsJSON: deletionJSON(assetIDs), ResourceIDsJSON: deletionJSON(resourceIDs), Status: "pending"}
	if err := r.db.Create(&intent).Error; err != nil {
		return err
	}
	if len(assetIDs) > 0 {
		if err := r.db.Model(&model.Asset{}).Where("id IN ?", assetIDs).Update("status", model.AssetVersionStatusDeleting).Error; err != nil {
			return err
		}
	}
	if len(resourceIDs) > 0 {
		if err := r.db.Model(&model.Resource{}).Where("id IN ?", resourceIDs).Update("status", model.ResourceStatusDeleting).Error; err != nil {
			return err
		}
		if err := r.db.Model(&model.ResourceProducerOperation{}).Where("resource_id IN ?", resourceIDs).Update("fenced", true).Error; err != nil {
			return err
		}
	}
	type objectGroup struct {
		object   model.ResourceDeletionObject
		resource model.Resource
		ids      []string
	}
	groups := map[string]*objectGroup{}
	for _, resource := range resources {
		variants := map[string]string{"original": resource.ObjectKey}
		if deletionProvider(resource.Provider) == "local" && resource.PlaybackObjectKey != "" {
			variants["playback"] = resource.PlaybackObjectKey
		}
		for variant, key := range variants {
			if strings.TrimSpace(key) == "" {
				continue
			}
			identity := variant + "\x00" + deletionIdentity(resource.Provider, resource.Endpoint, resource.Bucket, key)
			group := groups[identity]
			if group == nil {
				group = &objectGroup{resource: resource, object: model.ResourceDeletionObject{ID: deletionID(), IntentID: intent.ID, Variant: variant, Provider: deletionProvider(resource.Provider), Endpoint: resource.Endpoint, Bucket: resource.Bucket, ObjectKey: key, Status: "pending"}}
				groups[identity] = group
			}
			group.ids = append(group.ids, resource.ID)
		}
	}
	for _, group := range groups {
		group.object.ResourceIDsJSON = deletionJSON(group.ids)
		if err := r.db.Create(&group.object).Error; err != nil {
			return err
		}
		job := model.ResourceDeletionJob{ID: group.object.ID, UserID: userID, ResourceID: group.resource.ID, Provider: group.object.Provider, Endpoint: group.object.Endpoint, Bucket: group.object.Bucket, ObjectKey: group.object.ObjectKey, StorageSettingID: group.resource.StorageSettingID, Status: model.ResourceDeletionStatusPending, NextAttemptAt: time.Now()}
		if err := r.db.Create(&job).Error; err != nil {
			return err
		}
	}
	if len(groups) == 0 {
		return r.finalizeDeletionIntent(&intent)
	}
	return nil
}

func (r *Repository) finalizeDeletionIntent(intent *model.ResourceDeletionIntent) error {
	// Admission/Finish own the outer fence; never acquire it after lifecycle locks.
	if err := r.RequireStoryboardAssetOwners([]string{intent.UserID}); err != nil {
		return err
	}
	var incomplete int64
	if err := r.db.Model(&model.ResourceDeletionObject{}).Where("intent_id = ? AND status NOT IN ?", intent.ID, []string{"completed", "retained"}).Count(&incomplete).Error; err != nil {
		return err
	}
	if incomplete > 0 {
		return nil
	}
	assetIDs, err := deletionIDs(intent.AssetIDsJSON)
	if err != nil {
		return err
	}
	resourceIDs, err := deletionIDs(intent.ResourceIDsJSON)
	if err != nil {
		return err
	}
	if err := r.requireDeletionProducerEvidence(resourceIDs); err != nil {
		return err
	}
	if err := r.deletionReferences(intent.UserID, assetIDs, resourceIDs); err != nil {
		return err
	}
	if len(assetIDs) > 0 {
		versionIDs := r.db.Model(&model.AssetVersion{}).Select("id").Where("asset_id IN ?", assetIDs)
		for _, row := range []any{&model.CharacterVoiceBinding{}, &model.AssetRepresentation{}} {
			if err := r.db.Where("asset_version_id IN (?)", versionIDs).Delete(row).Error; err != nil {
				return err
			}
		}
		if err := r.db.Where("asset_id IN ?", assetIDs).Delete(&model.AssetVersion{}).Error; err != nil {
			return err
		}
		if err := r.db.Where("id IN ? AND status = ?", assetIDs, model.AssetVersionStatusDeleting).Delete(&model.Asset{}).Error; err != nil {
			return err
		}
	}
	if len(resourceIDs) > 0 {
		if err := r.db.Where("resource_id IN ?", resourceIDs).Delete(&model.ArkPrivateAssetBinding{}).Error; err != nil {
			return err
		}
		if err := r.db.Where("id IN ? AND status = ?", resourceIDs, model.ResourceStatusDeleting).Delete(&model.Resource{}).Error; err != nil {
			return err
		}
	}
	return r.db.Model(intent).Update("status", "completed").Error
}

// PrepareResourceDeletion rechecks the complete intent, identity and alias set
// immediately before I/O. A legacy job without retained rows is never authorized.
func (r *Repository) PrepareResourceDeletion(job *model.ResourceDeletionJob) (*model.ResourceDeletionObject, bool, error) {
	var object model.ResourceDeletionObject
	retained := false
	err := r.withStoryboardDeletionJobWrite(job, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			if err := tx.requireDeletionLease(job); err != nil {
				return err
			}
			if err := tx.db.First(&object, "id = ?", job.ID).Error; err != nil {
				return err
			}
			if err := tx.requireDeletionPhysicalLock(&object); err != nil {
				return err
			}
			if deletionIdentity(job.Provider, job.Endpoint, job.Bucket, job.ObjectKey) != deletionIdentity(object.Provider, object.Endpoint, object.Bucket, object.ObjectKey) {
				return ErrResourceDeletionIdentityChanged
			}
			var intent model.ResourceDeletionIntent
			if err := tx.db.First(&intent, "id = ? AND status = ?", object.IntentID, "pending").Error; err != nil {
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
			if err := tx.requireDeletionProducerEvidence(resourceIDs); err != nil {
				return err
			}
			if err := tx.deletionReferences(intent.UserID, assetIDs, resourceIDs); err != nil {
				return err
			}
			ids, err := deletionIDs(object.ResourceIDsJSON)
			if err != nil {
				return err
			}
			var rows []model.Resource
			if err := tx.db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) != len(ids) {
				return ErrResourceDeletionIdentityChanged
			}
			for _, row := range rows {
				// No thumbnail producer/key contract exists in this schema. Never guess a
				// materialized legacy object's location or silently finalize without it.
				if row.PlaybackStatus == model.PlaybackStatusProcessing {
					return errors.New("playback producer still running; deletion retained for retry")
				}
				if row.ThumbnailStatus == "ready" || row.ThumbnailSize > 0 {
					return errors.New("thumbnail object identity unavailable; deletion retained for retry")
				}
				key := row.ObjectKey
				if object.Variant == "playback" {
					key = row.PlaybackObjectKey
				}
				if row.Status != model.ResourceStatusDeleting || deletionIdentity(row.Provider, row.Endpoint, row.Bucket, key) != deletionIdentity(object.Provider, object.Endpoint, object.Bucket, object.ObjectKey) {
					return ErrResourceDeletionIdentityChanged
				}
			}
			// Nondeleting aliases keep the object. Deleting aliases are not mutual blockers.
			var aliases []model.Resource
			if err := tx.db.Where("status <> ?", model.ResourceStatusDeleting).Find(&aliases).Error; err != nil {
				return err
			}
			candidate := model.Resource{Provider: object.Provider, Endpoint: object.Endpoint, Bucket: object.Bucket, ObjectKey: object.ObjectKey, PlaybackObjectKey: object.ObjectKey}
			identity := resourceProducerIdentity(&candidate, object.Variant).Identity
			for _, alias := range aliases {
				for _, variant := range []string{"original", "playback"} {
					other := resourceProducerIdentity(&alias, variant)
					if other.ObjectKey != "" && other.Identity == identity {
						retained = true
					}
				}
			}
			return nil
		})
	})
	return &object, retained, err
}

func (r *Repository) ResourceDeletionObject(id string) (*model.ResourceDeletionObject, error) {
	var object model.ResourceDeletionObject
	err := r.db.First(&object, "id = ?", id).Error
	return &object, err
}

func (r *Repository) requireDeletionPhysicalLock(object *model.ResourceDeletionObject) error {
	if locks := r.physicalLocks(); locks != nil && deletionProvider(object.Provider) == "local" {
		row := model.Resource{Provider: object.Provider, Endpoint: object.Endpoint, Bucket: object.Bucket, ObjectKey: object.ObjectKey, PlaybackObjectKey: object.ObjectKey}
		return locks.Validate(ResourcePhysicalIdentity(&row, object.Variant))
	}
	return nil
}
func (r *Repository) requireDeletionLease(job *model.ResourceDeletionJob) error {
	var count int64
	err := r.db.Model(&model.ResourceDeletionJob{}).Where("id = ? AND lease_owner = ? AND attempts = ? AND status = ? AND lease_expires_at > ?", job.ID, job.LeaseOwner, job.Attempts, model.ResourceDeletionStatusProcessing, time.Now()).Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrResourceDeletionLeaseLost
	}
	return nil
}
func (r *Repository) FinishResourceDeletion(job *model.ResourceDeletionJob, retained bool) error {
	return r.withStoryboardDeletionJobWrite(job, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			if err := tx.requireDeletionLease(job); err != nil {
				return err
			}
			var object model.ResourceDeletionObject
			if err := tx.db.First(&object, "id = ?", job.ID).Error; err != nil {
				return err
			}
			if err := tx.requireDeletionPhysicalLock(&object); err != nil {
				return err
			}
			ids, err := deletionIDs(object.ResourceIDsJSON)
			if err != nil {
				return err
			}
			if err := tx.requireDeletionProducerEvidence(ids); err != nil {
				return err
			}
			status := "completed"
			if retained {
				status = "retained"
			}
			if err := tx.db.Model(&object).Update("status", status).Error; err != nil {
				return err
			}
			var intent model.ResourceDeletionIntent
			if err := tx.db.First(&intent, "id = ?", object.IntentID).Error; err != nil {
				return err
			}
			if err := tx.finalizeDeletionIntent(&intent); err != nil {
				return err
			}
			if err := tx.db.Delete(&model.ResourceDeletionJob{}, "id = ?", job.ID).Error; err != nil {
				return err
			}
			// Release is part of successful completion. A release error rolls back
			// records; the already committed stage tombstone still fences writers.
			// If commit fails, a retry must acquire a fresh capability: Validate
			// rejects this closed one even when the transaction helper retries.
			return tx.physicalLocks().Close()
		})
	})
}

func (r *Repository) requireLiveDocumentResources(userID, raw string) error {
	ids := map[string]struct{}{}
	if err := assetrefs.CollectOwnedDocumentReferences(raw, ids); err != nil {
		return err
	}
	for _, id := range assetrefs.SortedIDs(ids) {
		var row model.Resource
		if err := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ? AND status = ?", id, userID, model.ResourceStatusReady).First(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) requireResourceIdentityWritable(resource *model.Resource) error {
	variants := map[string]string{"original": resource.ObjectKey, "playback": resource.PlaybackObjectKey}
	for variant, key := range variants {
		if key == "" {
			continue
		}
		{
			var producers []model.Resource
			if err := r.db.Where("id <> ? AND playback_status = ?", resource.ID, model.PlaybackStatusProcessing).Find(&producers).Error; err != nil {
				return err
			}
			for _, producer := range producers {
				producerKey := producer.PlaybackObjectKey
				if producerKey == "" {
					producerKey = producer.ID + ".mp4"
				}
				producer.PlaybackObjectKey = producerKey
				if ResourcePhysicalIdentity(&producer, "playback") == ResourcePhysicalIdentity(resource, variant) {
					return ErrResourceDeleting
				}
			}
		}
		if err := r.requirePhysicalIdentityWritable(ResourcePhysicalIdentity(resource, variant)); err != nil {
			return err
		}
	}
	return nil
}

// CompletePlaybackTranscode is the producer-only terminal write. Generic saves
// cannot clear the processing reservation; partial output keeps its reserved key.
func (r *Repository) CompletePlaybackTranscode(userID, id, status, errorText string, tokens ...*model.ResourceProducerOperation) error {
	if status != model.PlaybackStatusReady && status != model.PlaybackStatusFailed {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		if len(tokens) != 1 || tokens[0] == nil {
			return ErrResourceProducerUnsettled
		}
		var op model.ResourceProducerOperation
		if err := tx.db.First(&op, "id = ?", tokens[0].ID).Error; err != nil {
			return err
		}
		if !samePlaybackOperation(&op, tokens[0]) || op.UserID != userID || op.ResourceID != id || op.Fenced || op.PhysicalState != "settled" && status == model.PlaybackStatusReady {
			return ErrResourceProducerUnsettled
		}
		if op.PhysicalState != "settled" && op.PhysicalState != "unknown" || status == model.PlaybackStatusReady && op.Outcome != "success" {
			return ErrResourceProducerUnsettled
		}
		row, err := tx.ResourceForUser(userID, id)
		if err != nil {
			return err
		}
		if row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing || row.PlaybackObjectKey != id+".mp4" {
			return ErrResourceDeleting
		}
		if op.Identity != ResourcePhysicalIdentity(row, "playback") {
			return ErrResourceDeletionIdentityChanged
		}
		if err := tx.requireResourceIdentityWritable(row); err != nil {
			return err
		}
		result := tx.db.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ? AND playback_status = ? AND playback_object_key = ?", id, userID, model.ResourceStatusReady, model.PlaybackStatusProcessing, id+".mp4").Updates(map[string]any{"playback_status": status, "playback_error": errorText})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrResourceDeleting
		}
		return nil
	})
}

// SettleDeletingPlayback is called only after the producer has stopped writing.
// It never publishes ready or deletes bytes. The frozen playback object remains
// pending for the same alias-aware, leased deletion worker as every other object.
func (r *Repository) SettleDeletingPlayback(userID, id string) error {
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		row, err := tx.ResourceForUser(userID, id)
		if err != nil {
			return err
		}
		if err := tx.requireResourceProducersSettled(row); err != nil {
			return err
		}
		return tx.db.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ? AND playback_status = ?", id, userID, model.ResourceStatusDeleting, model.PlaybackStatusProcessing).
			Updates(map[string]any{"playback_status": model.PlaybackStatusFailed, "playback_error": "deletion requested"}).Error
	})
}

// AppearanceResourceIDs follows the explicit site-settings schema, not suffix guesses.
func AppearanceResourceIDs(raw string) ([]string, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	var canvas map[string]json.RawMessage
	if rawCanvas, ok := settings["canvas"]; ok {
		if err := json.Unmarshal(rawCanvas, &canvas); err != nil {
			return nil, err
		}
	}
	ids := map[string]struct{}{}
	for key, value := range canvas {
		if key == "live2dResourceId" || key == "avatarBundleResourceId" {
			var id string
			if err := json.Unmarshal(value, &id); err != nil {
				return nil, err
			}
			if id = strings.TrimSpace(id); id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	for _, key := range []string{"logoResourceId", "darkLogoResourceId", "authVideoResourceId", "authVideoPosterResourceId"} {
		value, ok := settings[key]
		if !ok {
			continue
		}
		var id string
		if err := json.Unmarshal(value, &id); err != nil {
			return nil, err
		}
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = struct{}{}
		}
	}
	return assetrefs.SortedIDs(ids), nil
}
