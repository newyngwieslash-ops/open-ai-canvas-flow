package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestStoryboardAssetConcurrencyCandidateUnlinkOrders(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} { t.Run(dialect, func(t *testing.T) {
		for _, first := range []string{"unlink", "candidate"} { t.Run(first, func(t *testing.T) {
			dbs:=profileConcurrencyDatabases(t,dialect)
			if err:=database.MigrateSchema(dbs[0]);err!=nil{t.Fatal(err)}
			for _,row:=range []any{&model.Project{ID:"p",UserID:"owner",Name:"candidate",Revision:1}, &model.Project{ID:"q",UserID:"owner",Name:"shared",Revision:4}, &model.Asset{ID:"a",UserID:"owner",Kind:"entity",Category:model.AssetCategoryCharacter,Title:"Actor",PrimaryVersionID:"v",PayloadJSON:"{}"}, &model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:`{"name":"Actor"}`}, &model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}, &model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"}, &model.ProjectAssetCandidate{ID:"c",ProjectID:"p",Name:"Actor",Category:model.AssetCategoryCharacter,DetailsJSON:`{"name":"Actor"}`,Status:"pending_confirmation"}, &model.ProjectUnit{ID:"u",ProjectID:"p"}, &model.Shot{ID:"s",ProjectID:"p",UnitID:"u",Description:"untouched script"}, &model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s",Status:"ready",Selected:true}}{if err:=dbs[0].Create(row).Error;err!=nil{t.Fatal(err)}}
			snapshot:=func() repository.StoryboardAssetSnapshot {t.Helper();s,err:=repository.New(dbs[1]).ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{t.Fatal(err)};return s}
			before:=snapshot()
			arrived,release:=make(chan struct{}),make(chan struct{});var once,releaseOnce sync.Once;defer releaseOnce.Do(func(){close(release)})
			if err:=dbs[0].Callback().Raw().Before("gorm:raw").Register("candidate_unlink_order",func(tx *gorm.DB){sql:=tx.Statement.SQL.String();if !strings.Contains(sql,"pg_advisory_xact_lock")&&!strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0"){return};once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})});err!=nil{t.Fatal(err)}
			write:=func(db *gorm.DB,kind string)error{s:=&Service{repo:repository.New(db)};if kind=="unlink"{return s.UnlinkProjectAsset("owner","p","a")};_,err:=s.ConfirmProjectAssetCandidate("owner","p","c",ConfirmProjectAssetCandidateRequest{AssetID:"a"});return err}
			second:="candidate";if first=="candidate"{second="unlink"}
			done:=make(chan error,1);go func(){done<-write(dbs[0],second)}();select{case <-arrived:case <-time.After(5*time.Second):t.Fatal("missing owner barrier")}
			firstErr:=write(dbs[1],first);releaseOnce.Do(func(){close(release)});secondErr:=<-done
			if firstErr!=nil{t.Fatal(firstErr)};if first=="unlink"{if secondErr==nil{t.Fatal("candidate survived unlink")}}else if secondErr!=nil{t.Fatal(secondErr)}
			after:=snapshot();if !reflect.DeepEqual(before.Shots,after.Shots)||!reflect.DeepEqual(before.Revisions,after.Revisions)||!reflect.DeepEqual(before.References,after.References)||!reflect.DeepEqual(before.Artifacts,after.Artifacts){t.Fatal("candidate/unlink altered script/reference/artifact baseline")}
			var candidate model.ProjectAssetCandidate;if err:=dbs[1].First(&candidate,"id = ?","c").Error;err!=nil{t.Fatal(err)}
			versions,err:=repository.New(dbs[1]).AssetVersions("a");if err!=nil{t.Fatal(err)}
			wantVersions:=1;wantStatus:="pending_confirmation";wantQ:=uint64(4);if first=="candidate"{wantVersions=2;wantStatus="confirmed";wantQ=5}
			if len(versions)!=wantVersions||candidate.Status!=wantStatus{t.Fatalf("wrong candidate/version state: %s/%d",candidate.Status,len(versions))}
			q,err:=repository.New(dbs[1]).ProjectForUser("owner","q");if err!=nil{t.Fatal(err)};if uint64(q.Revision)!=wantQ{t.Fatalf("shared revision %d want %d",q.Revision,wantQ)}
			linked,err:=repository.New(dbs[1]).ProjectAssetLinked("p","a");if err!=nil||linked{t.Fatalf("unlink not committed: %v %v",linked,err)}
		}) }
	}) }
}

func TestStoryboardAssetConcurrencySharedWorkflowRevision(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		dbs:=profileConcurrencyDatabases(t,dialect)
		if err:=dbs[0].AutoMigrate(&model.ProjectUnit{},&model.ShotRevision{},&model.ShotArtifact{},&model.WorkflowInstance{},&model.WorkflowStepInstance{},&model.WorkflowStepTask{},&model.ProductionTaskLink{});err!=nil{t.Fatal(err)}
		for _,row:=range []any{&model.Project{ID:"p",UserID:"owner",Name:"first",Revision:1},&model.Project{ID:"q",UserID:"owner",Name:"second",Revision:4},&model.Asset{ID:"a",UserID:"owner",Category:model.AssetCategoryProp,PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"},&model.WorkflowInstance{ID:"wf",ProjectID:"p",Revision:1},&model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",StepKey:"assets",Status:model.WorkflowStepStatusRunning},&model.Task{ID:"task",UserID:"owner",ProjectID:"p",Status:model.TaskStatusSucceeded,ResultJSON:"{}"},&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady}}{if err:=dbs[0].Create(row).Error;err!=nil{t.Fatal(err)}}
		locked:=[]string{}
		if dialect=="postgres"{if err:=dbs[0].Callback().Query().After("gorm:query").Register("workflow_first_locks",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{locked=append(locked,tx.Statement.Table)}});err!=nil{t.Fatal(err)}}
		repo:=repository.New(dbs[0]);if _,err:=(&Service{repo:repo}).RegisterTaskOutput("owner","p","step",RegisterTaskOutputRequest{TaskID:"task",AssetVersionID:"v",ResourceID:"image",MediaType:"image",Role:"reference"});err!=nil{t.Fatal(err)}
		for id,want:=range map[string]int64{"p":2,"q":5}{p,err:=repo.ProjectForUser("owner",id);if err!=nil{t.Fatal(err)};if int64(p.Revision)!=want{t.Errorf("shared workflow project %s revision=%d want %d",id,p.Revision,want)}}
		if dialect=="postgres"{want:=[]string{"projects","project_units","shots","project_asset_links","assets","asset_versions"};if len(locked)<len(want)||!reflect.DeepEqual(locked[:min(len(locked),len(want))],want){t.Fatalf("workflow outer prelocked asset before shot scope: %v",locked)}}
	})}
}

func TestStoryboardAssetConcurrencySharedCategoryRevision(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		dbs:=profileConcurrencyDatabases(t,dialect)
		for _,row:=range []any{&model.Project{ID:"p",UserID:"owner",Name:"first",Revision:1},&model.Project{ID:"q",UserID:"owner",Name:"second",Revision:4},&model.Asset{ID:"a",UserID:"owner",Category:model.AssetCategoryProp,PayloadJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"}}{if err:=dbs[0].Create(row).Error;err!=nil{t.Fatal(err)}}
		category:="environment";repo:=repository.New(dbs[0]);if _,err:=(&Service{repo:repo}).UpdateProjectAsset("owner","p","a",UpdateProjectAssetRequest{Category:&category});err!=nil{t.Fatal(err)}
		for id,want:=range map[string]int64{"p":2,"q":5}{p,err:=repo.ProjectForUser("owner",id);if err!=nil{t.Fatal(err)};if int64(p.Revision)!=want{t.Errorf("shared category project %s revision=%d want %d",id,p.Revision,want)}}
	})}
}

func TestStoryboardAssetConcurrencyWorkflowRechecks(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		for _,mode:=range []string{"workflow_after_shot_delete","revision_change"}{t.Run(mode,func(t *testing.T){
			dbs:=profileConcurrencyDatabases(t,dialect)
			if err:=dbs[0].AutoMigrate(&model.ProjectUnit{},&model.ProjectAssetCandidate{},&model.ShotRevision{},&model.ShotArtifact{},&model.WorkflowInstance{},&model.WorkflowStepInstance{},&model.WorkflowStepTask{},&model.ProductionTaskLink{});err!=nil{t.Fatal(err)}
			for _,row:=range []any{&model.Project{ID:"p",UserID:"owner",Name:"workflow",Revision:1},&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u",CurrentRevisionID:"r1"},&model.ShotRevision{ID:"r1",ShotID:"s",Version:1},&model.WorkflowInstance{ID:"wf",ProjectID:"p",UnitID:"u",Revision:1},&model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",StepKey:"video",Status:model.WorkflowStepStatusRunning},&model.Task{ID:"task",UserID:"owner",ProjectID:"p",Status:model.TaskStatusSucceeded,ResultJSON:"{}"},&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady}}{if err:=dbs[0].Create(row).Error;err!=nil{t.Fatal(err)}}
			arrived,release:=make(chan struct{}),make(chan struct{});var once,releaseOnce sync.Once;defer releaseOnce.Do(func(){close(release)})
			if err:=dbs[0].Callback().Raw().Before("gorm:raw").Register("workflow_recheck",func(tx *gorm.DB){sql:=tx.Statement.SQL.String();if !strings.Contains(sql,"pg_advisory_xact_lock")&&!strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0"){return};once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})});err!=nil{t.Fatal(err)}
			done:=make(chan error,1);go func(){_,err:=(&Service{repo:repository.New(dbs[0])}).RegisterTaskOutput("owner","p","step",RegisterTaskOutputRequest{TaskID:"task",ShotID:"s",ResourceID:"image",ArtifactType:"image"});done<-err}()
			select{case <-arrived:case <-time.After(5*time.Second):t.Fatal("no owner barrier")}
			b:=repository.New(dbs[1]);var err error
			if mode=="workflow_after_shot_delete"{err=b.DeleteProjectShot("p","s",time.Now())}else{shot,e:=b.ShotForProject("p","s");if e!=nil{t.Fatal(e)};err=b.SaveShotWithRevision(shot,&model.ShotRevision{ID:"r2",ShotID:"s"},false)}
			releaseOnce.Do(func(){close(release)});result:=<-done;if err!=nil{t.Fatal(err)}
			var artifacts []model.ShotArtifact;if err:=dbs[1].Find(&artifacts).Error;err!=nil{t.Fatal(err)}
			if mode=="workflow_after_shot_delete"{if result==nil||len(artifacts)!=0{t.Fatalf("deleted shot registered artifact: result=%v count=%d",result,len(artifacts))}}else{if result!=nil{t.Fatal(result)};if len(artifacts)!=1||artifacts[0].RevisionID!="r2"{t.Fatalf("workflow used stale revision: %+v",artifacts)}}
		})}
	})}
}

func TestStoryboardAssetConcurrencyStaleAssetWrites(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"link_after_version_commit", "representations_after_category_commit"} {
				t.Run(method, func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					if err := dbs[0].AutoMigrate(&model.ProjectAssetFolder{}); err != nil { t.Fatal(err) }
					for _, row := range []any{
						&model.Project{ID:"p",UserID:"owner",Name:"stale",Revision:1},
						&model.Asset{ID:"a",UserID:"owner",Kind:"entity",Category:model.AssetCategoryProp,PrimaryVersionID:"v",PayloadJSON:"{}"},
						&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},
						&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady},
					} { if err := dbs[0].Create(row).Error; err != nil { t.Fatal(err) } }
					if method != "link_after_version_commit" { if err := dbs[0].Create(&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}).Error; err != nil {t.Fatal(err)} }
					arrived, release := make(chan struct{}), make(chan struct{})
					var once, releaseOnce sync.Once
					defer releaseOnce.Do(func(){close(release)})
					if err := dbs[0].Callback().Raw().Before("gorm:raw").Register("stale_asset_barrier",func(tx *gorm.DB){
						sql := tx.Statement.SQL.String()
						if !strings.Contains(sql,"pg_advisory_xact_lock") && !strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0") {return}
						once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})
					}); err != nil {t.Fatal(err)}
					svc := &Service{repo:repository.New(dbs[0])}
					done := make(chan error,1)
					go func(){var err error;if method == "link_after_version_commit" {_,err=svc.LinkProjectAsset("owner","p",LinkProjectAssetRequest{AssetID:"a"})} else {_,err=svc.ReplaceProjectAssetRepresentations("owner","p","a",ReplaceAssetRepresentationsRequest{Representations:[]CharacterRepresentationInput{{ResourceID:"image"}}})};done<-err}()
					select{case <-arrived:case <-time.After(5*time.Second):t.Fatal("writer missed owner barrier")}
					b:=repository.New(dbs[1])
					if method == "link_after_version_commit" {
						asset,err:=b.AssetForUser("owner","a");if err!=nil{t.Fatal(err)};asset.PrimaryVersionID="v2";asset.Status=model.AssetVersionStatusConfirmed
						if err:=b.SaveCharacterVersion("",asset,&model.AssetVersion{ID:"v2",AssetID:"a",Version:2,DefinitionJSON:"{}"},nil,nil,"v");err!=nil{t.Fatal(err)}
					} else {category:="environment";if _,err:=(&Service{repo:b}).UpdateProjectAsset("owner","p","a",UpdateProjectAssetRequest{Category:&category});err!=nil{t.Fatal(err)}}
					releaseOnce.Do(func(){close(release)})
					if err:=<-done;err!=nil{t.Fatal(err)}
					asset,err:=b.AssetForUser("owner","a");if err!=nil{t.Fatal(err)}
					if method=="link_after_version_commit" && asset.PrimaryVersionID!="v2" {t.Fatalf("link reverted primary version to %s",asset.PrimaryVersionID)}
					if method=="representations_after_category_commit" && asset.Category!=model.AssetCategoryEnvironment {t.Fatalf("representation reverted category to %s",asset.Category)}
					versions,err:=b.AssetVersions("a");if err!=nil{t.Fatal(err)};if len(versions)!=2{t.Fatalf("versions=%d",len(versions))}
					linked,err:=b.ProjectAssetLinked("p","a");if err!=nil||!linked{t.Fatalf("link=%v err=%v",linked,err)}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyAssetRechecks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && os.Getenv("CANVAS_TEST_POSTGRES_DSN") == "" {
				t.Fatal("isolated PostgreSQL required")
			}
			for _, method := range []string{"category", "representations"} {
				t.Run(method, func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					for _, row := range []any{&model.Project{ID: "p", UserID: "owner", Name: "asset", Revision: 1}, &model.Asset{ID: "a", UserID: "owner", Category: model.AssetCategoryProp, PrimaryVersionID: "v", PayloadJSON: "{}"}, &model.AssetVersion{ID: "v", AssetID: "a", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "l", ProjectID: "p", AssetID: "a"}, &model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady}} {
						if err := dbs[0].Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					arrived, release := make(chan struct{}), make(chan struct{})
					var once, releaseOnce sync.Once
					defer releaseOnce.Do(func() { close(release) })
					if err := dbs[0].Callback().Raw().Before("gorm:raw").Register("asset_recheck_barrier", func(tx *gorm.DB) {
						sql := tx.Statement.SQL.String()
						if !strings.Contains(sql, "pg_advisory_xact_lock") && !strings.Contains(sql, "UPDATE assets SET id = id WHERE 1 = 0") {
							return
						}
						once.Do(func() {
							close(arrived)
							select {
							case <-release:
							case <-time.After(5 * time.Second):
								tx.AddError(context.DeadlineExceeded)
							}
						})
					}); err != nil {
						t.Fatal(err)
					}
					svc := &Service{repo: repository.New(dbs[0])}
					done := make(chan error, 1)
					go func() {
						var err error
						if method == "category" {
							category := "environment"
							_, err = svc.UpdateProjectAsset("owner", "p", "a", UpdateProjectAssetRequest{Category: &category})
						} else {
							_, err = svc.ReplaceProjectAssetRepresentations("owner", "p", "a", ReplaceAssetRepresentationsRequest{Representations: []CharacterRepresentationInput{{ResourceID: "image"}}})
						}
						done <- err
					}()
					select {
					case <-arrived:
					case <-time.After(5 * time.Second):
						t.Fatal("asset writer did not reach owner barrier")
					}
					b := repository.New(dbs[1])
					err := b.DeleteProjectAssetLink("p", "a")
					releaseOnce.Do(func() { close(release) })
					result := <-done
					if err != nil {
						t.Fatal(err)
					}
					if result == nil {
						t.Fatal("asset writer accepted link removed before its owner lock")
					}
					asset, err := b.AssetForUser("owner", "a")
					if err != nil {
						t.Fatal(err)
					}
					if asset.Category != model.AssetCategoryProp || asset.PrimaryVersionID != "v" {
						t.Fatal("detached asset mutated despite rejected write")
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyShotAuthorization(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && os.Getenv("CANVAS_TEST_POSTGRES_DSN") == "" {
				t.Fatal("isolated PostgreSQL required")
			}
			for _, method := range []string{"create", "replace", "delete", "link", "unlink"} {
				t.Run(method, func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					if err := database.MigrateSchema(dbs[0]); err != nil {
						t.Fatal(err)
					}
					for _, row := range []any{&model.Project{ID: "p", UserID: "owner", Name: "auth", Revision: 1}, &model.ProjectUnit{ID: "u", ProjectID: "p"}, &model.Shot{ID: "shot", ProjectID: "p", UnitID: "u", Description: "plot"}, &model.Asset{ID: "a", UserID: "owner"}, &model.AssetVersion{ID: "v", AssetID: "a", Version: 1}, &model.ProjectAssetLink{ID: "l", ProjectID: "p", AssetID: "a"}, &model.ShotAssetReference{ID: "ref", ShotID: "shot", AssetVersionID: "v", Role: "reference"}} {
						if err := dbs[0].Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					arrived, release := make(chan struct{}), make(chan struct{})
					var once, releaseOnce sync.Once
					defer releaseOnce.Do(func() { close(release) })
					if err := dbs[0].Callback().Raw().Before("gorm:raw").Register("shot_auth_barrier", func(tx *gorm.DB) {
						sql := tx.Statement.SQL.String()
						if !strings.Contains(sql, "pg_advisory_xact_lock") && !strings.Contains(sql, "UPDATE assets SET id = id WHERE 1 = 0") {
							return
						}
						once.Do(func() {
							close(arrived)
							select {
							case <-release:
							case <-time.After(5 * time.Second):
								tx.AddError(context.DeadlineExceeded)
							}
						})
					}); err != nil {
						t.Fatal(err)
					}
					svc := &Service{repo: repository.New(dbs[0])}
					done := make(chan error, 1)
					go func() {
						var err error
						switch method {
						case "create":
							_, err = svc.CreateProjectShot("owner", "p", CreateProjectShotRequest{UnitID: "u", Title: "new", Description: "plot"})
						case "replace":
							_, err = svc.ReplaceProjectUnitShots("owner", "p", "u", ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"shot"}, Shots: []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "new", Description: "plot"}}}})
						case "delete":
							err = svc.DeleteProjectShot("owner", "p", "shot")
						case "link":
							_, err = svc.LinkShotAsset("owner", "p", "shot", LinkShotAssetRequest{AssetVersionID: "v", Role: "reference"})
						default:
							err = svc.UnlinkShotAsset("owner", "p", "shot", "ref")
						}
						done <- err
					}()
					select {
					case <-arrived:
					case <-time.After(5 * time.Second):
						t.Fatal("writer did not reach owner barrier")
					}
					b := repository.New(dbs[1])
					p, err := b.ProjectForUser("owner", "p")
					if err != nil {
						t.Fatal(err)
					}
					p.Status = model.ProjectStatusArchived
					err = b.UpdateProject(p)
					releaseOnce.Do(func() { close(release) })
					result := <-done
					if err != nil {
						t.Fatal(err)
					}
					if result == nil {
						t.Fatal("writer accepted project archived before its owner lock")
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyRevisionRechecks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && os.Getenv("CANVAS_TEST_POSTGRES_DSN") == "" {
				t.Fatal("isolated PostgreSQL required")
			}
			dbs := profileConcurrencyDatabases(t, dialect)
			if err := dbs[0].AutoMigrate(&model.ProjectUnit{}, &model.ShotRevision{}, &model.ShotArtifact{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}); err != nil {
				t.Fatal(err)
			}
			for _, row := range []any{&model.Project{ID: "p", UserID: "owner", Name: "revision", Status: "active", Revision: 1}, &model.Shot{ID: "shot", ProjectID: "p", Title: "old", Description: "plot", DurationMs: 1000}} {
				if err := dbs[0].Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			arrived, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			if err := dbs[0].Callback().Raw().Before("gorm:raw").Register("revision_owner_barrier", func(tx *gorm.DB) {
				sql := tx.Statement.SQL.String()
				if !strings.Contains(sql, "pg_advisory_xact_lock") && !strings.Contains(sql, "UPDATE assets SET id = id WHERE 1 = 0") {
					return
				}
				once.Do(func() {
					close(arrived)
					select {
					case <-release:
					case <-time.After(5 * time.Second):
						tx.AddError(context.DeadlineExceeded)
					}
				})
			}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, _, err := (&Service{repo: repository.New(dbs[0])}).CreateShotRevision("owner", "p", "shot", ShotRevisionInput{PlotDescription: "new script"})
				done <- err
			}()
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("revision did not reach owner barrier")
			}
			// Competing direct Shot field writer commits before revision acquires owner.
			b := repository.New(dbs[1])
			shot, err := b.ShotForProject("p", "shot")
			if err != nil {
				t.Fatal(err)
			}
			shot.Title = "concurrent title"
			err = b.SaveShot(shot, false)
			releaseOnce.Do(func() { close(release) })
			result := <-done
			if err != nil {
				t.Fatal(err)
			}
			if result != nil {
				t.Fatal(result)
			}
			current, err := b.ShotForProject("p", "shot")
			if err != nil {
				t.Fatal(err)
			}
			if current.Title != "concurrent title" {
				t.Fatal("revision overwrote Shot fields read before owner lock")
			}
		})
	}
}

func TestStoryboardAssetConcurrencyImageOuter(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && os.Getenv("CANVAS_TEST_POSTGRES_DSN") == "" {
				t.Fatal("isolated PostgreSQL required")
			}
			dbs := profileConcurrencyDatabases(t, dialect)
			if dialect == "sqlite" {
				if err := dbs[1].Exec("PRAGMA busy_timeout = 100").Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, row := range []any{
				&model.Project{ID: "p", UserID: "owner", Name: "image", Revision: 1},
				&model.Project{ID: "shared", UserID: "other-owner", Name: "shared", Revision: 1},
				&model.Asset{ID: "a", UserID: "owner", Category: model.AssetCategoryProp, Kind: "entity", PrimaryVersionID: "v", PayloadJSON: "{}"},
				&model.AssetVersion{ID: "v", AssetID: "a", Version: 1, DefinitionJSON: "{}"},
				&model.ProjectAssetLink{ID: "l", ProjectID: "p", AssetID: "a"},
				&model.ProjectAssetLink{ID: "s", ProjectID: "shared", AssetID: "a"},
				&model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady, Width: 10, Height: 10, MimeType: "image/png"},
			} {
				if err := dbs[0].Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := repository.New(dbs[0]).WithStoryboardAssetOwners(context.Background(), []string{"other-owner"}, func(*repository.Repository) error {
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
				svc := &Service{repo: repository.New(dbs[1]).WithContext(ctx)}
				_, err := svc.bindProjectAssetImage("owner", "p", "a", BindProjectAssetImageRequest{ResourceID: "image", ExpectedVersionID: "v"}, "op")
				if err == nil {
					t.Error("image app outer bypassed linked project owner")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			svc := &Service{repo: repository.New(dbs[1])}
			if _, err := svc.bindProjectAssetImage("owner", "p", "a", BindProjectAssetImageRequest{ResourceID: "image", ExpectedVersionID: "v"}, "op"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoryboardAssetConcurrencyUnlinkRechecks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && os.Getenv("CANVAS_TEST_POSTGRES_DSN") == "" {
				t.Fatal("isolated PostgreSQL required; no SKIP acceptance")
			}
			for _, first := range []string{"confirmation", "unlink"} {
				t.Run(first, func(t *testing.T) {
					dbs := profileConcurrencyDatabases(t, dialect)
					if err:=dbs[0].AutoMigrate(&model.ProjectUnit{},&model.ShotRevision{},&model.ShotArtifact{});err!=nil{t.Fatal(err)}
					a, b := repository.New(dbs[0]), repository.New(dbs[1])
					for _, row := range []any{
						&model.Project{ID: "p", UserID: "owner", Name: "unlink", Revision: 1},
						&model.Asset{ID: "a", UserID: "owner", Category: model.AssetCategoryProp},
						&model.AssetVersion{ID: "v", AssetID: "a", Version: 1},
						&model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "a"},
						&model.Shot{ID: "shot", ProjectID: "p", UnitID: "u"},
					} {
						if err := dbs[0].Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					svc := &Service{repo: a}
					if first == "unlink" {
						if err := svc.UnlinkProjectAsset("owner", "p", "a"); err != nil {
							t.Fatal(err)
						}
						// Simulated confirmation must re-read the linked version after taking its lock.
						err := b.WithStoryboardAssetOwners(context.Background(), []string{"owner"}, func(tx *repository.Repository) error {
							_, err := tx.AssetVersionForProject("p", "v")
							return err
						})
						if err == nil {
							t.Fatal("confirmation accepted an unlinked version")
						}
						return
					}
					arrived, release := make(chan struct{}), make(chan struct{})
					var once, releaseOnce sync.Once
					defer releaseOnce.Do(func() { close(release) })
					if err := dbs[0].Callback().Raw().Before("gorm:raw").Register("unlink_owner_barrier", func(tx *gorm.DB) {
						sql := tx.Statement.SQL.String()
						if !strings.Contains(sql, "pg_advisory_xact_lock") && !strings.Contains(sql, "UPDATE assets SET id = id WHERE 1 = 0") {
							return
						}
						once.Do(func() {
							close(arrived)
							select {
							case <-release:
							case <-time.After(5 * time.Second):
								tx.AddError(context.DeadlineExceeded)
							}
						})
					}); err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { done <- svc.UnlinkProjectAsset("owner", "p", "a") }()
					select {
					case <-arrived:
					case <-time.After(5 * time.Second):
						t.Fatal("unlink did not reach coordination barrier")
					}
					// Only the protected insert is simulated; this is not full append acceptance.
					err := b.WithStoryboardAssetOwners(context.Background(), []string{"owner"}, func(tx *repository.Repository) error {
						return tx.UpsertShotAssetReference(&model.ShotAssetReference{ID: "ref", ShotID: "shot", AssetVersionID: "v", Role: "visual"})
					})
					releaseOnce.Do(func() { close(release) })
					result := <-done
					if err != nil {
						t.Fatal(err)
					}
					if result == nil {
						t.Fatal("unlink used pre-lock reference count and removed a newly referenced link")
					}
					linked, err := b.ProjectAssetLinked("p", "a")
					if err != nil || !linked {
						t.Fatalf("protected link missing: %v", err)
					}
					p, err := b.ProjectForUser("owner", "p")
					if err != nil {
						t.Fatal(err)
					}
					if p.Revision != 1 {
						t.Fatalf("rejected unlink changed revision: %d", p.Revision)
					}
				})
			}
		})
	}
}

func seedSettledLocalResourceForLegacyBarrier(t *testing.T, repo *repository.Repository, dataDir, userID, resourceID string) {
	t.Helper()
	objKey := resourceID + ".png"
	fullPath := filepath.Join(dataDir, "resources", objKey)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatal(err)
	}
	res := model.Resource{
		ID:        resourceID,
		UserID:    userID,
		Provider:  "local",
		ObjectKey: objKey,
		Kind:      "image",
		MimeType:  "image/png",
		Width:     64,
		Height:    64,
		Size:      16,
		Status:    model.ResourceStatusPending,
	}
	op, err := repo.BeginResourceProduction(&res, "original", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte("legacy-fixture!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	res.Status = model.ResourceStatusReady
	if err := repo.SaveResource(&res); err != nil {
		t.Fatal(err)
	}
}

func TestStoryboardAssetConcurrencyLegacyAppendAllEntryOrders(t *testing.T) {
	methods := []string{
		"script_revision",
		"reference_link",
		"reference_unlink",
		"shot_replace",
		"shot_delete",
		"asset_unlink",
		"version_switch_shared",
		"shared_category_update",
		"admin_target_owner_resource_delete",
		"resource_delete_outbox",
		"resource_purge_outbox",
	}
	for _, dialect := range []string{"sqlite", "postgres"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			if dialect == "postgres" && strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN")) == "" {
				t.Fatal("isolated PostgreSQL required; no SKIP acceptance")
			}
			for _, method := range methods {
				method := method
				t.Run(method, func(t *testing.T) {
					for _, first := range []string{"competitor", "append"} {
						first := first
						t.Run(first, func(t *testing.T) {
							dbs := profileConcurrencyDatabases(t, dialect)
							if err := database.MigrateSchema(dbs[0]); err != nil {
								t.Fatal(err)
							}
							dataDir := t.TempDir()
							repo0 := repository.New(dbs[0])
							repo1 := repository.New(dbs[1])
							svc0 := &Service{repo: repo0, dataDir: dataDir}
							svc1 := &Service{repo: repo1, dataDir: dataDir}

							seedRows := []any{
								&model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
								&model.User{ID: "co-owner", Username: "co-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
								&model.User{ID: "res-owner", Username: "res-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
								&model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive},
								&model.Project{ID: "p", UserID: "owner", Name: "legacy-p", Status: "active", Revision: 10},
								&model.Project{ID: "q", UserID: "co-owner", Name: "shared-q", Status: "active", Revision: 4},
								&model.ProjectUnit{ID: "unit", ProjectID: "p", Title: "u1"},

								&model.Shot{ID: "s1", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "r1", Title: "s1", Description: "角色：阿青\n道具：玉佩", DurationMs: 4000, Status: "ready", Position: 1},
								&model.Shot{ID: "s2", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "r2", Title: "s2", Description: "道具：青钢剑", DurationMs: 4000, Status: "ready", Position: 2},
								&model.Shot{ID: "s3", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "r3", Title: "s3", Description: "阿青缓步走过长廊", DurationMs: 3000, Status: "ready", Position: 3},

								&model.ShotRevision{ID: "r1", ShotID: "s1", Version: 1, PlotDescription: "角色：阿青\n道具：玉佩", Dialogue: "阿青：走", ActionBeatsJSON: `[]`},
								&model.ShotRevision{ID: "r2", ShotID: "s2", Version: 1, PlotDescription: "道具：青钢剑", Dialogue: "", ActionBeatsJSON: `[]`},
								&model.ShotRevision{ID: "r3", ShotID: "s3", Version: 1, PlotDescription: "阿青缓步走过长廊", Dialogue: "", ActionBeatsJSON: `[]`},

								&model.Asset{ID: "a", UserID: "owner", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "阿青", Status: "draft", PrimaryVersionID: "v-old", PayloadJSON: `{}`},
								&model.Asset{ID: "b", UserID: "owner", Category: model.AssetCategoryProp, Kind: "image", Title: "青钢剑", Status: "draft", PrimaryVersionID: "vb1", PayloadJSON: `{}`},
								&model.Asset{ID: "c", UserID: "owner", Category: model.AssetCategoryProp, Kind: "entity", Title: "玉佩", Status: "draft", PrimaryVersionID: "vc1", PayloadJSON: `{}`},
								&model.Asset{ID: "d", UserID: "owner", Category: model.AssetCategoryEnvironment, Kind: "image", Title: "古亭", Status: "draft", PrimaryVersionID: "vd1", PayloadJSON: `{}`},
								&model.Asset{ID: "e-res-owner", UserID: "res-owner", Category: model.AssetCategoryProp, Kind: "image", Title: "共享目标资产", Status: "draft", PayloadJSON: `{}`},

								&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"},
								&model.ProjectAssetLink{ID: "link-b", ProjectID: "p", AssetID: "b"},
								&model.ProjectAssetLink{ID: "link-c", ProjectID: "p", AssetID: "c"},
								&model.ProjectAssetLink{ID: "link-c-shared", ProjectID: "q", AssetID: "c"},
								&model.ProjectAssetLink{ID: "link-d", ProjectID: "p", AssetID: "d"},
								&model.ProjectAssetLink{ID: "link-e-p", ProjectID: "p", AssetID: "e-res-owner"},
								&model.ProjectAssetLink{ID: "link-e-q", ProjectID: "q", AssetID: "e-res-owner"},

								&model.AssetVersion{ID: "v-old", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{}`},
								&model.AssetVersion{ID: "vb1", AssetID: "b", Version: 1, Status: "draft", DefinitionJSON: `{}`},
								&model.AssetVersion{ID: "vc1", AssetID: "c", Version: 1, Status: "draft", DefinitionJSON: `{}`},
								&model.AssetVersion{ID: "vd1", AssetID: "d", Version: 1, Status: "draft", DefinitionJSON: `{}`},

								&model.ShotAssetReference{ID: "ref-s1-char", ShotID: "s1", AssetVersionID: "v-old", Role: "character", Status: "confirmed"},
								&model.ShotAssetReference{ID: "ref-s1-ref", ShotID: "s1", AssetVersionID: "v-old", Role: "reference", Status: "linked"},
								&model.ShotAssetReference{ID: "ref-s2-b", ShotID: "s2", AssetVersionID: "vb1", Role: "prop", Status: "confirmed"},

								&model.ShotArtifact{ID: "art-s1", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "r1", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{}`},
								&model.ShotArtifact{ID: "art-s2", ProjectID: "p", UnitID: "unit", ShotID: "s2", RevisionID: "r2", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{}`},
								&model.ShotArtifact{ID: "art-s3", ProjectID: "p", UnitID: "unit", ShotID: "s3", RevisionID: "r3", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{}`},
							}
							for _, row := range seedRows {
								if err := dbs[0].Create(row).Error; err != nil {
									t.Fatal(err)
								}
							}
							seedSettledLocalResourceForLegacyBarrier(t, repo0, dataDir, "res-owner", "res-unref")
							if err := dbs[0].Create(&model.AssetRepresentation{
								ID:             "rep-d1-target",
								AssetVersionID: "vd1",
								ResourceID:     "res-unref",
								Role:           "primary",
								MediaType:      "image",
								MetadataJSON:   `{}`,
							}).Error; err != nil {
								t.Fatal(err)
							}

							ctx := context.Background()
							preview, err := svc0.PreviewProjectUnitShotAssetBindings(ctx, "owner", "p", "unit", BindingPreviewRequest{
								ShotIDs: []string{"s1", "s2", "s3"},
							})
							if err != nil {
								t.Fatalf("preview failed: %v", err)
							}
							appendReq := BindingAppendRequest{
								Baseline: preview.Baseline,
								Selections: []BindingSelection{
									{ShotID: "s1", AssetVersionIDs: []string{"v-old", "vc1"}},
									{ShotID: "s2", AssetVersionIDs: []string{"vb1"}},
									{ShotID: "s3", AssetVersionIDs: []string{}},
								},
							}

							runAppend := func(svc *Service) (BindingAppendResult, error) {
								return svc.AppendProjectUnitShotAssetBindings(ctx, "owner", "p", "unit", appendReq)
							}
							runCompetitor := func(svc *Service) error {
								switch method {
								case "script_revision":
									_, _, err := svc.CreateShotRevision("owner", "p", "s1", ShotRevisionInput{PlotDescription: "并发改写脚本"})
									return err
								case "reference_link":
									_, err := svc.LinkShotAsset("owner", "p", "s3", LinkShotAssetRequest{AssetVersionID: "vd1", Role: "reference"})
									return err
								case "reference_unlink":
									return svc.UnlinkShotAsset("owner", "p", "s1", "ref-s1-ref")
								case "shot_replace":
									_, err := svc.ReplaceProjectUnitShots("owner", "p", "unit", ReplaceProjectUnitShotsRequest{
										ExpectedShotIDs: []string{"s1", "s2", "s3"},
										Shots: []ReplaceProjectUnitShotInput{
											{CreateProjectShotRequest: CreateProjectShotRequest{Title: "rep-1", Description: "d1", DurationMs: 3000}},
										},
									})
									return err
								case "shot_delete":
									return svc.DeleteProjectShot("owner", "p", "s3")
								case "asset_unlink":
									return svc.UnlinkProjectAsset("owner", "p", "c")
								case "version_switch_shared":
									asset, err := svc.repo.AssetForUser("owner", "c")
									if err != nil {
										return err
									}
									asset.PrimaryVersionID = "vc2"
									asset.Status = model.AssetVersionStatusConfirmed
									return svc.repo.SaveCharacterVersion("p", asset, &model.AssetVersion{
										ID:             "vc2",
										AssetID:        "c",
										Version:        2,
										Status:         model.AssetVersionStatusConfirmed,
										DefinitionJSON: `{"summary":"v2"}`,
									}, nil, nil, "vc1")
								case "shared_category_update":
									cat := "environment"
									_, err := svc.UpdateProjectAsset("owner", "p", "c", UpdateProjectAssetRequest{Category: &cat})
									return err
								case "admin_target_owner_resource_delete":
									resRow, err := svc.repo.Resource("res-unref")
									if err != nil {
										return err
									}
									return svc.repo.DeleteAdminResources([]model.Resource{*resRow}, nil, []model.AdminAuditEvent{{ID: "audit-admin-del"}})
								case "resource_delete_outbox":
									return svc.repo.DeleteResource("res-owner", "res-unref")
								case "resource_purge_outbox":
									return svc.repo.DeleteAssetsAndResources("res-owner", nil, []string{"res-unref"}, nil, true)
								default:
									return errors.New("unknown method")
								}
							}

							// Coordinate two-order barrier on dbs[0] (delayed) vs dbs[1] (immediate)
							arrived := make(chan struct{})
							release := make(chan struct{})
							var once, releaseOnce sync.Once
							defer releaseOnce.Do(func() { close(release) })
							cbName := "legacy_all_entry_barrier_" + method + "_" + first
							if err := dbs[0].Callback().Raw().Before("gorm:raw").Register(cbName, func(tx *gorm.DB) {
								sql := tx.Statement.SQL.String()
								if !strings.Contains(sql, "pg_advisory_xact_lock") && !strings.Contains(sql, "UPDATE assets SET id = id WHERE 1 = 0") {
									return
								}
								once.Do(func() {
									close(arrived)
									select {
									case <-release:
									case <-time.After(5 * time.Second):
										tx.AddError(context.DeadlineExceeded)
									}
								})
							}); err != nil {
								t.Fatal(err)
							}
							defer func() { _ = dbs[0].Callback().Raw().Remove(cbName) }()

							isOutboxMethod := method == "admin_target_owner_resource_delete" || method == "resource_delete_outbox" || method == "resource_purge_outbox"

							if first == "competitor" {
								// Append reaches barrier on dbs[0], competitor commits first on dbs[1], then append resumes and must reject with 409 conflict
								type appendOutcome struct {
									res BindingAppendResult
									err error
								}
								done := make(chan appendOutcome, 1)
								go func() {
									res, err := runAppend(svc0)
									done <- appendOutcome{res, err}
								}()
								select {
								case <-arrived:
								case <-time.After(5 * time.Second):
									t.Fatal("append did not reach owner coordination barrier")
								}

								compErr := runCompetitor(svc1)
								stateAfterCompetitor := appendFullState(t, dbs[1])
								releaseOnce.Do(func() { close(release) })
								outcome := <-done

								if compErr != nil {
									t.Fatalf("expected competitor %s to succeed when first, got %v", method, compErr)
								}
								assertAppendAppError(t, outcome.err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)
								if appendFullState(t, dbs[1]) != stateAfterCompetitor {
									t.Fatalf("rejected append mutated DB state after competitor %s!", method)
								}

								// Outbox verification: only resource deletion/purge methods create Outbox rows, and physical file remains on disk until worker runs
								var intentCnt, jobCnt, taskCnt int64
								_ = dbs[1].Model(&model.ResourceDeletionIntent{}).Count(&intentCnt).Error
								_ = dbs[1].Model(&model.ResourceDeletionJob{}).Count(&jobCnt).Error
								_ = dbs[1].Model(&model.Task{}).Count(&taskCnt).Error
								if taskCnt != 0 {
									t.Fatalf("expected 0 Task rows, got %d", taskCnt)
								}
								wantOutbox := int64(0)
								if isOutboxMethod {
									wantOutbox = 1
								}
								if intentCnt != wantOutbox || jobCnt != wantOutbox {
									t.Fatalf("method %s (first=competitor) outbox count mismatch: intents=%d jobs=%d want=%d", method, intentCnt, jobCnt, wantOutbox)
								}
								if _, statErr := os.Stat(filepath.Join(dataDir, "resources", "res-unref.png")); statErr != nil {
									t.Fatalf("expected physical file res-unref.png to remain on disk during transactional Outbox staging, got %v", statErr)
								}
							} else {
								// Competitor reaches barrier on dbs[0], append commits first on dbs[1], then competitor resumes
								done := make(chan error, 1)
								go func() {
									done <- runCompetitor(svc0)
								}()
								select {
								case <-arrived:
								case <-time.After(5 * time.Second):
									t.Fatal("competitor did not reach owner coordination barrier")
								}

								appendRes, appendErr := runAppend(svc1)
								if appendErr != nil {
									releaseOnce.Do(func() { close(release) })
									<-done
									t.Fatalf("expected append to succeed when first, got %v", appendErr)
								}
								if appendRes.Unchanged || appendRes.AddedCount != 1 || appendRes.ProjectRevision != 11 {
									releaseOnce.Do(func() { close(release) })
									<-done
									t.Fatalf("unexpected append result when first: %+v", appendRes)
								}
								// Verify zero outbox or tasks created by append itself
								var preCompIntentCnt, preCompJobCnt, preCompTaskCnt int64
								_ = dbs[1].Model(&model.ResourceDeletionIntent{}).Count(&preCompIntentCnt).Error
								_ = dbs[1].Model(&model.ResourceDeletionJob{}).Count(&preCompJobCnt).Error
								_ = dbs[1].Model(&model.Task{}).Count(&preCompTaskCnt).Error
								if preCompIntentCnt != 0 || preCompJobCnt != 0 || preCompTaskCnt != 0 {
									t.Fatalf("append created unexpected Task or Outbox rows: tasks=%d intents=%d jobs=%d", preCompTaskCnt, preCompIntentCnt, preCompJobCnt)
								}

								releaseOnce.Do(func() { close(release) })
								compErr := <-done

								switch method {
								case "asset_unlink":
									// Append added a reference to vc1 (asset c) on s1 -> UnlinkProjectAsset must observe the new reference inside the lock and fail!
									if compErr == nil {
										t.Fatalf("expected UnlinkProjectAsset to reject unlinking asset c after append bound vc1 on s1")
									}
									linked, err := repo1.ProjectAssetLinked("p", "c")
									if err != nil || !linked {
										t.Fatalf("expected asset c link to remain protected after rejected unlink: linked=%v err=%v", linked, err)
									}
									projP, err := repo1.ProjectForUser("owner", "p")
									if err != nil || projP.Revision != 11 {
										t.Fatalf("expected project p revision to remain 11 after rejected unlink, got %+v (err=%v)", projP, err)
									}
								case "version_switch_shared", "shared_category_update":
									if compErr != nil {
										t.Fatalf("expected %s to succeed after append, got %v", method, compErr)
									}
									// Shared asset c is linked to both project p (rev 11 -> 12) and project q (rev 4 -> 5)
									projP, err := repo1.ProjectForUser("owner", "p")
									if err != nil || projP.Revision != 12 {
										t.Fatalf("expected project p revision=12 (monotonic after append+shared asset update), got %+v (err=%v)", projP, err)
									}
									projQ, err := repo1.ProjectForUser("co-owner", "q")
									if err != nil || projQ.Revision != 5 {
										t.Fatalf("expected shared project q revision=5, got %+v (err=%v)", projQ, err)
									}
								default:
									if compErr != nil {
										t.Fatalf("expected competitor %s to complete after append, got %v", method, compErr)
									}
								}

								if isOutboxMethod {
									var postIntentCnt, postJobCnt int64
									_ = dbs[1].Model(&model.ResourceDeletionIntent{}).Count(&postIntentCnt).Error
									_ = dbs[1].Model(&model.ResourceDeletionJob{}).Count(&postJobCnt).Error
									if postIntentCnt != 1 || postJobCnt != 1 {
										t.Fatalf("expected 1 Outbox intent+job after %s (first=append), got intents=%d jobs=%d", method, postIntentCnt, postJobCnt)
									}
									if _, statErr := os.Stat(filepath.Join(dataDir, "resources", "res-unref.png")); statErr != nil {
										t.Fatalf("expected physical file res-unref.png to remain on disk during Outbox staging, got %v", statErr)
									}
								}

								// Unless shot_replace replaced the unit's shots, s1 must still hold its newly appended vc1 reference
								if method != "shot_replace" {
									var s1Refs []model.ShotAssetReference
									if err := dbs[1].Where("shot_id = ?", "s1").Find(&s1Refs).Error; err != nil {
										t.Fatal(err)
									}
									refVersions := []string{}
									for _, r := range s1Refs {
										refVersions = append(refVersions, r.AssetVersionID)
									}
									if !slices.Contains(refVersions, "vc1") {
										t.Fatalf("expected s1 to retain appended reference vc1 after %s, got %+v", method, s1Refs)
									}
								}
							}
						})
					}
				})
			}
		})
	}
}

