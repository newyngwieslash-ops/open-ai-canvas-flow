package app

import (
	"errors"
	"io"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type resourceWriteResult struct {
	ETag string
	// Only the concrete synchronous writer may assert this, never its caller
	// based on Provider, business success, timeout, or a cached file probe.
	PhysicalSettled bool
}

// Only this concrete synchronous original writer can register local-go-lock-v1.
// Callback writers keep the P1 contract and cannot borrow this protocol marker.
func (s *Service) writeStoredResourceProduction(resource *model.Resource, operation *model.ResourceProducerOperation, name string, body io.Reader) (etag string, op *model.ResourceProducerOperation, err error) {
	if operation == nil {
		if resource.Provider == "local" {
			operation, err = s.repo.BeginLocalResourceProduction(resource, false)
		} else {
			operation, err = s.repo.BeginResourceProduction(resource, "original", false)
		}
		if err != nil {
			return "", operation, err
		}
	}
	if operation.Protocol != repository.LocalGoProducerProtocol {
		etag, err = s.writeResourceProduction(resource, operation, name, body, s.storeResourceObject)
		return etag, operation, err
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{operation.Identity})
	if err != nil {
		return "", operation, err
	}
	defer func() { err = errors.Join(err, locks.Close()) }()
	writer := &Service{repo: s.repo.WithResourcePhysicalLocks(locks), dataDir: s.dataDir}
	etag, err = writer.writeResourceProduction(resource, operation, name, body, s.storeResourceObject)
	return etag, operation, err
}

func (s *Service) withResourceDeletionLocks(assetIDs, resourceIDs []string, apply func(*repository.Repository) error) (err error) {
	repo := s.repo
	if s.playbackControl != nil {
		repo = repo.WithControlledPlayback(s.playbackControl.Operations())
	}
	for attempt := 0; attempt < 3; attempt++ {
		identities, e := repo.ResourceDeletionLockIdentities(assetIDs, resourceIDs)
		if e != nil {
			return e
		}
		locks, e := repository.AcquireResourcePhysicalLocks(s.dataDir, identities)
		if e != nil {
			return e
		}
		err = apply(repo.WithResourcePhysicalLocks(locks))
		closeErr := locks.Close()
		if closeErr != nil {
			return errors.Join(err, closeErr)
		}
		if !errors.Is(err, repository.ErrResourceCleanupSetChanged) {
			return err
		}
	}
	return err
}

func (s *Service) publishResourceProduction(resource *model.Resource, operation *model.ResourceProducerOperation, reservationID string) error {
	if operation == nil || operation.Protocol != repository.LocalGoProducerProtocol {
		if resource.Status == model.ResourceStatusReady {
			return s.saveResourceWithinStorageLimit(resource, reservationID)
		}
		return s.repo.SaveResource(resource)
	}
	var limit, daily int64
	if resource.Status == model.ResourceStatusReady {
		policy, err := s.RuntimePolicy()
		if err != nil {
			return err
		}
		limit, daily = gigabytes(policy.Resource.StoredFileGB), megabytes(policy.Resource.DailyUploadMB)
	}
	return uploadReservationError(s.repo.SaveResourceProduction(resource, operation, limit, daily, reservationID))
}

func (s *Service) writeResourceProduction(resource *model.Resource, operation *model.ResourceProducerOperation, fileName string, body io.Reader, writer func(*model.Resource, string, io.Reader) (resourceWriteResult, error)) (string, error) {
	if operation == nil {
		var err error
		operation, err = s.repo.BeginResourceProduction(resource, "original", false)
		if err != nil {
			return "", err
		}
	}
	if err := s.repo.StartResourceProduction(operation); err != nil {
		return "", err
	}
	result, writeErr := writer(resource, fileName, body)
	settleErr := s.repo.FinishResourceProduction(operation, result.PhysicalSettled, writeErr == nil, "local-writer-returned")
	return result.ETag, errors.Join(writeErr, settleErr)
}
