package repository

import (
	assetrefs "yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

// ResourceDeletionLockIdentities is an unlocked candidate snapshot. Stage must
// rebuild the authoritative set and reject any missing capability in its tx.
func (r *Repository) ResourceDeletionLockIdentities(assetIDs, resourceIDs []string) ([]string, error) {
	wanted := map[string]struct{}{}
	for _, id := range resourceIDs {
		wanted[id] = struct{}{}
	}
	if len(assetIDs) > 0 {
		var assets []model.Asset
		if err := r.db.Where("id IN ?", assetIDs).Find(&assets).Error; err != nil {
			return nil, err
		}
		for _, asset := range assets {
			if err := assetrefs.CollectOwnedDocumentReferences(asset.PayloadJSON, wanted); err != nil {
				return nil, err
			}
		}
		versions, reps, err := r.AssetsResourceRecords(assetIDs)
		if err != nil {
			return nil, err
		}
		for _, version := range versions {
			if err := assetrefs.CollectOwnedDocumentReferences(version.DefinitionJSON, wanted); err != nil {
				return nil, err
			}
		}
		for _, rep := range reps {
			if rep.ResourceID != "" {
				wanted[rep.ResourceID] = struct{}{}
			}
			if err := assetrefs.CollectOwnedDocumentReferences(rep.MetadataJSON, wanted); err != nil {
				return nil, err
			}
		}
	}
	resourceIDs = assetrefs.SortedIDs(wanted)
	var rows []model.Resource
	if err := r.db.Where("id IN ?", resourceIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	var identities []string
	for _, row := range rows {
		if r.hasControlledPlayback(&row) {
			continue
		}
		if deletionProvider(row.Provider) == "local" {
			identities = append(identities, ResourcePhysicalIdentity(&row, "original"))
			if row.PlaybackObjectKey != "" || row.PlaybackStatus == model.PlaybackStatusProcessing {
				if row.PlaybackObjectKey == "" {
					row.PlaybackObjectKey = row.ID + ".mp4"
				}
				identities = append(identities, ResourcePhysicalIdentity(&row, "playback"))
			}
		}
	}
	return identities, nil
}

// Called only by stage, under the same transaction as references and the ledger.
// A direct repository caller without real OS locks cannot perform recovery.
func (r *Repository) recoverLocalResourceProducers(rows []model.Resource) error {
	locks := r.physicalLocks()
	if locks == nil {
		return nil
	}
	selected := map[string]bool{}
	for _, row := range rows {
		selected[row.ID] = true
	}
	for i := range rows {
		row := &rows[i]
		// This exact controller-owned playback cannot be stopped while waiting
		// for its read/output locks. Stage still validates every real obligation.
		if r.hasControlledPlayback(row) {
			continue
		}
		if deletionProvider(row.Provider) != "local" {
			continue
		}
		identity := ResourcePhysicalIdentity(row, "original")
		if err := locks.Validate(identity); err != nil {
			return ErrResourceCleanupSetChanged
		}
		if row.PlaybackObjectKey != "" || row.PlaybackStatus == model.PlaybackStatusProcessing {
			copy := *row
			if copy.PlaybackObjectKey == "" {
				copy.PlaybackObjectKey = copy.ID + ".mp4"
			}
			if err := locks.Validate(ResourcePhysicalIdentity(&copy, "playback")); err != nil {
				return ErrResourceCleanupSetChanged
			}
		}
		var operations []model.ResourceProducerOperation
		if err := r.db.Where("identity = ? OR (resource_id = ? AND variant = ?)", identity, row.ID, "original").Find(&operations).Error; err != nil {
			return err
		}
		needed := row.Status == model.ResourceStatusPending
		ownNew := false
		for _, op := range operations {
			if op.ResourceID == row.ID && op.Protocol == LocalGoProducerProtocol {
				ownNew = true
			}
			if op.PhysicalState == "settled" {
				continue
			}
			needed = true
			if !selected[op.ResourceID] || op.Protocol != LocalGoProducerProtocol || op.Variant != "original" || op.Identity != identity {
				return ErrResourceProducerUnsettled
			}
		}
		if !needed {
			continue
		}
		if !ownNew {
			return ErrResourceProducerUnsettled
		}
		for _, op := range operations {
			if op.ResourceID != row.ID || op.Protocol != LocalGoProducerProtocol {
				continue
			}
			if op.Fenced && op.PhysicalState == "settled" && op.Proof == "local-writer-returned" {
				continue // Superseded publication token; retain its original physical proof.
			}
			if op.Identity != identity || op.Fenced {
				return ErrResourceProducerUnsettled
			}
			if err := r.db.Model(&model.ResourceProducerOperation{}).Where("id = ? AND fenced = ?", op.ID, false).Updates(map[string]any{"fenced": true, "physical_state": "settled", "outcome": "failed", "proof": "local-lock-recovered"}).Error; err != nil {
				return err
			}
		}
		if row.Status == model.ResourceStatusPending {
			if err := r.db.Model(&model.Resource{}).Where("id = ? AND status = ?", row.ID, model.ResourceStatusPending).Updates(map[string]any{"status": model.ResourceStatusFailed, "error": "local producer interrupted; success unproven"}).Error; err != nil {
				return err
			}
			row.Status = model.ResourceStatusFailed
		}
	}
	return nil
}
