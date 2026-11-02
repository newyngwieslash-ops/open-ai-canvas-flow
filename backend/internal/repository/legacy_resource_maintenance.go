package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"yingce/backend/internal/model"
)

// LegacyResourceSnapshotDigest computes a deterministic digest of the resource's
// physical and lifecycle columns so any drift between read-only export, dry-run,
// and approved maintenance execution is caught fail-closed.
func LegacyResourceSnapshotDigest(row *model.Resource) string {
	if row == nil {
		return ""
	}
	payload := strings.Join([]string{
		row.ID,
		row.UserID,
		string(row.Status),
		deletionProvider(row.Provider),
		strings.TrimSpace(row.Endpoint),
		strings.TrimSpace(row.Bucket),
		strings.TrimSpace(row.ObjectKey),
		strings.TrimSpace(row.PlaybackObjectKey),
		strings.TrimSpace(row.PlaybackStatus),
		strings.TrimSpace(row.ThumbnailStatus),
		fmt.Sprintf("%d", row.ThumbnailSize),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// LegacyResourceProducerIdentity exposes the canonical producer identity for a
// resource and variant ("original" or "playback") for maintenance validation.
func LegacyResourceProducerIdentity(row *model.Resource, variant string) model.ResourceProducerOperation {
	return resourceProducerIdentity(row, variant)
}

// LegacyResourceProducerOperations loads all producer operations matching either
// the resource ID or the canonical physical identities of its variants.
func (r *Repository) LegacyResourceProducerOperations(row *model.Resource) ([]model.ResourceProducerOperation, error) {
	if row == nil || strings.TrimSpace(row.ID) == "" {
		return nil, nil
	}
	identities := []string{resourceProducerIdentity(row, "original").Identity}
	if strings.TrimSpace(row.PlaybackObjectKey) != "" || row.PlaybackStatus == model.PlaybackStatusProcessing {
		identities = append(identities, resourceProducerIdentity(row, "playback").Identity)
	}
	var ops []model.ResourceProducerOperation
	if err := r.db.Where("resource_id = ? OR identity IN ?", row.ID, identities).Order("id").Find(&ops).Error; err != nil {
		return nil, err
	}
	return ops, nil
}

// LegacyResourcePhysicalAliases returns all other Resource rows that share the
// same canonical physical identity on "original" or "playback".
func (r *Repository) LegacyResourcePhysicalAliases(row *model.Resource) ([]model.Resource, error) {
	if row == nil || strings.TrimSpace(row.ID) == "" {
		return nil, nil
	}
	targetIdentities := map[string]struct{}{}
	if orig := resourceProducerIdentity(row, "original"); orig.ObjectKey != "" {
		targetIdentities[orig.Identity] = struct{}{}
	}
	if play := resourceProducerIdentity(row, "playback"); play.ObjectKey != "" {
		targetIdentities[play.Identity] = struct{}{}
	}
	if len(targetIdentities) == 0 {
		return nil, nil
	}
	var candidates []model.Resource
	if err := r.db.Where("id <> ?", row.ID).Order("id").Find(&candidates).Error; err != nil {
		return nil, err
	}
	var aliases []model.Resource
	for _, cand := range candidates {
		matched := false
		for _, variant := range []string{"original", "playback"} {
			idOp := resourceProducerIdentity(&cand, variant)
			if idOp.ObjectKey != "" {
				if _, ok := targetIdentities[idOp.Identity]; ok {
					matched = true
					break
				}
			}
		}
		if matched {
			aliases = append(aliases, cand)
		}
	}
	return aliases, nil
}

// LegacyResourceHasSettledCoverage reports whether a resource row already has
// complete settled producer evidence for all of its materialized variants.
func (r *Repository) LegacyResourceHasSettledCoverage(row *model.Resource) bool {
	if row == nil {
		return false
	}
	return r.requireResourceProducersSettled(row) == nil
}

// RegisterLegacyResourceEvidence atomically records verified historical settled
// producer evidence, clears verified-absent legacy thumbnail state when proven,
// and appends an immutable AdminAuditEvent inside the resource lifecycle lock.
func (r *Repository) RegisterLegacyResourceEvidence(
	expectedRow *model.Resource,
	expectedDigest string,
	settledOps []model.ResourceProducerOperation,
	clearVerifiedAbsentThumbnail bool,
	auditEvent model.AdminAuditEvent,
) error {
	if expectedRow == nil || strings.TrimSpace(expectedRow.ID) == "" {
		return fmt.Errorf("invalid legacy resource target")
	}
	return r.withStoryboardResourceWrite(nil, []string{expectedRow.ID}, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			var current model.Resource
			if err := tx.db.First(&current, "id = ?", expectedRow.ID).Error; err != nil {
				return err
			}
			// If already idempotently applied in an earlier run with the exact same
			// post-thumbnail-clear state, allow idempotent re-verification when all
			// other fields match.
			currentDigest := LegacyResourceSnapshotDigest(&current)
			if currentDigest != expectedDigest {
				if clearVerifiedAbsentThumbnail && current.ThumbnailStatus == "" && current.ThumbnailSize == 0 {
					reconstructed := current
					reconstructed.ThumbnailStatus = expectedRow.ThumbnailStatus
					reconstructed.ThumbnailSize = expectedRow.ThumbnailSize
					if LegacyResourceSnapshotDigest(&reconstructed) != expectedDigest {
						return ErrResourceCleanupSetChanged
					}
				} else {
					return ErrResourceCleanupSetChanged
				}
			}
			if current.UserID != expectedRow.UserID || current.Status == model.ResourceStatusDeleting {
				return ErrResourceCleanupSetChanged
			}

			for _, op := range settledOps {
				if strings.TrimSpace(op.Variant) == "" || strings.TrimSpace(op.Proof) == "" {
					return ErrResourceProducerUnsettled
				}
				canonical := resourceProducerIdentity(&current, op.Variant)
				if canonical.ObjectKey == "" || canonical.Identity != op.Identity {
					return ErrResourceDeletionIdentityChanged
				}
				var existing []model.ResourceProducerOperation
				if err := tx.db.Where("resource_id = ? AND variant = ?", current.ID, op.Variant).Find(&existing).Error; err != nil {
					return err
				}
				matchedSettled := false
				for _, old := range existing {
					if old.Identity != canonical.Identity {
						return ErrResourceDeletionIdentityChanged
					}
					if old.PhysicalState == "writing" || old.PhysicalState == "reserved" {
						return ErrResourceProducerUnsettled
					}
					if old.PhysicalState == "unknown" {
						if err := tx.db.Model(&model.ResourceProducerOperation{}).
							Where("id = ? AND physical_state = ?", old.ID, "unknown").
							Updates(map[string]any{
								"physical_state": "settled",
								"outcome":        op.Outcome,
								"proof":          op.Proof,
							}).Error; err != nil {
							return err
						}
						matchedSettled = true
					} else if old.PhysicalState == "settled" {
						matchedSettled = true
					}
				}
				if !matchedSettled {
					record := canonical
					record.ID = deletionID()
					record.Protocol = op.Protocol
					record.PhysicalState = "settled"
					record.Outcome = op.Outcome
					record.Proof = op.Proof
					if err := tx.db.Create(&record).Error; err != nil {
						return err
					}
				}
			}

			if clearVerifiedAbsentThumbnail && (current.ThumbnailStatus != "" || current.ThumbnailSize != 0) {
				if err := tx.db.Model(&model.Resource{}).
					Where("id = ? AND user_id = ?", current.ID, current.UserID).
					Updates(map[string]any{
						"thumbnail_status": "",
						"thumbnail_size":   0,
					}).Error; err != nil {
					return err
				}
			}

			if strings.TrimSpace(auditEvent.ID) != "" {
				if err := tx.db.Create(&auditEvent).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}
