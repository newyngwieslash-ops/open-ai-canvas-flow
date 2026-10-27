package app

import (
	"errors"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// No production implementation is installed in P3-B. A future verified
// controller must bind exact operation generations, serialize launch with
// CloseAndStop, and return success only after no writer can join or remain.
// Tokens are local capabilities, not values accepted from HTTP or provider data.
type playbackProducerControl interface {
	Operations() []model.ResourceProducerOperation
	CloseAndStop(model.ResourceProducerOperation) error
	// Completion closes launch and proves the whole domain stopped, and carries
	// the encoder result from that same invocation. Stop alone is not success.
	Completion(model.ResourceProducerOperation) (repository.ControlledPlaybackReceipt, error)
}

func (s *Service) completeControlledPlayback(op *model.ResourceProducerOperation) (err error) {
	if s.playbackControl == nil || op == nil {
		return repository.ErrResourceProducerUnsettled
	}
	repo := s.repo.WithControlledPlayback(s.playbackControl.Operations())
	row, err := repo.PrepareControlledPlaybackCompletion(op)
	if err != nil {
		return err
	}
	receipt, err := s.playbackControl.Completion(*op)
	if err != nil {
		return err
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(row, "original"), op.Identity})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.Close()) }()
	return repo.WithResourcePhysicalLocks(locks).CompleteControlledPlaybackProduction(op, receipt)
}
