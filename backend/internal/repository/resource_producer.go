package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path"
	"runtime"
	"strings"

	"gorm.io/gorm"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

var ErrResourceProducerUnsettled = kernel.NewAppError(http.StatusConflict, "资源写入的物理结算证据不足，已保留记录")

func resourceProducerIdentity(row *model.Resource, variant string) model.ResourceProducerOperation {
	key, namespace := row.ObjectKey, ""
	provider, endpoint, bucket := deletionProvider(row.Provider), row.Endpoint, row.Bucket
	if variant == "playback" {
		key = row.PlaybackObjectKey
	}
	identityKey := key
	if provider == "local" {
		namespace, endpoint, bucket = "resources", "", ""
		if variant == "playback" {
			namespace = "playback"
		}
		identityKey = path.Join(namespace, strings.ReplaceAll(key, "\\", "/"))
		// Hash the final relative path, including cross-namespace aliases.
		namespace = "local-data"
		if runtime.GOOS == "windows" {
			identityKey = strings.ToLower(identityKey)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{provider, endpoint, bucket, namespace, identityKey}, "\x00")))
	return model.ResourceProducerOperation{ResourceID: row.ID, UserID: row.UserID, Variant: variant, Provider: provider, Endpoint: row.Endpoint, Bucket: row.Bucket, Namespace: namespace, ObjectKey: key, Identity: hex.EncodeToString(sum[:]), Protocol: "producer-accounting-v1"}
}

// BeginResourceProduction atomically creates the initial row and its write
// obligation, or admits a retry against the current database identity.
func (r *Repository) BeginResourceProduction(snapshot *model.Resource, variant string, create bool) (*model.ResourceProducerOperation, error) {
	return r.beginResourceProduction(snapshot, variant, create, "producer-accounting-v1")
}

func (r *Repository) BeginLocalResourceProduction(snapshot *model.Resource, create bool) (*model.ResourceProducerOperation, error) {
	if snapshot == nil || deletionProvider(snapshot.Provider) != "local" {
		return nil, gorm.ErrInvalidData
	}
	return r.beginResourceProduction(snapshot, "original", create, LocalGoProducerProtocol)
}

func (r *Repository) beginResourceProduction(snapshot *model.Resource, variant string, create bool, protocol string) (*model.ResourceProducerOperation, error) {
	if snapshot == nil || snapshot.ID == "" || snapshot.UserID == "" {
		return nil, gorm.ErrInvalidData
	}
	var operation model.ResourceProducerOperation
	admit := func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			row := snapshot
			if create {
				if variant != "original" || row.Status != model.ResourceStatusPending {
					return gorm.ErrInvalidData
				}
				if err := tx.requireResourceIdentityWritable(row); err != nil {
					return err
				}
				if err := tx.db.Create(row).Error; err != nil {
					return err
				}
			} else {
				current, err := tx.ResourceForUser(snapshot.UserID, snapshot.ID)
				if err != nil {
					return err
				}
				if deletionIdentity(current.Provider, current.Endpoint, current.Bucket, current.ObjectKey) != deletionIdentity(snapshot.Provider, snapshot.Endpoint, snapshot.Bucket, snapshot.ObjectKey) || current.PlaybackObjectKey != snapshot.PlaybackObjectKey {
					return ErrResourceDeletionIdentityChanged
				}
				row = current
			}
			if variant == "original" {
				if row.Status != model.ResourceStatusPending {
					return ErrResourceDeleting
				}
			} else if variant != "playback" || row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing || row.PlaybackObjectKey != row.ID+".mp4" {
				return ErrResourceDeleting
			}
			if err := tx.requireResourceIdentityWritable(row); err != nil {
				return err
			}
			operation = resourceProducerIdentity(row, variant)
			if operation.ObjectKey == "" {
				return gorm.ErrInvalidData
			}
			prior, err := tx.checkResourceProductionAdmission(&operation)
			if err != nil {
				return err
			}
			if !create && prior == 0 {
				// Absence of an operation is not proof that an older binary never wrote.
				legacy := operation
				legacy.ID, legacy.PhysicalState, legacy.Proof = deletionID(), "unknown", "legacy-untracked"
				if err := tx.db.Create(&legacy).Error; err != nil {
					return err
				}
			}
			operation.ID, operation.PhysicalState = deletionID(), "reserved"
			operation.Protocol = protocol
			if protocol == LocalGoProducerProtocol {
				// Supersede publication rights atomically; timestamps are not a token order.
				// Keep every prior physical proof, including unknown legacy obligations.
				if err := tx.db.Model(&model.ResourceProducerOperation{}).Where("resource_id = ? AND variant = ? AND protocol = ? AND physical_state = ?", row.ID, variant, protocol, "settled").Update("fenced", true).Error; err != nil {
					return err
				}
			}
			return tx.db.Create(&operation).Error
		})
	}
	var err error
	if create {
		err = r.WithStoryboardAssetOwners(r.db.Statement.Context, []string{snapshot.UserID}, admit)
	} else {
		err = r.withStoryboardResourceWrite(nil, []string{snapshot.ID}, admit)
	}
	return &operation, err
}

// Shared by original writes and playback claims, under the lifecycle transaction.
func (r *Repository) checkResourceProductionAdmission(operation *model.ResourceProducerOperation) (int, error) {
	if operation.Provider == "local" {
		key := strings.ReplaceAll(operation.ObjectKey, "\\", "/")
		if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, ":") || path.Clean(key) == ".." || strings.HasPrefix(path.Clean(key), "../") {
			return 0, gorm.ErrInvalidData
		}
	}
	if err := r.requirePhysicalIdentityWritable(operation.Identity); err != nil {
		return 0, err
	}
	var prior []model.ResourceProducerOperation
	if err := r.db.Where("resource_id = ? AND variant = ?", operation.ResourceID, operation.Variant).Find(&prior).Error; err != nil {
		return 0, err
	}
	for _, old := range prior {
		if old.Identity != operation.Identity {
			return 0, ErrResourceDeletionIdentityChanged
		}
		if old.Fenced && !(old.Protocol == LocalGoProducerProtocol && old.PhysicalState == "settled" && old.Proof == "local-writer-returned") {
			return 0, ErrResourceDeleting
		}
		if old.PhysicalState == "writing" || old.PhysicalState == "reserved" {
			return 0, ErrResourceProducerUnsettled
		}
	}
	return len(prior), nil
}

// StartResourceProduction consumes one immutable admission before any IO.
// A delayed start remains an obligation; a second invocation cannot reuse it.
func (r *Repository) StartResourceProduction(operation *model.ResourceProducerOperation) error {
	if operation == nil {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		var current model.ResourceProducerOperation
		if err := tx.db.First(&current, "id = ?", operation.ID).Error; err != nil {
			return err
		}
		if current.ResourceID != operation.ResourceID || current.UserID != operation.UserID || current.Identity != operation.Identity || current.PhysicalState != "reserved" || current.Fenced {
			return ErrResourceProducerUnsettled
		}
		if current.Protocol == LocalGoProducerProtocol {
			if err := tx.physicalLocks().Validate(current.Identity); err != nil {
				return err
			}
		}
		row, err := tx.ResourceForUser(current.UserID, current.ResourceID)
		if err != nil {
			return err
		}
		if resourceProducerIdentity(row, current.Variant).Identity != current.Identity {
			return ErrResourceDeletionIdentityChanged
		}
		if err := tx.requireResourceIdentityWritable(row); err != nil {
			return err
		}
		if (current.Variant == "original" && row.Status != model.ResourceStatusPending) || (current.Variant == "playback" && (row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing)) {
			return ErrResourceDeleting
		}
		result := tx.db.Model(&model.ResourceProducerOperation{}).Where("id = ? AND physical_state = ? AND fenced = ?", current.ID, "reserved", false).Update("physical_state", "writing")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrResourceProducerUnsettled
		}
		return nil
	})
}

// FinishResourceProduction records only this invocation's physical outcome.
// It cannot publish ready, fence another operation, or settle an older attempt.
func (r *Repository) FinishResourceProduction(operation *model.ResourceProducerOperation, settled, success bool, proof string) error {
	if operation == nil {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		var current model.ResourceProducerOperation
		if err := tx.db.First(&current, "id = ?", operation.ID).Error; err != nil {
			return err
		}
		if current.ResourceID != operation.ResourceID || current.UserID != operation.UserID || current.Identity != operation.Identity || current.PhysicalState != "writing" || current.Fenced {
			return ErrResourceProducerUnsettled
		}
		if current.Protocol == LocalGoProducerProtocol {
			if err := tx.physicalLocks().Validate(current.Identity); err != nil {
				return err
			}
		}
		row, err := tx.ResourceForUser(current.UserID, current.ResourceID)
		if err != nil {
			return err
		}
		if resourceProducerIdentity(row, current.Variant).Identity != current.Identity {
			return ErrResourceDeletionIdentityChanged
		}
		state, outcome := "unknown", "failed"
		if current.Protocol == ControlledPlaybackProtocol {
			return ErrResourceProducerUnsettled
		}
		if success {
			outcome = "success"
		}
		if settled {
			if current.Provider != "local" || (proof != "local-writer-returned" && proof != "ffmpeg-wait-returned") {
				return gorm.ErrInvalidData
			}
			state = "settled"
		} else {
			proof = "no-terminal-proof"
		}
		result := tx.db.Model(&model.ResourceProducerOperation{}).Where("id = ? AND physical_state = ? AND fenced = ?", current.ID, "writing", false).Updates(map[string]any{"physical_state": state, "outcome": outcome, "proof": proof})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrResourceProducerUnsettled
		}
		return nil
	})
}

// Check every attempt, not only the resource's latest business status. All
// aliases need provenance too; a tracked new alias cannot launder an old writer.
func (r *Repository) requireResourceProducersSettled(row *model.Resource) error {
	return r.requireResourceProducers(row, nil, "")
}

func (r *Repository) requireResourceProducers(row *model.Resource, selected map[string]bool, userID string) error {
	variants := []string{"original"}
	if row.PlaybackObjectKey != "" || row.PlaybackStatus == model.PlaybackStatusProcessing {
		variants = append(variants, "playback")
	}
	for _, variant := range variants {
		identity := resourceProducerIdentity(row, variant)
		if identity.ObjectKey == "" {
			return ErrResourceProducerUnsettled
		}
		var operations []model.ResourceProducerOperation
		if err := r.db.Where("identity = ? OR (resource_id = ? AND variant = ?)", identity.Identity, row.ID, variant).Find(&operations).Error; err != nil {
			return err
		}
		covered := map[string]bool{}
		for _, operation := range operations {
			if operation.Identity != identity.Identity || operation.PhysicalState != "settled" && !r.controlledPlaybackAllowed(&operation, selected, userID) {
				return ErrResourceProducerUnsettled
			}
			if operation.PhysicalState != "settled" {
				if err := r.requireControlledPlaybackDomain(&operation, selected, userID); err != nil {
					return err
				}
			}
			covered[operation.ResourceID+"\x00"+operation.Variant] = true
		}
		if !covered[row.ID+"\x00"+variant] {
			return ErrResourceProducerUnsettled
		}
		var aliases []model.Resource
		// ponytail: scan resources for canonical local aliases; add a persisted
		// canonical identity index if resource counts make this scan expensive.
		if err := r.db.Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			for _, aliasVariant := range []string{"original", "playback"} {
				aliasIdentity := resourceProducerIdentity(&alias, aliasVariant)
				if aliasIdentity.ObjectKey != "" && aliasIdentity.Identity == identity.Identity && !covered[alias.ID+"\x00"+aliasVariant] {
					return ErrResourceProducerUnsettled
				}
			}
		}
	}
	return nil
}

func (r *Repository) requireDeletionProducerEvidence(resourceIDs []string) error {
	var rows []model.Resource
	if err := r.db.Where("id IN ?", resourceIDs).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(resourceIDs) {
		return ErrResourceDeletionIdentityChanged
	}
	for _, row := range rows {
		if err := r.requireResourceProducersSettled(&row); err != nil {
			return err
		}
	}
	return nil
}
