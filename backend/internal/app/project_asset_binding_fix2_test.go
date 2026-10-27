package app

import (
	"reflect"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestStoryboardAssetHeadlessLinkExplicitCategory(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, category := range []string{"character", "environment", "prop", "material", ""} {
				t.Run(map[bool]string{true: "omitted", false: category}[category == ""], func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
					asset := model.Asset{ID: "a", UserID: "owner", FolderID: "library", Kind: "video", Category: model.AssetCategoryMaterial, Status: model.AssetVersionStatusConfirmed, Title: "candidate", PayloadJSON: `{"custom":"keep"}`, CreatedAt: stamp, UpdatedAt: stamp}
					for _, row := range []any{&asset, &model.Project{ID: "p", UserID: "owner", Name: "target", Revision: 7, CreatedAt: stamp, UpdatedAt: stamp}, &model.Project{ID: "q", UserID: "owner", Name: "shared", Revision: 12, CreatedAt: stamp, UpdatedAt: stamp}, &model.ProjectAssetLink{ID: "shared-link", ProjectID: "q", AssetID: "a", Position: 4, CreatedAt: stamp}} {
						if err := dbs[0].Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					type state struct {
						Assets   []model.Asset
						Versions []model.AssetVersion
						Links    []model.ProjectAssetLink
						Projects []model.Project
					}
					read := func() state {
						var out state
						for _, dest := range []any{&out.Assets, &out.Versions, &out.Links, &out.Projects} {
							if err := dbs[1].Order("id").Find(dest).Error; err != nil {
								t.Fatal(err)
							}
						}
						return out
					}
					want := read()
					service := &Service{repo: repository.New(dbs[0])}
					started := time.Now().Add(-time.Microsecond)
					result, err := service.LinkProjectAsset("owner", "p", LinkProjectAssetRequest{AssetID: "a", Category: category})
					if err != nil {
						t.Fatal(err)
					}
					wantCategory := model.AssetCategory(category)
					if wantCategory == "" {
						wantCategory = model.AssetCategoryMaterial
					}
					if result.Category != wantCategory || result.PrimaryVersionID == "" || result.VersionCount != 1 {
						t.Fatalf("explicit first-link category lost: want=%s result=%+v", wantCategory, result)
					}
					got := read()
					checkTime := func(at time.Time) {
						if at.Before(started) || at.After(time.Now().Add(time.Microsecond)) {
							t.Fatalf("write timestamp out of bounds: %s", at)
						}
					}
					want.Assets[0].Category = wantCategory
					want.Assets[0].PrimaryVersionID = result.PrimaryVersionID
					if wantCategory != model.AssetCategoryMaterial {
						checkTime(got.Assets[0].UpdatedAt)
						want.Assets[0].UpdatedAt = got.Assets[0].UpdatedAt
						want.Projects[1].Revision++
						checkTime(got.Projects[1].UpdatedAt)
						want.Projects[1].UpdatedAt = got.Projects[1].UpdatedAt
					}
					want.Projects[0].Revision++
					checkTime(got.Projects[0].UpdatedAt)
					want.Projects[0].UpdatedAt = got.Projects[0].UpdatedAt
					if len(got.Versions) != 1 || len(got.Links) != 2 {
						t.Fatalf("unexpected version/link counts: %+v", got)
					}
					checkTime(got.Versions[0].CreatedAt)
					checkTime(got.Versions[0].UpdatedAt)
					want.Versions = []model.AssetVersion{{ID: result.PrimaryVersionID, AssetID: "a", Version: 1, Status: model.AssetVersionStatusConfirmed, DefinitionJSON: "{}", CreatedAt: got.Versions[0].CreatedAt, UpdatedAt: got.Versions[0].UpdatedAt}}
					var target model.ProjectAssetLink
					for _, link := range got.Links {
						if link.ProjectID == "p" {
							target = link
						}
					}
					if target.ID == "" {
						t.Fatal("target link missing")
					}
					checkTime(target.CreatedAt)
					want.Links = append(want.Links, model.ProjectAssetLink{ID: target.ID, ProjectID: "p", AssetID: "a", CreatedAt: target.CreatedAt})
					if want.Links[0].ID > want.Links[1].ID {
						want.Links[0], want.Links[1] = want.Links[1], want.Links[0]
					}
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("unexpected full row changes:\nwant=%+v\ngot=%+v", want, got)
					}
					filtered, err := service.FilterProjectAssets("owner", "p", ProjectAssetFilter{Category: string(wantCategory)})
					if err != nil || len(filtered) != 1 || filtered[0].ID != "a" {
						t.Fatalf("category discovery failed: %+v %v", filtered, err)
					}
					preview, err := buildStoryboardAssetPreview("owner", got.Projects[0], "u", []string{"s"}, repository.StoryboardAssetSnapshot{
						Assets: got.Assets, Versions: got.Versions, Links: []model.ProjectAssetLink{target},
						Shots: []model.Shot{{ID: "s", ProjectID: "p", UnitID: "u", Description: "candidate"}},
					})
					if err != nil || len(preview.Shots) != 1 {
						t.Fatalf("binding preview failed: %+v %v", preview, err)
					}
					candidates := preview.Shots[0].Candidates
					if wantCategory == model.AssetCategoryMaterial {
						if len(candidates) != 0 {
							t.Fatal("material entered binding candidates")
						}
					} else if len(candidates) != 1 || candidates[0].AssetID != "a" || candidates[0].Category != BindingCategory(wantCategory) || candidates[0].AssetVersionID == nil || *candidates[0].AssetVersionID != result.PrimaryVersionID {
						t.Fatalf("persisted category/version missing from binding candidates: %+v", candidates)
					}
					for _, replayCategory := range []string{category, ""} {
						if _, err := service.LinkProjectAsset("owner", "p", LinkProjectAssetRequest{AssetID: "a", Category: replayCategory}); err != nil {
							t.Fatal(err)
						}
						if after := read(); !reflect.DeepEqual(got, after) {
							t.Fatal("replay changed complete persisted rows")
						}
					}
				})
			}
		})
	}
}

func TestStoryboardAssetLinkInitialVersionCallers(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, mode := range []string{"uploaded_resource", "headless_asset"} {
				t.Run(mode, func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					project := model.Project{ID: "p", UserID: "owner", Name: "initial-link", Revision: 1}
					if err := dbs[0].Create(&project).Error; err != nil {
						t.Fatal(err)
					}
					if mode == "uploaded_resource" {
						if err := dbs[0].Create(&model.Resource{ID: "a", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4", Width: 1920, Height: 1080}).Error; err != nil {
							t.Fatal(err)
						}
					} else {
						if err := dbs[0].Create(&model.Asset{ID: "a", UserID: "owner", Kind: "video", Category: model.AssetCategoryMaterial, Status: model.AssetVersionStatusConfirmed, PayloadJSON: "{}"}).Error; err != nil {
							t.Fatal(err)
						}
					}
					service := &Service{repo: repository.New(dbs[0])}
					result, err := service.LinkProjectAsset("owner", "p", LinkProjectAssetRequest{AssetID: "a", Title: "first"})
					if err != nil {
						t.Fatal(err)
					}
					if result.ID != "a" || result.PrimaryVersionID == "" || result.VersionCount != 1 || result.Category != model.AssetCategoryMaterial || result.Status != model.AssetVersionStatusConfirmed {
						t.Fatalf("initial link lost asset domain or version: %+v", result)
					}
					var asset model.Asset
					var versions []model.AssetVersion
					var links []model.ProjectAssetLink
					for _, query := range []struct {
						dest  any
						where string
						arg   string
					}{{&asset, "id = ?", "a"}, {&versions, "asset_id = ?", "a"}, {&links, "project_id = ?", "p"}} {
						if err := dbs[1].Where(query.where, query.arg).Find(query.dest).Error; err != nil {
							t.Fatal(err)
						}
					}
					if len(versions) != 1 || len(links) != 1 || versions[0].ID != asset.PrimaryVersionID || asset.PrimaryVersionID != result.PrimaryVersionID || versions[0].Version != 1 || links[0].AssetID != "a" {
						t.Fatal("initial link did not persist one matching version and link")
					}
					replay, err := service.LinkProjectAsset("owner", "p", LinkProjectAssetRequest{AssetID: "a"})
					if err != nil {
						t.Fatal(err)
					}
					if replay.PrimaryVersionID != result.PrimaryVersionID || replay.VersionCount != 1 {
						t.Fatal("link replay replaced initial version")
					}
					current, err := repository.New(dbs[1]).ProjectForUser("owner", "p")
					if err != nil {
						t.Fatal(err)
					}
					if current.Revision != 2 {
						t.Fatalf("link replay changed project revision: %d", current.Revision)
					}
				})
			}
		})
	}
}
