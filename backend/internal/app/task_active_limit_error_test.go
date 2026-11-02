package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestTaskActiveLimitError(t *testing.T) {
	err := taskActiveLimitError(2)
	if err.Status != 400 || err.Code != 400 || err.Reason != ReasonTaskActiveLimitExceeded || !err.Retryable {
		t.Fatalf("unexpected capacity error: %#v", err)
	}
}

type taskBillingAccountingSnapshot struct {
	tasks                 int64
	billingOrders         int64
	ledgerEntries         int64
	availableMicrocredits int64
}

func captureTaskBillingAccounting(t *testing.T, db *gorm.DB, userID string) taskBillingAccountingSnapshot {
	t.Helper()
	var snap taskBillingAccountingSnapshot
	if err := db.Model(&model.Task{}).Where("user_id = ?", userID).Count(&snap.tasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ?", userID).Count(&snap.billingOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", userID).Count(&snap.ledgerEntries).Error; err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	snap.availableMicrocredits = account.AvailableMicrocredits
	return snap
}

func assertTaskActiveLimitAppError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected active limit error, got nil")
	}
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if appErr.Status != 400 || appErr.Code != 400 || appErr.Reason != ReasonTaskActiveLimitExceeded || !appErr.Retryable {
		t.Fatalf("unexpected AppError fields: %#v", appErr)
	}
}

func TestTaskActiveLimitCreateAndRetryZeroSideEffects(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	policy, err := s.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	limit := policy.Task.ActiveTaskLimit
	if limit <= 0 {
		t.Fatalf("unexpected ActiveTaskLimit: %d", limit)
	}

	// First create one real task and mark it failed + release its billing reservation so it is legally retryable.
	seedTask, err := s.CreateTask("user", creationTextRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", seedTask.ID).Updates(map[string]any{
		"status":           model.TaskStatusFailed,
		"error":            "upstream timeout",
		"billing_order_id": "",
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Insert exactly ActiveTaskLimit queued tasks for the user.
	for i := 0; i < limit; i++ {
		queued := &model.Task{
			ID:     fmt.Sprintf("queued-slot-%d", i),
			UserID: "user",
			Type:   "canvas_text",
			Status: model.TaskStatusQueued,
			Prompt: "slot",
		}
		if err := db.Create(queued).Error; err != nil {
			t.Fatal(err)
		}
	}

	before := captureTaskBillingAccounting(t, db, "user")

	// 1. Pre-check capacity rejection in CreateTask.
	_, err = s.CreateTask("user", creationTextRequest())
	assertTaskActiveLimitAppError(t, err)
	afterCreate := captureTaskBillingAccounting(t, db, "user")
	if afterCreate != before {
		t.Fatalf("CreateTask mutated accounting on active limit: before=%+v after=%+v", before, afterCreate)
	}

	// 2. RetryTask capacity rejection while active tasks remain at limit.
	_, err = s.RetryTask("user", seedTask.ID)
	assertTaskActiveLimitAppError(t, err)
	afterRetry := captureTaskBillingAccounting(t, db, "user")
	if afterRetry != before {
		t.Fatalf("RetryTask mutated accounting on active limit: before=%+v after=%+v", before, afterRetry)
	}
	var reloadedSeed model.Task
	if err := db.First(&reloadedSeed, "id = ?", seedTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedSeed.Status != model.TaskStatusFailed {
		t.Fatalf("expected failed task to remain failed, got %s", reloadedSeed.Status)
	}
}

func TestTaskActiveLimitRepositoryRaceBranchInCreateTask(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	policy, err := s.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	limit := policy.Task.ActiveTaskLimit
	// Fill limit - 1 slots so the pre-check ActiveTaskCountForUser passes.
	for i := 0; i < limit-1; i++ {
		if err := db.Create(&model.Task{
			ID:     fmt.Sprintf("race-slot-%d", i),
			UserID: "user",
			Type:   "canvas_text",
			Status: model.TaskStatusQueued,
			Prompt: "slot",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	before := captureTaskBillingAccounting(t, db, "user")
	var injected atomic.Bool
	var activeCountQueries atomic.Int32
	callbackName := "test:inject_active_task_after_precheck"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "tasks" {
			return
		}
		query := strings.ToLower(tx.Statement.SQL.String())
		if !strings.Contains(query, "count(") || !strings.Contains(query, "status in") {
			return
		}
		if activeCountQueries.Add(1) != 1 {
			return
		}
		count, ok := tx.Statement.Dest.(*int64)
		if !ok || *count != int64(limit-1) {
			t.Fatalf("precheck did not observe the available slot: %#v", tx.Statement.Dest)
		}
		if _, transactional := tx.Statement.ConnPool.(*sql.Tx); transactional {
			t.Fatal("first active-count query should be the service precheck, outside the repository transaction")
		}
		if err := db.Session(&gorm.Session{NewDB: true}).Create(&model.Task{
			ID:     "race-slot-final",
			UserID: "user",
			Type:   "canvas_text",
			Status: model.TaskStatusQueued,
			Prompt: "race",
		}).Error; err != nil {
			t.Fatal(err)
		}
		injected.Store(true)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
	})

	if injected.Load() {
		t.Fatal("race injected before CreateTask reached its capacity precheck")
	}
	_, err = s.CreateTask("user", creationTextRequest())
	if !injected.Load() {
		t.Fatal("expected race injection callback to fire after pre-check")
	}
	if activeCountQueries.Load() != 2 {
		t.Fatalf("expected service precheck and repository admission query, got %d", activeCountQueries.Load())
	}
	assertTaskActiveLimitAppError(t, err)
	after := captureTaskBillingAccounting(t, db, "user")
	// Only the 1 injected task should exist; no task, billing order, ledger entry, or credit deduction from the rejected CreateTask.
	if after.tasks != before.tasks+1 || after.billingOrders != before.billingOrders || after.ledgerEntries != before.ledgerEntries || after.availableMicrocredits != before.availableMicrocredits {
		t.Fatalf("unexpected accounting after repository race rejection: before=%+v after=%+v", before, after)
	}
}
