package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"yingce/backend/internal/model"
)

func TestProfileOwnerLifecycleRetryPreservesContextAndNotifications(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, state := range []string{"40001", "40P01"} {
			t.Run(dialect+"/"+state, func(t *testing.T) {
				a, b := storyboardDatabases(t, dialect)
				if err := a.db.AutoMigrate(&model.Resource{}, &model.Asset{}); err != nil {
					t.Fatal(err)
				}
				if err := a.db.Create(&model.Asset{ID: "retry-asset", UserID: "owner", Title: "original"}).Error; err != nil {
					t.Fatal(err)
				}
				type key struct{}
				notified, calls := 0, 0
				r := a.WithContext(context.WithValue(context.Background(), key{}, "preserved")).WithCanvasChangeNotifier(func(user, canvas string) {
					if user != "owner" || canvas != "canvas" {
						t.Errorf("unexpected notification %s/%s", user, canvas)
					}
					notified++
				})
				err := r.WithStoryboardAssetOwners(r.db.Statement.Context, []string{"owner"}, func(owned *Repository) error {
					calls++
					if owned.db.Statement.Context.Value(key{}) != "preserved" {
						t.Fatal("lost caller context")
					}
					var before model.Asset
					if err := owned.db.First(&before, "id = ?", "retry-asset").Error; err != nil {
						return err
					}
					if before.Title != "original" {
						t.Fatalf("previous attempt did not roll back: %s", before.Title)
					}
					return owned.resourceLifecycleTransaction(func(tx *Repository) error {
						if tx != owned {
							t.Fatal("nested lifecycle replaced repository/hooks")
						}
						tx.canvasChanged("owner", "canvas")
						if notified != 0 {
							t.Fatal("notified before outer commit")
						}
						if err := tx.db.Model(&model.Asset{}).Where("id = ?", "retry-asset").Update("title", "committed").Error; err != nil {
							return err
						}
						if calls == 1 {
							if dialect == "postgres" {
								return tx.db.Exec("DO $$ BEGIN RAISE EXCEPTION 'retry transaction' USING ERRCODE = '" + state + "'; END $$").Error
							}
							return sqlite3.Error{Code: sqlite3.ErrBusy}
						}
						return nil
					})
				})
				if err != nil || calls != 2 || notified != 1 {
					t.Fatalf("whole retry/context/notification: calls=%d notices=%d err=%v", calls, notified, err)
				}
				var actual model.Asset
				if err := b.db.First(&actual, "id = ?", "retry-asset").Error; err != nil || actual.Title != "committed" {
					t.Fatalf("observer=%+v err=%v", actual, err)
				}
				// A non-owner outer transaction still obtains lifecycle locking and receives
				// the exact error, not a converted status or savepoint-local retry.
				expected := &pgconn.PgError{Code: "40P01"}
				nestedCalls := 0
				err = a.Transaction(func(outer *Repository) error {
					return outer.resourceLifecycleTransaction(func(inner *Repository) error {
						nestedCalls++
						if inner != outer {
							t.Fatal("lost external owner")
						}
						return expected
					})
				})
				if !errors.Is(err, expected) || nestedCalls != 1 {
					t.Fatalf("nested error changed: calls=%d err=%v", nestedCalls, err)
				}
			})
		}
	}
}
