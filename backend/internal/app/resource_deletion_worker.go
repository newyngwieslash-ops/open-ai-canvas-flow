package app

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"math"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const resourceDeletionLease = 2 * time.Minute
const incompleteResourceRetention = time.Hour
const detachedReadyResourceRetention = 24 * time.Hour

func (s *Service) startResourceDeletionWorker(ctx context.Context) {
	s.runWorkerLoop(func(ctx context.Context) {
		s.drainResourceDeletionJobs(32)
		s.cleanupStaleAnnouncementImageDrafts()
		s.cleanupExpiredArchivedAssets()
		s.cleanupDetachedResources()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		lastPeriodicCleanup := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.drainResourceDeletionJobs(32)
				if time.Since(lastPeriodicCleanup) >= time.Hour {
					s.cleanupStaleAnnouncementImageDrafts()
					s.cleanupExpiredArchivedAssets()
					s.cleanupDetachedResources()
					lastPeriodicCleanup = time.Now()
				}
			}
		}
	})
}

func (s *Service) cleanupDetachedResources() {
	now := time.Now()
	candidates, err := s.repo.ResourceCleanupCandidates(
		now.Add(-incompleteResourceRetention),
		now.Add(-detachedReadyResourceRetention),
		500,
	)
	if err != nil {
		log.Printf("detached resource cleanup query failed: %v", err)
		return
	}
	byUser := map[string][]model.Resource{}
	for _, resource := range candidates {
		byUser[resource.UserID] = append(byUser[resource.UserID], resource)
	}
	for userID, resources := range byUser {
		if err := s.cleanupDetachedUserResources(userID, resources); err != nil {
			log.Printf("detached resource cleanup failed for user %s: %v", userID, err)
		}
	}
}

func (s *Service) cleanupDetachedUserResources(userID string, candidates []model.Resource) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	resourceIDs := make([]string, 0, len(candidates))
	candidateSet := make(map[string]struct{}, len(candidates))
	for _, resource := range candidates {
		resourceIDs = append(resourceIDs, resource.ID)
		candidateSet[resource.ID] = struct{}{}
	}
	snapshot, err := s.repo.ResourceReferenceSnapshot(userID, "", resourceIDs)
	if err != nil {
		return err
	}
	referenced := map[string]struct{}{}
	// Site assets are global references and are not part of a user's canvas snapshot.
	for resourceID := range s.appearanceResourceReferences(resourceIDs) {
		referenced[resourceID] = struct{}{}
	}
	for _, reference := range snapshot.Direct {
		if _, exists := candidateSet[reference.ResourceID]; exists {
			referenced[reference.ResourceID] = struct{}{}
		}
	}
	for _, document := range snapshot.Documents {
		for resourceID := range documentReferencedResourceIDs(document.PrimaryJSON, candidateSet) {
			referenced[resourceID] = struct{}{}
		}
		for resourceID := range documentReferencedResourceIDs(document.SecondaryJSON, candidateSet) {
			referenced[resourceID] = struct{}{}
		}
	}
	detached := make([]model.Resource, 0, len(candidates))
	for _, resource := range candidates {
		if _, exists := referenced[resource.ID]; !exists {
			detached = append(detached, resource)
		}
	}
	if len(detached) == 0 {
		return nil
	}
	detachedIDs := make([]string, 0, len(detached))
	for _, resource := range detached {
		detachedIDs = append(detachedIDs, resource.ID)
	}
	physicalObjects := map[string]*model.Resource{}
	for index := range detached {
		resource := &detached[index]
		if strings.TrimSpace(resource.ObjectKey) == "" {
			continue
		}
		sharedCount, countErr := s.repo.ResourceStorageReferenceCount(resource, detachedIDs)
		if countErr != nil {
			return countErr
		}
		if sharedCount == 0 {
			physicalObjects[resourceStorageIdentity(resource)] = resource
		}
	}
	deletionJobs := resourceDeletionJobs(userID, physicalObjects)
	if err := s.withResourceDeletionLocks(nil, detachedIDs, func(repo *repository.Repository) error { return repo.DeleteDetachedResources(detached, deletionJobs) }); err != nil {
		return err
	}
	slog.Info("detached resource cleanup", "removed", len(detached), "user", userID)
	if len(deletionJobs) > 0 {
		s.runWorkerTask(func() { s.drainResourceDeletionJobs(len(deletionJobs)) })
	}
	return nil
}

func (s *Service) cleanupExpiredArchivedAssets() {
	policy, err := s.RuntimePolicy()
	if err != nil {
		return
	}
	retentionDays := policy.Resource.RecycleBinRetentionDays
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	expired, err := s.repo.FindExpiredArchivedAssets(cutoff, 100)
	if err != nil {
		log.Printf("expired archived assets query failed: %v", err)
		return
	}
	deleted := 0
	for _, asset := range expired {
		// 自动清理与用户手动删除走同一条强校验路径：先检查业务引用，
		// 再以事务 + Outbox 删除资源记录和物理对象，不能从 repository 旁路。
		if err := s.DeleteUserAsset(asset.UserID, asset.ID); err != nil {
			log.Printf("expired archived asset delete failed for %s: %v", asset.ID, err)
			continue
		}
		deleted++
	}
	if deleted > 0 {
		slog.Info("recycle bin cleanup", "deleted", deleted, "retention_days", retentionDays)
	}
}

func (s *Service) drainResourceDeletionJobs(limit int) {
	owner := s.workerID
	if owner == "" {
		owner = newID()
	}
	for index := 0; index < limit; index++ {
		job, err := s.repo.ClaimNextResourceDeletionJob(owner, resourceDeletionLease)
		if err != nil {
			log.Printf("resource deletion worker claim failed: %v", err)
			return
		}
		if job == nil {
			return
		}
		err = s.processResourceDeletionJob(job)
		if err != nil {
			log.Printf("resource deletion worker attempt failed for %s: %v", job.ID, err)
			continue
		}
	}
}

func (s *Service) processResourceDeletionJob(job *model.ResourceDeletionJob) (err error) {
	var locks *repository.ResourcePhysicalLocks
	defer func() {
		if err != nil {
			err = errors.Join(err, s.repo.RetryResourceDeletionJob(job.ID, job.LeaseOwner, err.Error(), time.Now().Add(resourceDeletionRetryDelay(job.Attempts))))
		}
		err = errors.Join(err, locks.Close())
	}()
	resource := &model.Resource{ID: job.ResourceID, UserID: job.UserID, Provider: job.Provider, Endpoint: job.Endpoint, Bucket: job.Bucket, StorageSettingID: job.StorageSettingID, ObjectKey: job.ObjectKey, PlaybackObjectKey: job.ObjectKey}
	repo := s.repo
	var stopped []model.ResourceProducerOperation
	var controlIdentities []string
	if s.playbackControl != nil {
		repo = repo.WithControlledPlayback(s.playbackControl.Operations())
		stopped, controlIdentities, err = repo.ControlledPlaybackForDeletion(job)
		if err != nil {
			return err
		}
		for _, operation := range stopped {
			if err = s.playbackControl.CloseAndStop(operation); err != nil {
				return err
			}
		}
	}
	if job.Provider == "local" || job.Provider == "" {
		candidate, e := s.repo.ResourceDeletionObject(job.ID)
		if e != nil {
			return e
		}
		controlIdentities = append(controlIdentities, repository.ResourcePhysicalIdentity(resource, candidate.Variant))
		locks, e = repository.AcquireResourcePhysicalLocks(s.dataDir, controlIdentities)
		if e != nil {
			return e
		}
		repo = repo.WithResourcePhysicalLocks(locks)
	}
	if len(stopped) > 0 {
		if err = repo.SettleControlledPlaybackDeletion(job, stopped); err != nil {
			return err
		}
	}
	object, retained, err := repo.PrepareResourceDeletion(job)
	if err != nil {
		return err
	}
	if !retained {
		if locks != nil {
			if err := locks.Validate(repository.ResourcePhysicalIdentity(resource, object.Variant)); err != nil {
				return err
			}
		}
		if object.Variant == "playback" {
			err = s.deleteLocalPlaybackObject(object.ObjectKey)
		} else {
			err = s.deleteStoredResourceObject(job.UserID, resource)
		}
		if err != nil {
			return err
		}
	}
	return repo.FinishResourceDeletion(job, retained)
}

func resourceDeletionRetryDelay(attempts int) time.Duration {
	exponent := math.Min(float64(max(attempts-1, 0)), 8)
	return time.Duration(math.Pow(2, exponent)) * 15 * time.Second
}
