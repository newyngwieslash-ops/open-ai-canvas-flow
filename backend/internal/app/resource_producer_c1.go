package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// C1 is deliberately installed only by the isolated composition root. Neither
// NewService, HTTP, environment variables nor deployment enable this controller.
type controlledPlaybackLauncher interface {
	claim(string) (*model.ResourceProducerOperation, error)
	run(*model.ResourceProducerOperation, string, string) error
	recover(*Service) error
}

type playbackControlDomain interface {
	start(string, []string, string) error
	result(bool) (string, error)
	close() error
}

type c1PlaybackController struct {
	repo                    *repository.Repository
	root, owner, executable string
	mu                      sync.Mutex
	domains                 map[string]playbackControlDomain
	tokens                  map[string]model.ResourceProducerOperation
}

func newC1PlaybackController(repo *repository.Repository, root, executable string) (*c1PlaybackController, error) {
	if repo == nil || !filepath.IsAbs(executable) {
		return nil, os.ErrInvalid
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := validateC1Path(root, root, false); err != nil {
		return nil, err
	}
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(os.ErrInvalid, err)
	}
	if err := playbackControlSupported(); err != nil {
		return nil, err
	}
	return &c1PlaybackController{repo: repo, root: root, executable: executable, owner: uuid.NewString(), domains: map[string]playbackControlDomain{}, tokens: map[string]model.ResourceProducerOperation{}}, nil
}

func (c *c1PlaybackController) claim(id string) (*model.ResourceProducerOperation, error) {
	return c.repo.ClaimWindowsControlledPlayback(id, c.owner)
}

func (c *c1PlaybackController) gate(op *model.ResourceProducerOperation) (*repository.ResourcePhysicalLocks, error) {
	if op == nil {
		return nil, repository.ErrResourceProducerUnsettled
	}
	return repository.AcquireResourcePhysicalLocks(c.root, []string{repository.PlaybackControlGateIdentity(op.ID)})
}

func (c *c1PlaybackController) remember(op *model.ResourceProducerOperation, domain playbackControlDomain) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[op.ID] = *op
	if domain != nil {
		c.domains[op.ID] = domain
	}
}

func (c *c1PlaybackController) domain(id string) playbackControlDomain {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.domains[id]
}

func (c *c1PlaybackController) Operations() []model.ResourceProducerOperation {
	c.mu.Lock()
	var tokens []model.ResourceProducerOperation
	for _, op := range c.tokens {
		tokens = append(tokens, op)
	}
	c.mu.Unlock()
	var result []model.ResourceProducerOperation
	for _, token := range tokens {
		op, err := c.repo.ControlledPlaybackOperation(token.ID)
		if err == nil && sameC1Operation(op, &token) && (op.ControlPhase == "stopped" || op.ControlOwner == c.owner && c.domain(op.ID) != nil) {
			result = append(result, *op)
		}
	}
	return result
}

func sameC1Operation(a, b *model.ResourceProducerOperation) bool {
	return a != nil && b != nil && a.ID == b.ID && a.ResourceID == b.ResourceID && a.UserID == b.UserID && a.Identity == b.Identity && a.Protocol == b.Protocol && a.Provider == "local" && b.Provider == "local" && a.Variant == "playback" && b.Variant == "playback" && a.ControlOwner == b.ControlOwner
}

// launch is private trusted code; callers hold the original/output physical
// locks throughout IO. The persistent gate is held across attempted and the
// atomic OS create, so close can never admit a late process.
func (c *c1PlaybackController) launch(op *model.ResourceProducerOperation, args []string) (err error) {
	gate, err := c.gate(op)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, gate.Close()) }()
	current, err := c.repo.ControlledPlaybackOperation(op.ID)
	if err != nil {
		return err
	}
	if !sameC1Operation(current, op) || current.ControlOwner != c.owner || current.ControlPhase != "armed" {
		return repository.ErrResourceProducerUnsettled
	}
	domain, err := newPlaybackControlDomain()
	if err != nil {
		return err
	}
	c.remember(current, domain)
	repo := c.repo.WithResourcePhysicalLocks(gate)
	if err = repo.TransitionPlaybackControl(current, "armed", "attempted", ""); err != nil {
		// No create was attempted. Keep the closed capability only if its receipt
		// commits; a later recovery can still close the armed row under the gate.
		_ = domain.close()
		c.mu.Lock()
		delete(c.domains, op.ID)
		delete(c.tokens, op.ID)
		c.mu.Unlock()
		return err
	}
	if err = domain.start(c.executable, args, c.root); err != nil {
		// CreateProcess failure has no child, but query the original Job anyway.
		// An uncertain query or a failed commit keeps its original handle alive.
		if e := c.stopLocked(repo, current, domain, true); e != nil {
			return errors.Join(err, e)
		}
		return err
	}
	return nil
}

func (c *c1PlaybackController) stopLocked(repo *repository.Repository, op *model.ResourceProducerOperation, domain playbackControlDomain, stop bool) error {
	current, err := repo.ControlledPlaybackOperation(op.ID)
	if err != nil {
		return err
	}
	if !sameC1Operation(current, op) {
		return repository.ErrResourceProducerUnsettled
	}
	if current.ControlPhase == "stopped" {
		c.remember(current, nil)
		return nil
	}
	if current.ControlPhase != "closed" {
		if err := repo.TransitionPlaybackControl(current, current.ControlPhase, "closed", ""); err != nil {
			return err
		}
	}
	if domain == nil || current.ControlOwner != c.owner {
		return repository.ErrResourceProducerUnsettled
	}
	result, err := domain.result(stop)
	if err != nil {
		return err
	}
	if err := repo.TransitionPlaybackControl(current, "closed", "stopped", result); err != nil {
		return err
	}
	current, err = repo.ControlledPlaybackOperation(op.ID)
	if err != nil {
		return err
	}
	c.remember(current, nil)
	if err := domain.close(); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.domains, op.ID)
	c.mu.Unlock()
	return nil
}

func (c *c1PlaybackController) CloseAndStop(op model.ResourceProducerOperation) (err error) {
	gate, err := c.gate(&op)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, gate.Close()) }()
	repo := c.repo.WithResourcePhysicalLocks(gate)
	current, err := repo.ControlledPlaybackOperation(op.ID)
	if err != nil {
		return err
	}
	if !sameC1Operation(current, &op) {
		return repository.ErrResourceProducerUnsettled
	}
	if current.ControlPhase == "armed" {
		if err := repo.RecoverUnstartedPlaybackControl(current); err != nil {
			return err
		}
		current, err = repo.ControlledPlaybackOperation(op.ID)
		if err != nil {
			return err
		}
		c.remember(current, nil)
		return nil
	}
	return c.stopLocked(repo, current, c.domain(op.ID), true)
}

func (c *c1PlaybackController) Completion(op model.ResourceProducerOperation) (repository.ControlledPlaybackReceipt, error) {
	current, err := c.repo.ControlledPlaybackOperation(op.ID)
	if err != nil || !sameC1Operation(current, &op) || current.ControlPhase != "stopped" {
		return repository.ControlledPlaybackReceipt{}, errors.Join(repository.ErrResourceProducerUnsettled, err)
	}
	receipt := repository.ControlledPlaybackReceipt{Operation: *current, Success: current.ControlResult == "success"}
	if !receipt.Success {
		receipt.ErrorText = "controlled encoder did not complete successfully"
	}
	return receipt, nil
}

func validateC1Path(root, target string, missingLeaf bool) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return os.ErrInvalid
	}
	for at := target; ; at = filepath.Dir(at) {
		info, e := os.Lstat(at)
		if e != nil {
			if missingLeaf && at == target && os.IsNotExist(e) {
				continue
			}
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return os.ErrInvalid
		}
		if e := validateC1PlatformPath(at); e != nil {
			return e
		}
		if at == root {
			break
		}
	}
	return nil
}

func (c *c1PlaybackController) run(op *model.ResourceProducerOperation, src, dst string) (err error) {
	return c.runCommand(op, src, dst, h264TranscodeArgs(src, dst))
}

func (c *c1PlaybackController) runCommand(op *model.ResourceProducerOperation, src, dst string, args []string) (err error) {
	row, err := c.repo.ResourceForUser(op.UserID, op.ResourceID)
	if err != nil {
		return err
	}
	if filepath.Clean(src) != filepath.Join(c.root, "resources", filepath.FromSlash(row.ObjectKey)) || filepath.Clean(dst) != filepath.Join(c.root, playbackDirName, filepath.FromSlash(row.PlaybackObjectKey)) {
		return os.ErrInvalid
	}
	locks, err := repository.AcquireResourcePhysicalLocks(c.root, []string{repository.ResourcePhysicalIdentity(row, "original"), op.Identity})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.Close()) }()
	if err := validateC1Path(c.root, filepath.Dir(dst), true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return err
	}
	if err := validateC1Path(c.root, src, false); err != nil {
		return err
	}
	if err := validateC1Path(c.root, dst, true); err != nil {
		return err
	}
	if err := c.launch(op, args); err != nil {
		return err
	}
	domain := c.domain(op.ID)
	if domain == nil { // A concurrent stop may already have durably closed it.
		current, e := c.repo.ControlledPlaybackOperation(op.ID)
		if e == nil && current.ControlPhase == "stopped" {
			return nil
		}
		return repository.ErrResourceProducerUnsettled
	}
	if _, err := domain.result(false); err != nil {
		return err
	}
	gate, err := c.gate(op)
	if err != nil {
		return err
	}
	err = c.stopLocked(c.repo.WithResourcePhysicalLocks(gate), op, domain, false)
	return errors.Join(err, gate.Close())
}

// Bounded cursor recovery is independent of PlaybackPendingVideos and deletion.
// No replacement domain or process is ever created by recovery.
func (c *c1PlaybackController) recover(s *Service) error {
	var allErr error
	for after := ""; ; {
		rows, err := c.repo.UnsettledPlaybackControls(after, 50)
		if err != nil {
			return errors.Join(allErr, err)
		}
		if len(rows) == 0 {
			return allErr
		}
		for i := range rows {
			op := &rows[i]
			after = op.ID
			current, e := c.repo.ControlledPlaybackOperation(op.ID)
			if e != nil {
				allErr = errors.Join(allErr, e)
				continue
			}
			if current.ControlPhase == "armed" {
				e = c.CloseAndStop(*current)
			} else if current.ControlPhase != "stopped" {
				c.mu.Lock()
				token, owned := c.tokens[current.ID]
				domain := c.domains[current.ID]
				c.mu.Unlock()
				if owned && domain != nil && current.ControlOwner == c.owner && sameC1Operation(current, &token) {
					e = c.CloseAndStop(*current)
				} else {
					e = repository.ErrResourceProducerUnsettled
				}
			}
			if e != nil {
				allErr = errors.Join(allErr, fmt.Errorf("controlled recovery %s: %w", op.ID, e))
				continue
			}
			current, e = c.repo.ControlledPlaybackOperation(op.ID)
			if e != nil {
				allErr = errors.Join(allErr, e)
				continue
			}
			c.remember(current, nil)
			if !current.Fenced {
				allErr = errors.Join(allErr, s.completeControlledPlayback(current))
			}
		}
	}
}
