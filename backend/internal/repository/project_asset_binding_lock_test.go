package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/database"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

func TestStoryboardAssetOwnerLockWorkflowTableOrder(t *testing.T) {
	a,_:=storyboardDatabases(t,"postgres");if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
	instance:=model.WorkflowInstance{ID:"wf",ProjectID:"p",UnitID:"u",Revision:1};step:=model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",OutputJSON:"{}"}
	for _,row:=range []any{&instance,&step,&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
	locked:=[]string{};if err:=a.db.Callback().Query().After("gorm:query").Register("workflow_table_order",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{locked=append(locked,tx.Statement.Table)}});err!=nil{t.Fatal(err)}
	instance.Revision++;if err:=a.RegisterWorkflowTaskOutput(&step,nil,&instance,"p",&model.WorkflowStepTask{ID:"tl",WorkflowStepID:"step",TaskID:"task"},&model.AssetRepresentation{ID:"rep",AssetVersionID:"v",ResourceID:"image",MediaType:"image",Role:"output"},nil,&model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s",ResourceID:"image"});err!=nil{t.Fatal(err)}
	want:=[]string{"projects","project_units","shots","project_asset_links","assets","asset_versions","shot_revisions","asset_representations","resources","shot_asset_references","shot_artifacts"};if len(locked)<len(want)||!reflect.DeepEqual(locked[:min(len(locked),len(want))],want){t.Fatalf("mixed workflow wrong first-lock order: %v",locked)}
}

func TestStoryboardAssetOwnerLockShotMultiRowOrder(t *testing.T) {
	a, _ := storyboardDatabases(t, "postgres")
	if err := database.MigrateSchema(a.db); err != nil { t.Fatal(err) }
	for _, suffix := range []string{"z", "a"} {
		for _, row := range []any{
			&model.ProjectUnit{ID:"u"+suffix, ProjectID:"p"},
			&model.Shot{ID:"s"+suffix, ProjectID:"p", UnitID:"u"+suffix},
			&model.Asset{ID:"a"+suffix, UserID:"owner", PayloadJSON:"{}"},
			&model.AssetVersion{ID:"v"+suffix, AssetID:"a"+suffix, Version:1, DefinitionJSON:"{}"},
			&model.ProjectAssetLink{ID:"l"+suffix, ProjectID:"p", AssetID:"a"+suffix},
			&model.ShotAssetReference{ID:"r"+suffix, ShotID:"s"+suffix, AssetVersionID:"v"+suffix},
		} { if err:=a.db.Create(row).Error; err!=nil { t.Fatal(err) } }
	}
	seen:=map[string]bool{}
	if err:=a.db.Callback().Query().After("gorm:query").Register("multi_row_order",func(tx *gorm.DB){
		if _,ok:=tx.Statement.Clauses["FOR"];!ok{return}
		value:=reflect.Indirect(reflect.ValueOf(tx.Statement.Dest))
		if value.Kind()!=reflect.Slice || value.Len()<2{return}
		ids:=[]string{}
		for i:=0;i<value.Len();i++{ids=append(ids,value.Index(i).FieldByName("ID").String())}
		if !slices.IsSorted(ids){t.Errorf("%s rows acquired out of primary-key order: %v",tx.Statement.Table,ids)}
		if !strings.Contains(tx.Statement.SQL.String(),"ORDER BY id"){t.Errorf("%s has no explicit primary-key order",tx.Statement.Table)}
		seen[tx.Statement.Table]=true
	});err!=nil{t.Fatal(err)}
	if err:=a.WithStoryboardProjectWrite("p",func(tx *Repository)error{return tx.lockStoryboardShotRows("p","",[]string{"sz","sa"},nil,nil)});err!=nil{t.Fatal(err)}
	for _,table:=range []string{"project_units","shots","project_asset_links","assets","asset_versions","shot_asset_references"}{if !seen[table]{t.Errorf("missing multi-row lock for %s",table)}}
}

func TestStoryboardAssetOwnerLockDeletionVersionOrder(t *testing.T) {
	a,_:=storyboardDatabases(t,"postgres");if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
	for _,row:=range []any{&model.Asset{ID:"a",UserID:"owner",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:`{"resourceId":"image"}`},&model.Resource{ID:"image",UserID:"owner",Provider:"local",ObjectKey:"image",Status:model.ResourceStatusReady}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
	storyboardClosedLocalResource(t,a,"image")
	locked:=[]string{};if err:=a.db.Callback().Query().After("gorm:query").Register("deletion_table_order",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{locked=append(locked,tx.Statement.Table)}});err!=nil{t.Fatal(err)}
	if err:=a.DeleteAssetsAndResources("owner",[]string{"a"},nil,nil,false);err!=nil{t.Fatal(err)}
	want:=[]string{"assets","asset_versions","asset_representations","resources"};if len(locked)<len(want)||!reflect.DeepEqual(locked[:min(len(locked),len(want))],want){t.Fatalf("deletion skipped version/representation locks: %v",locked)}
}

func TestStoryboardAssetOwnerLockShotIncomingScope(t *testing.T) {
	a,_:=storyboardDatabases(t,"postgres");if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
	for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Resource{ID:"image",UserID:"owner",Status:model.ResourceStatusReady}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
	units,resources:=[]string{},[]string{};seenRefs:=false
	if err:=a.db.Callback().Query().After("gorm:query").Register("incoming_scope",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];!ok{return};switch rows:=tx.Statement.Dest.(type){case *[]model.ProjectUnit:for _,row:=range *rows{units=append(units,row.ID)};case *[]model.Resource:for _,row:=range *rows{resources=append(resources,row.ID)};case *[]model.ShotAssetReference:seenRefs=true;case *model.Resource:if seenRefs && !slices.Contains(resources,rows.ID){t.Error("incoming resource first locked after reference/artifact tables")}}});err!=nil{t.Fatal(err)}
	if err:=a.SaveShotWithRevision(&model.Shot{ID:"new",ProjectID:"p",UnitID:"u"},&model.ShotRevision{ID:"r",ShotID:"new",ActionBeatsJSON:`{"resourceId":"image"}`},true);err!=nil{t.Fatal(err)}
	if !reflect.DeepEqual(units,[]string{"u"})||!reflect.DeepEqual(resources,[]string{"image"}){t.Fatalf("creation/incoming scope absent from ordered locks: units=%v resources=%v",units,resources)}
}

func TestStoryboardAssetOwnerLockShotTableOrder(t *testing.T) {
	for _, method:=range []string{"revision","reference","artifact","delete","replace","save","stale","reference_invalidate","reference_delete","unit_update","unit_delete","project_delete"}{t.Run(method,func(t *testing.T){
		a,_:=storyboardDatabases(t,"postgres");if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
		for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.ShotAssetReference{ID:"ref",ShotID:"s",AssetVersionID:"v",Role:"visual"},&model.ShotArtifact{ID:"out",ShotID:"s",ProjectID:"p",UnitID:"u",Selected:true}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
		locked:=[]string{};if err:=a.db.Callback().Query().After("gorm:query").Register("shot_table_order",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{locked=append(locked,tx.Statement.Table)}});err!=nil{t.Fatal(err)}
		var err error;switch method{case "project_delete":err=a.DeleteProject("owner","p",nil);case "unit_update":err=a.UpdateProjectUnit(&model.ProjectUnit{ID:"u",ProjectID:"p",SourceText:"changed"},true);case "unit_delete":err=a.DeleteProjectUnit("p","u");case "save":err=a.SaveShot(&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},false);case "stale":err=a.MarkShotArtifactsStale("s",time.Now());case "reference_invalidate":err=a.UpsertShotAssetReferenceAndInvalidate("p",&model.ShotAssetReference{ID:"new",ShotID:"s",AssetVersionID:"v",Role:"reference"},time.Now());case "reference_delete":_,err=a.DeleteShotAssetReferenceAndInvalidate("p","s","ref",time.Now());case "revision":err=a.SaveShotWithRevision(&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.ShotRevision{ID:"r",ShotID:"s"},false);case "reference":err=a.UpsertShotAssetReference(&model.ShotAssetReference{ID:"new",ShotID:"s",AssetVersionID:"v",Role:"reference"});case "artifact":err=a.CreateShotArtifact(&model.ShotArtifact{ID:"new",ShotID:"s",ProjectID:"p",UnitID:"u"});case "delete":err=a.DeleteProjectShot("p","s",time.Now());case "replace":err=a.ReplaceProjectUnitShots("p","u",[]model.Shot{{ID:"new",ProjectID:"p",UnitID:"u"}},nil,nil,[]string{"s"})};if err!=nil{t.Fatal(err)}
		want:=[]string{"projects","project_units","shots","project_asset_links","assets","asset_versions","shot_revisions","asset_representations","resources","shot_asset_references","shot_artifacts"}
		if len(locked)<len(want)||!reflect.DeepEqual(locked[:min(len(locked),len(want))],want){t.Fatalf("%s missing fixed row-lock prefix: %v",method,locked)}
	})}
}

func TestStoryboardAssetResourceRaceSaveResourceOrders(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} { t.Run(dialect, func(t *testing.T) {
		for _, first := range []string{"resource", "confirmation"} { t.Run(first, func(t *testing.T) {
			a,b:=storyboardDatabases(t,dialect);if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
			resource:=model.Resource{ID:"image",UserID:"owner",Kind:"image",Provider:"local",ObjectKey:"image.png",Status:model.ResourceStatusReady,Width:10}
			for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u",Description:"script",CurrentRevisionID:"r"},&model.ShotRevision{ID:"r",ShotID:"s",ActionBeatsJSON:`[{"text":"original"}]`},&model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.AssetRepresentation{ID:"rep",AssetVersionID:"v",ResourceID:"image",Role:"primary",MediaType:"image"},&resource,&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.ShotAssetReference{ID:"manual",ShotID:"s",AssetVersionID:"v",Role:"character",Status:"manual"},&model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s",Status:"ready",Selected:true}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
			read:=func(repo *Repository) StoryboardAssetSnapshot {t.Helper();s,err:=repo.ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{t.Fatal(err)};return s}
			baseline:=read(a);conflict:=errors.New("baseline changed")
			write:=func(repo *Repository,kind string)error{
				if kind=="resource"{copy:=resource;copy.Width=20;return repo.SaveResource(&copy)}
				return repo.WithStoryboardAssetOwners(repo.db.Statement.Context,[]string{"owner"},func(tx *Repository)error{current,err:=tx.ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{return err};if !reflect.DeepEqual(current,baseline){return conflict};return tx.BumpProjectRevision("p")})
			}
			arrived,release:=make(chan struct{}),make(chan struct{});var once,releaseOnce sync.Once;defer releaseOnce.Do(func(){close(release)})
			if err:=a.db.Callback().Raw().Before("gorm:raw").Register("resource_orders",func(tx *gorm.DB){if tx.Statement.Context.Value("resource-order-barrier")!=true{return};sql:=tx.Statement.SQL.String();if !strings.Contains(sql,"pg_advisory_xact_lock")&&!strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0"){return};once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})});err!=nil{t.Fatal(err)}
			second:="confirmation";if first=="confirmation"{second="resource"};done:=make(chan error,1)
			go func(){done<-write(a.WithContext(context.WithValue(context.Background(),"resource-order-barrier",true)),second)}();select{case <-arrived:case <-time.After(5*time.Second):t.Fatal("missing barrier")}
			firstErr:=write(b,first);releaseOnce.Do(func(){close(release)});secondErr:=<-done;if firstErr!=nil{t.Fatal(firstErr)}
			if first=="resource"{if !errors.Is(secondErr,conflict){t.Fatalf("stale confirmation accepted: %v",secondErr)}}else if secondErr!=nil{t.Fatal(secondErr)}
			after:=read(b);if len(after.Resources)!=1||after.Resources[0].Width!=20{t.Fatal("resource writer did not commit")}
			// Compare every remaining snapshot field, including the complete manual
			// reference tuple, script revision and selected historical artifact.
			after.Resources[0].Width=baseline.Resources[0].Width;after.Resources[0].UpdatedAt=baseline.Resources[0].UpdatedAt
			if !reflect.DeepEqual(baseline,after){t.Fatalf("resource race changed unrelated snapshot state: before=%+v after=%+v",baseline,after)}
			p,err:=b.ProjectForUser("owner","p");if err!=nil{t.Fatal(err)};want:=int64(1);if first=="confirmation"{want=2};if int64(p.Revision)!=want{t.Fatalf("revision %d want %d",p.Revision,want)}
		}) }
	}) }
}

func TestStoryboardAssetConcurrencyWorkflowDirectRechecks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} { t.Run(dialect, func(t *testing.T) {
		for _, mode := range []string{"deleted_shot", "changed_revision", "changed_instance", "foreign_step", "changed_step"} { t.Run(mode, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			if err := database.MigrateSchema(a.db); err != nil { t.Fatal(err) }
			instance := model.WorkflowInstance{ID:"wf", ProjectID:"p", UnitID:"u", Revision:1}
			step := model.WorkflowStepInstance{ID:"step", WorkflowInstanceID:"wf", OutputJSON:"{}"}
			for _, row := range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"}, &model.Shot{ID:"s",ProjectID:"p",UnitID:"u",CurrentRevisionID:"r"}, &model.ShotRevision{ID:"r",ShotID:"s"}, &instance, &step} { if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)} }
			switch mode {
			case "deleted_shot": if err:=b.db.Delete(&model.Shot{},"id = ?","s").Error;err!=nil{t.Fatal(err)}
			case "changed_revision": if err:=b.db.Model(&model.Shot{}).Where("id = ?","s").Update("current_revision_id","r2").Error;err!=nil{t.Fatal(err)}
			case "changed_instance": if err:=b.db.Model(&model.WorkflowInstance{}).Where("id = ?","wf").Update("revision",2).Error;err!=nil{t.Fatal(err)}
			case "foreign_step": step.WorkflowInstanceID="other"
			case "changed_step": changed:=step;changed.Status=model.WorkflowStepStatusFailed;changed.Error="concurrent failure";progress:=instance;progress.Revision++;if err:=b.UpdateWorkflowProgress(&changed,nil,&progress,"p");err!=nil{t.Fatal(err)}
			}
			var before model.WorkflowInstance; if err:=b.db.First(&before,"id = ?","wf").Error;err!=nil{t.Fatal(err)}
			projectBefore,err:=b.ProjectForUser("owner","p");if err!=nil{t.Fatal(err)}
			instance.Revision=2
			err=a.RegisterWorkflowTaskOutput(&step,nil,&instance,"p",&model.WorkflowStepTask{ID:"tl",WorkflowStepID:"step",TaskID:"task"},nil,nil,&model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s",RevisionID:"r",TaskID:"task",Type:"image",Selected:true})
			if err==nil{t.Errorf("direct stale workflow accepted %s",mode)}
			var after model.WorkflowInstance;if err:=b.db.First(&after,"id = ?","wf").Error;err!=nil{t.Fatal(err)};if !reflect.DeepEqual(before,after){t.Error("rejected workflow changed instance")}
			for _,table:=range []any{&model.ShotArtifact{},&model.WorkflowStepTask{}}{var count int64;if err:=b.db.Model(table).Count(&count).Error;err!=nil{t.Fatal(err)};if count!=0{t.Errorf("rejected workflow leaked %T",table)}}
			p,err:=b.ProjectForUser("owner","p");if err!=nil{t.Fatal(err)};if !reflect.DeepEqual(p,projectBefore){t.Error("rejected workflow changed project")}
		}) }
	}) }
}

func TestStoryboardAssetConcurrencyVersionIdentityRecheck(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect);if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.CharacterVoiceBinding{});err!=nil{t.Fatal(err)}
		asset:=model.Asset{ID:"a",UserID:"owner",Category:model.AssetCategoryProp,Kind:"entity",PrimaryVersionID:"v",PayloadJSON:"{}"}
		for _,row:=range []any{&asset,&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
		if err:=b.db.Model(&model.Asset{}).Where("id = ?","a").Update("category",model.AssetCategoryEnvironment).Error;err!=nil{t.Fatal(err)}
		var expected model.Asset;if err:=b.db.First(&expected,"id = ?","a").Error;err!=nil{t.Fatal(err)}
		asset.PrimaryVersionID="v2"
		if err:=a.SaveCharacterVersion("p",&asset,&model.AssetVersion{ID:"v2",AssetID:"a",Version:2,DefinitionJSON:"{}"},nil,nil,"v");!errors.Is(err,ErrProjectAssetVersionChanged){t.Fatalf("stale identity accepted: %v",err)}
		var actual model.Asset;if err:=b.db.First(&actual,"id = ?","a").Error;err!=nil{t.Fatal(err)};if !reflect.DeepEqual(expected,actual){t.Fatal("rejected version overwrote asset")}
		versions,err:=b.AssetVersions("a");if err!=nil{t.Fatal(err)};if len(versions)!=1{t.Fatal("rejected version leaked")}
	})}
}

func TestStoryboardAssetOwnerLockRealAssetOuterOrder(t *testing.T) {
	a,_:=storyboardDatabases(t,"postgres")
	if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
	for _,row:=range []any{&model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
	var locked []string
	if err:=a.db.Callback().Query().After("gorm:query").Register("outer_order",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{locked=append(locked,tx.Statement.Table)}});err!=nil{t.Fatal(err)}
	err:=a.WithStoryboardAssetWrite("p","a",func(tx *Repository)error{return tx.ProfileWriteTransaction(func(tx *Repository)error{_,err:=tx.LockProfileAsset("owner","a");return err})})
	if err!=nil{t.Fatal(err)}
	if len(locked)<3||locked[0]!="projects"||locked[1]!="project_asset_links"||locked[2]!="assets"{t.Fatalf("real app-style outer locks asset before project: %v",locked)}
}

func TestStoryboardAssetOwnerLockDatabaseExpansion(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect)
		if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.Resource{});err!=nil{t.Fatal(err)}
		for _,row:=range []any{&model.Asset{ID:"a",UserID:"owner"},&model.Project{ID:"q",UserID:"earlier",Name:"shared"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
		arrived,release:=make(chan struct{}),make(chan struct{});var once,releaseOnce sync.Once
		defer releaseOnce.Do(func(){close(release)})
		if err:=a.db.Callback().Raw().Before("gorm:raw").Register("scope_growth",func(tx *gorm.DB){if tx.Statement.Context.Value("scope-growth-test")!=true{return};sql:=tx.Statement.SQL.String();if !strings.Contains(sql,"pg_advisory_xact_lock")&&!strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0"){return};once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})});err!=nil{t.Fatal(err)}
		done:=make(chan error,1);go func(){done<-a.WithContext(context.WithValue(context.Background(),"scope-growth-test",true)).WithStoryboardAssetWrite("p","a",func(tx *Repository)error{held,_:=tx.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string);if !reflect.DeepEqual(held,[]string{"earlier","owner"}){return fmt.Errorf("database expanded owners missing: %v",held)};return tx.BumpProjectRevision("p")})}()
		select{case <-arrived:case <-time.After(5*time.Second):t.Fatal("missing owner barrier")}
		if err:=b.WithStoryboardAssetOwners(context.Background(),[]string{"owner","earlier"},func(tx *Repository)error{return tx.db.Create(&model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"}).Error});err!=nil{t.Fatal(err)}
		releaseOnce.Do(func(){close(release)});if err:=<-done;err!=nil{t.Fatal(err)}
		var p model.Project;if err:=b.db.First(&p,"id = ?","p").Error;err!=nil{t.Fatal(err)};if p.Revision!=2{t.Fatalf("expansion committed revision %d",p.Revision)}
	})}
}

func TestStoryboardAssetOwnerLockReverseConnections(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect);start:=make(chan struct{});done:=make(chan error,2)
		for i,repo:=range []*Repository{a,b}{go func(i int,repo *Repository){<-start;owners:=[]string{"a","z"};if i==1{owners=[]string{"z","a"}};done<-repo.WithStoryboardAssetOwners(context.Background(),owners,func(tx *Repository)error{return tx.BumpProjectRevision("p")})}(i,repo)}
		close(start);for range 2{select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(6*time.Second):t.Fatal("opposite owner input deadlocked")}}
		var p model.Project;if err:=a.db.First(&p,"id = ?","p").Error;err!=nil{t.Fatal(err)};if p.Revision!=3{t.Fatalf("lost writer: %d",p.Revision)}
	})}
}

func TestStoryboardAssetOwnerLockFinalDeletionScope(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} { t.Run(dialect, func(t *testing.T) {
		a, b := storyboardDatabases(t, dialect)
		if err := database.MigrateSchema(a.db); err != nil { t.Fatal(err) }
		for _, row := range []any{
			&model.Asset{ID:"a", UserID:"owner", PayloadJSON:"{}"},
			&model.AssetVersion{ID:"v", AssetID:"a", Version:1, DefinitionJSON:`{"resourceId":"image"}`},
			&model.Resource{ID:"image", UserID:"owner", Provider:"local", ObjectKey:"shared", Status:model.ResourceStatusReady},
			&model.Resource{ID:"alias", UserID:"earlier-owner", Provider:"local", ObjectKey:"shared", Status:model.ResourceStatusReady},
		} { if err := a.db.Create(row).Error; err != nil { t.Fatal(err) } }
		storyboardClosedLocalResource(t,a,"image")
		storyboardClosedLocalResource(t,a,"alias")
		calls := 0
		err := a.WithStoryboardAssetOwners(context.Background(), []string{"owner"}, func(tx *Repository) error {
			calls++
			return tx.resourceLifecycleTransaction(func(tx *Repository) error {
				if err := tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", gorm.Expr("revision + 1")).Error; err != nil { return err }
				return tx.stageResourceDeletion("owner", []string{"a"}, nil)
			})
		})
		if err != nil { t.Fatal(err) }
		if calls != 2 { t.Errorf("final derived physical owner did not restart transaction: calls=%d", calls) }
		var project model.Project; if err := b.db.First(&project,"id = ?","p").Error; err != nil {t.Fatal(err)}
		if project.Revision != 2 {t.Errorf("expanded transaction leaked revision: %d", project.Revision)}
		var intents []model.ResourceDeletionIntent; if err := b.db.Find(&intents).Error; err != nil {t.Fatal(err)}
		if len(intents)!=1 || intents[0].ResourceIDsJSON!=`["image"]` {t.Fatalf("wrong final deletion set: %+v",intents)}
	}) }
}

func TestStoryboardAssetOwnerLockVersionRowOrder(t *testing.T) {
 for _,method:=range []string{"version","image","reference"}{t.Run(method,func(t *testing.T){
	a,_:=storyboardDatabases(t,"postgres")
	if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.CharacterVoiceBinding{});err!=nil{t.Fatal(err)}
	for _,row:=range []any{&model.Project{ID:"a-project",UserID:"owner",Name:"earlier",Revision:1},&model.Asset{ID:"asset",UserID:"owner",PrimaryVersionID:"v",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"asset",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"link",ProjectID:"p",AssetID:"asset"},&model.ProjectAssetLink{ID:"link-a",ProjectID:"a-project",AssetID:"asset"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
	var tables []string
	if err:=a.db.Callback().Query().After("gorm:query").Register("version_row_order",func(tx *gorm.DB){if _,ok:=tx.Statement.Clauses["FOR"];ok{tables=append(tables,tx.Statement.Table)}});err!=nil{t.Fatal(err)}
	asset,err:=a.AssetForUser("owner","asset");if err!=nil{t.Fatal(err)};asset.PrimaryVersionID="v2"
	version:=model.AssetVersion{ID:"v2",AssetID:"asset",Version:2,DefinitionJSON:"{}"}
	switch method {
	case "version":err=a.SaveCharacterVersion("p",asset,&version,nil,nil,"v")
	case "reference":err=a.CommitAssetReferenceVersion("p","v",asset,&version,nil,nil)
	case "image":
		if e:=a.db.Create(&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady}).Error;e!=nil{t.Fatal(e)}
		_,err=a.CommitProjectAssetImage("p","v",asset,&version,&model.AssetRepresentation{ID:"rep",AssetVersionID:"v2",ResourceID:"image",MediaType:"image",Role:"primary",TaskID:"task"})
	}
	if err!=nil{t.Fatal(err)}
	want := []string{"projects", "project_asset_links", "assets", "asset_versions", "asset_representations"}
		if len(tables)<len(want)||!reflect.DeepEqual(tables[:min(len(tables),len(want))],want){t.Fatalf("version row locks missing stable asset/version/representation prefix: %v",tables)}
 })}
}

func TestStoryboardAssetOwnerLockAdmissionLifecycle(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		for _,method:=range []string{"revision","replace","reference","reference_invalidate","artifact","entity"}{t.Run(method,func(t *testing.T){
			a,_:=storyboardDatabases(t,dialect)
			if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
			for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.Asset{ID:"a",UserID:"owner",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
			lifecycle:=false;write:=false
			if err:=a.db.Callback().Raw().Before("gorm:raw").Register("admission_lifecycle",func(tx *gorm.DB){sql:=tx.Statement.SQL.String();if strings.Contains(sql,"UPDATE resources SET status = status WHERE 1 = 0")||strings.Contains(sql,"hashtext"){lifecycle=true}});err!=nil{t.Fatal(err)}
			if err:=a.db.Callback().Create().Before("gorm:create").Register("admission_write",func(tx *gorm.DB){write=true;if !lifecycle{t.Errorf("%s write before lifecycle",method)}});err!=nil{t.Fatal(err)}
			var err error
			switch method{
			case "revision":err=a.SaveShotWithRevision(&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.ShotRevision{ID:"r",ShotID:"s"},false)
			case "replace":err=a.ReplaceProjectUnitShots("p","u",[]model.Shot{{ID:"new",ProjectID:"p",UnitID:"u"}},nil,nil,[]string{"s"})
			case "reference":err=a.UpsertShotAssetReference(&model.ShotAssetReference{ID:"ref",ShotID:"s",AssetVersionID:"v",Role:"visual"})
			case "reference_invalidate":err=a.UpsertShotAssetReferenceAndInvalidate("p",&model.ShotAssetReference{ID:"ref",ShotID:"s",AssetVersionID:"v",Role:"visual"},time.Now())
			case "artifact":err=a.CreateShotArtifact(&model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s"})
			case "entity":err=a.CreateProjectEntity(&model.Asset{ID:"new",UserID:"owner"},&model.AssetVersion{ID:"new-v",AssetID:"new",Version:1,DefinitionJSON:"{}"},&model.ProjectAssetLink{ID:"new-link",ProjectID:"p",AssetID:"new"},nil)
			}
			if err!=nil{t.Fatal(err)};if !write{t.Fatal("no write observed")}
		})}
	})}
}

func TestStoryboardAssetOwnerLockResourceWriters(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,_:=storyboardDatabases(t,dialect)
		if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.ResourceDeletionObject{});err!=nil{t.Fatal(err)}
		resource:=model.Resource{ID:"image",UserID:"owner",Provider:"local",ObjectKey:"shared-object",Status:model.ResourceStatusReady}
		for _,row:=range []any{&resource,&model.Resource{ID:"shared",UserID:"other",Provider:"local",ObjectKey:"shared-object",Status:model.ResourceStatusReady},&model.Asset{ID:"a",UserID:"owner",PayloadJSON:"{}"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:`{"resourceId":"image"}`}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
		t.Run("asset_only_shared_physical_owner",func(t *testing.T){
			err:=a.withStoryboardResourceWrite([]string{"a"},nil,func(tx *Repository)error{
				owners,_:=tx.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string)
				if !reflect.DeepEqual(owners,[]string{"other","owner"}){t.Fatalf("derived resource owners missing: %v",owners)}
				return nil
			});if err!=nil{t.Fatal(err)}
		})
		t.Run("save_resource_owner_before_lifecycle",func(t *testing.T){
			observed:=false
			if err:=a.db.Callback().Update().Before("gorm:update").Register("save_resource_owner",func(tx *gorm.DB){
				if tx.Statement.Table!="resources"{return};observed=true
				owners,_:=tx.Statement.Context.Value(storyboardOwnersContextKey{}).([]string)
				if !reflect.DeepEqual(owners,[]string{"other","owner"}){t.Errorf("SaveResource missing real shared owners: %v",owners)}
			});err!=nil{t.Fatal(err)}
			resource.Width=42;if err:=a.SaveResource(&resource);err!=nil{t.Fatal(err)};if !observed{t.Fatal("resource write not observed")}
		})
	})}
}

func TestStoryboardAssetConcurrencyRepositoryStaleLink(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect);if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.Resource{});err!=nil{t.Fatal(err)}
		stale:=model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v1",Category:model.AssetCategoryProp,PayloadJSON:"{}"}
		if err:=a.db.Create(&stale).Error;err!=nil{t.Fatal(err)}
		if err:=b.db.Model(&model.Asset{}).Where("id = ?","a").Updates(map[string]any{"primary_version_id":"v2","category":model.AssetCategoryEnvironment,"status":model.AssetVersionStatusConfirmed}).Error;err!=nil{t.Fatal(err)}
		var expected model.Asset;if err:=b.db.First(&expected,"id = ?","a").Error;err!=nil{t.Fatal(err)}
		if _,err:=a.LinkProjectAsset(&stale,nil,&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"});err!=nil{t.Fatal(err)}
		var actual model.Asset;if err:=b.db.First(&actual,"id = ?","a").Error;err!=nil{t.Fatal(err)}
		if !reflect.DeepEqual(expected,actual){t.Fatalf("link changed current asset: before=%+v after=%+v",expected,actual)}
	})}
}

func TestStoryboardAssetConcurrencyArtifactScope(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		for _,mode:=range []string{"deleted_shot","foreign_revision","unit_stale_source_recheck"}{t.Run(mode,func(t *testing.T){
			a,b:=storyboardDatabases(t,dialect)
			if err:=a.db.AutoMigrate(&model.ProjectUnit{},&model.Shot{},&model.ShotRevision{},&model.ShotArtifact{},&model.ShotAssetReference{},&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.WorkflowInstance{},&model.WorkflowStepInstance{});err!=nil{t.Fatal(err)}
			unit:=model.ProjectUnit{ID:"u",ProjectID:"p",SourceText:"old"}
			for _,row:=range []any{&unit,&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.ShotRevision{ID:"foreign",ShotID:"other"},&model.ShotArtifact{ID:"old",ProjectID:"p",UnitID:"u",ShotID:"s",Status:"ready",Selected:true},&model.WorkflowInstance{ID:"wf",ProjectID:"p",UnitID:"u",Revision:1},&model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",StepKey:"story",Status:model.WorkflowStepStatusCompleted}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
			if mode=="unit_stale_source_recheck" {
				if err:=b.db.Model(&model.ProjectUnit{}).Where("id = ?","u").Update("source_text","winner").Error;err!=nil{t.Fatal(err)}
				if err:=a.UpdateProjectUnit(&unit,false);err!=nil{t.Fatal(err)}
				var artifact model.ShotArtifact;if err:=b.db.First(&artifact,"id = ?","old").Error;err!=nil{t.Fatal(err)}
				if artifact.Status!="stale"||artifact.Selected{t.Fatal("stale source decision skipped artifact invalidation")}
				var step model.WorkflowStepInstance;if err:=b.db.First(&step,"id = ?","step").Error;err!=nil{t.Fatal(err)};if step.Status!=model.WorkflowStepStatusRunning{t.Fatal("stale source decision skipped workflow invalidation")}
			} else {
				if mode=="deleted_shot"{if err:=b.db.Delete(&model.Shot{},"id = ?","s").Error;err!=nil{t.Fatal(err)}}
				artifact:=model.ShotArtifact{ID:"new",ProjectID:"p",UnitID:"u",ShotID:"s",Type:"image",Status:"ready",Selected:true}
				if mode=="foreign_revision"{artifact.RevisionID="foreign"}
				if err:=a.CreateShotArtifact(&artifact);err==nil{t.Fatal("artifact admitted without valid shot/revision scope")}
				var rows []model.ShotArtifact;if err:=b.db.Order("id").Find(&rows).Error;err!=nil{t.Fatal(err)};if len(rows)!=1||!rows[0].Selected{t.Fatal("rejected artifact mutated existing selection")}
			}
		})}
	})}
}

func TestStoryboardAssetConcurrencyCandidateScope(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, detached := range []bool{false, true} {
				t.Run(fmt.Sprintf("detached_%v", detached), func(t *testing.T) {
					a,b := storyboardDatabases(t,dialect)
					if err:=a.db.AutoMigrate(&model.ProjectAssetLink{},&model.AssetVersion{},&model.AssetRepresentation{},&model.CharacterVoiceBinding{},&model.ProjectAssetCandidate{},&model.Shot{},&model.ShotAssetReference{});err!=nil{t.Fatal(err)}
					asset:=model.Asset{ID:"a",UserID:"owner",Kind:"entity",Category:model.AssetCategoryCharacter,PrimaryVersionID:"v",PayloadJSON:"{}"}
					candidate:=model.ProjectAssetCandidate{ID:"c",ProjectID:"p",Category:model.AssetCategoryCharacter,Status:"pending_confirmation",DetailsJSON:"{}"}
					for _,row:=range []any{&asset,&candidate,&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},&model.Project{ID:"q",UserID:"owner",Name:"shared",Revision:1},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},&model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"}} {if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
					if detached {if err:=b.DeleteProjectAssetLink("p","a");err!=nil{t.Fatal(err)}}
					asset.PrimaryVersionID="v2";candidate.Status="confirmed";candidate.ResolvedAssetID="a"
					err:=a.ConfirmProjectCharacterCandidate(&candidate,&asset,&model.AssetVersion{ID:"v2",AssetID:"a",Version:2,DefinitionJSON:"{}"},nil,nil,"v")
					if detached {if err==nil{t.Fatal("candidate adopted after unlink")}} else if err!=nil{t.Fatal(err)}
					want:=int64(2);if detached{want=1}
					for _,id:=range []string{"p","q"}{p,err:=b.ProjectForUser("owner",id);if err!=nil{t.Fatal(err)};if int64(p.Revision)!=want{t.Errorf("project %s revision=%d want %d",id,p.Revision,want)}}
					versions,err:=b.AssetVersions("a");if err!=nil{t.Fatal(err)};if int64(len(versions))!=want{t.Errorf("version count=%d",len(versions))}
				})
			}
		})
	}
}

func TestStoryboardAssetOwnerLockDatabaseTimeout(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect)
		err:=a.db.Transaction(func(tx *gorm.DB)error{
			if err:=tx.Model(&model.Project{}).Where("id = ?","p").UpdateColumn("revision",1).Error;err!=nil{return err}
			err:=b.WithStoryboardAssetOwners(context.Background(),[]string{"owner"},func(owned *Repository)error{
				if dialect=="postgres"{if err:=owned.db.Exec("SET LOCAL lock_timeout = '50ms'").Error;err!=nil{return err}}
				return owned.db.Model(&model.Project{}).Where("id = ?","p").UpdateColumn("revision",99).Error
			})
			var busy *kernel.AppError;if !errors.As(err,&busy)||busy.Status!=503||!busy.Retryable{t.Fatalf("DB lock timeout must be retryable503: %v",err)}
			return nil
		});if err!=nil{t.Fatal(err)}
		var p model.Project;if err:=b.db.First(&p,"id = ?","p").Error;err!=nil{t.Fatal(err)};if p.Revision!=1{t.Fatalf("lock timeout leaked write: %d",p.Revision)}
	})}
}

func TestStoryboardAssetOwnerLockBudget(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, mode := range []string{"internal", "caller_cancel", "caller_deadline"} {
				t.Run(mode, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					ctx, cancel := context.WithCancel(context.Background())
					if mode == "caller_deadline" { ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond) }
					defer cancel()
					err := a.WithStoryboardAssetOwners(ctx, []string{"owner"}, func(tx *Repository) error {
						if err := tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", 99).Error; err != nil { return err }
						if mode == "caller_cancel" { cancel() }
						<-tx.db.Statement.Context.Done()
						return tx.db.Statement.Context.Err()
					})
					if mode == "internal" {
						var busy *kernel.AppError
						if !errors.As(err, &busy) || busy.Status != 503 || !busy.Retryable { t.Fatalf("internal budget must be retryable 503, got %v", err) }
					} else {
						want := context.Canceled
						if mode == "caller_deadline" { want = context.DeadlineExceeded }
						if !errors.Is(err, want) { t.Fatalf("caller context lost: %v", err) }
					}
					var current model.Project
					if err := b.db.First(&current, "id = ?", "p").Error; err != nil { t.Fatal(err) }
					if current.Revision != 1 { t.Fatalf("timeout leaked revision: %d", current.Revision) }
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyDirectUnlinkProtection(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			if err := a.db.AutoMigrate(&model.ProjectAssetLink{}, &model.AssetVersion{}, &model.Shot{}, &model.ShotAssetReference{}); err != nil {
				t.Fatal(err)
			}
			for _, row := range []any{&model.Asset{ID: "a", UserID: "owner"}, &model.AssetVersion{ID: "v", AssetID: "a", Version: 1}, &model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "a"}, &model.Shot{ID: "shot", ProjectID: "p"}} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			// A simulated confirmation commits first. Even a direct repository caller
			// must not bypass the existing reference protection on unlink.
			if err := a.WithStoryboardAssetOwners(context.Background(), []string{"owner"}, func(tx *Repository) error {
				return tx.db.Create(&model.ShotAssetReference{ID: "ref", ShotID: "shot", AssetVersionID: "v", Role: "visual"}).Error
			}); err != nil {
				t.Fatal(err)
			}
			if err := b.DeleteProjectAssetLink("p", "a"); err == nil {
				t.Fatal("direct unlink bypassed reference protection")
			}
			linked, err := a.ProjectAssetLinked("p", "a")
			if err != nil || !linked {
				t.Fatalf("protected link missing: %v", err)
			}
		})
	}
}

func TestStoryboardAssetConcurrencyProjectWriters(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"delete_project", "delete_unit", "update_unit", "link", "unlink", "workflow", "create_entity", "create_character", "confirm_candidate", "move_asset"} {
				t.Run(method, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := database.MigrateSchema(a.db); err != nil {
						t.Fatal(err)
					}
					asset := model.Asset{ID: "a", UserID: "owner"}
					unit := model.ProjectUnit{ID: "u", ProjectID: "p"}
					instance := model.WorkflowInstance{ID: "wf", ProjectID: "p", UnitID: "u"}
					step := model.WorkflowStepInstance{ID: "step", WorkflowInstanceID: "wf", OutputJSON: "{}"}
					for _, row := range []any{&asset, &unit, &instance, &step} {
						if err := a.db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					if method == "unlink" || method == "move_asset" {
						if err := a.db.Create(&model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "a"}).Error; err != nil {
							t.Fatal(err)
						}
					}
					if method == "confirm_candidate" {
						if err := a.db.Create(&model.ProjectAssetCandidate{ID: "candidate", ProjectID: "p", Status: "pending_confirmation"}).Error; err != nil {
							t.Fatal(err)
						}
					}
					write := func(r *Repository) error {
						switch method {
						case "delete_project":
							return r.DeleteProject("owner", "p", nil)
						case "delete_unit":
							return r.DeleteProjectUnit("p", "u")
						case "update_unit":
							unit.SourceText = "new script"
							return r.UpdateProjectUnit(&unit, true)
						case "link":
							_, err := r.LinkProjectAsset(&asset, nil, &model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "a"})
							return err
						case "unlink":
							return r.DeleteProjectAssetLink("p", "a")
						case "move_asset":
							return r.MoveProjectAsset("p", "a", "", 2)
						case "create_entity":
							return r.CreateProjectEntity(&model.Asset{ID: "new", UserID: "owner"}, &model.AssetVersion{ID: "v-new", AssetID: "new", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "new-link", ProjectID: "p", AssetID: "new"}, nil)
						case "create_character":
							return r.CreateProjectCharacter("p", &model.Asset{ID: "new", UserID: "owner"}, &model.AssetVersion{ID: "v-new", AssetID: "new", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "new-link", ProjectID: "p", AssetID: "new"})
						case "confirm_candidate":
							return r.ConfirmProjectAssetCandidate(&model.ProjectAssetCandidate{ID: "candidate", ProjectID: "p", Status: "confirmed"}, &model.Asset{ID: "new", UserID: "owner"}, &model.AssetVersion{ID: "v-new", AssetID: "new", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "new-link", ProjectID: "p", AssetID: "new"}, true)
						default:
							outputInstance := instance
							outputInstance.Revision++
							return r.RegisterWorkflowTaskOutput(&step, nil, &outputInstance, "p", &model.WorkflowStepTask{ID: "task-link", WorkflowStepID: "step", TaskID: "task"}, nil, nil, nil)
						}
					}
					// Simulate confirmation's protected check/insert window, not the append API.
					err := storyboardLock(t, a, context.Background(), []string{"owner"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
						defer cancel()
						err := write(b.WithContext(ctx))
						if !storyboardContentionMatches(dialect, err) {
							t.Errorf("%s bypassed owner window: %v", method, err)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if t.Failed() {
						return
					}
					if err := write(b); err != nil {
						t.Fatalf("writer did not recover after owner release: %v", err)
					}
				})
			}
		})
	}
}

type storyboardOwnerLocker interface {
	WithStoryboardAssetOwners(context.Context, []string, func(*Repository) error) error
}

func storyboardLock(t *testing.T, r *Repository, ctx context.Context, owners []string, fn func(*Repository) error) error {
	t.Helper()
	locker, ok := any(r).(storyboardOwnerLocker)
	if !ok {
		t.Fatal("missing WithStoryboardAssetOwners: competing writers cannot coordinate")
	}
	return locker.WithStoryboardAssetOwners(ctx, owners, fn)
}

// Each handle has its own single-connection pool, including file-backed SQLite.
func storyboardContentionMatches(_ string, err error) bool {
	// These callers expire at 30ms, before SQLite's 100ms busy timeout and
	// PostgreSQL's internal lock budget. Neither may project cancellation as 503.
	var busy *kernel.AppError
	return errors.Is(err, context.DeadlineExceeded) && !errors.As(err, &busy)
}

func storyboardDatabases(t *testing.T, dialect string) (*Repository, *Repository) {
	t.Helper()
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var open func() (*gorm.DB, error)
	if dialect == "sqlite" {
		dsn := filepath.Join(t.TempDir(), "owners.db") + "?_journal_mode=WAL&_busy_timeout=100"
		open = func() (*gorm.DB, error) { return gorm.Open(sqlite.Open(dsn), config) }
	} else {
		dsn := os.Getenv("CANVAS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Fatal("PostgreSQL gate blocked: CANVAS_TEST_POSTGRES_DSN must name an isolated database; SKIP is not acceptance")
		}
		admin, err := gorm.Open(postgres.Open(dsn), config)
		if err != nil {
			t.Fatal("cannot connect to isolated PostgreSQL")
		}
		schema := fmt.Sprintf("storyboard_lock_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
			pool, _ := admin.DB()
			_ = pool.Close()
		})
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("test DSN must be a PostgreSQL URL")
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		open = func() (*gorm.DB, error) { return gorm.Open(postgres.Open(u.String()), config) }
	}
	handles := []*Repository{}
	for range 2 {
		db, err := open()
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = pool.Close() })
		handles = append(handles, New(db))
	}
	if err := handles[0].db.AutoMigrate(&model.Asset{}, &model.Project{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	if err := handles[0].db.Create(&model.Project{ID: "p", UserID: "owner", Name: "lock-test", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	return handles[0], handles[1]
}

func TestStoryboardAssetResourceRaceFinalizeRequiresOwner(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, _ := storyboardDatabases(t, dialect)
			if err := database.MigrateSchema(a.db); err != nil {
				t.Fatal(err)
			}
			intent := model.ResourceDeletionIntent{ID: "intent", UserID: "owner", Status: "pending", AssetIDsJSON: "[]", ResourceIDsJSON: "[\"resource\"]"}
			resource := model.Resource{ID: "resource", UserID: "owner", Status: model.ResourceStatusDeleting}
			for _, row := range []any{&intent, &resource} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := a.ResourceLifecycleTransaction(func(tx *Repository) error { return tx.finalizeDeletionIntent(&intent) }); err == nil {
				t.Fatal("finalize accepted lifecycle-only caller without owner fence")
			}
			var count int64
			if err := a.db.Model(&model.Resource{}).Where("id = ?", resource.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("uncoordinated finalize retired resource")
			}
		})
	}
}

func TestStoryboardAssetResourceRaceWorker(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"prepare", "finish"} {
				t.Run(method, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := database.MigrateSchema(a.db); err != nil {
						t.Fatal(err)
					}
					resource := model.Resource{ID: "resource", UserID: "owner", Provider: "local", ObjectKey: "worker-object", Status: model.ResourceStatusReady}
					if err := a.db.Create(&resource).Error; err != nil {
						t.Fatal(err)
					}
					storyboardClosedLocalResource(t, a, resource.ID)
					if err := a.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
						t.Fatal(err)
					}
					job, err := a.ClaimNextResourceDeletionJob("worker", time.Minute)
					if err != nil || job == nil {
						t.Fatalf("claim: %v", err)
					}
					write := func(r *Repository) error {
						if method == "prepare" {
							_, _, err := r.PrepareResourceDeletion(job)
							return err
						}
						return r.FinishResourceDeletion(job, false)
					}
					err = a.WithStoryboardAssetOwners(context.Background(), []string{"owner"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
						defer cancel()
						err := write(b.WithContext(ctx))
						if !storyboardContentionMatches(dialect, err) {
							t.Errorf("worker %s bypassed owner window: %v", method, err)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if t.Failed() {
						return
					}
					if err := write(b); err != nil {
						t.Fatal(err)
					}
					var count int64
					if err := a.db.Model(&model.Resource{}).Where("id = ?", resource.ID).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					if method == "finish" && count != 0 {
						t.Fatal("finish did not retire resource")
					}
					if method == "prepare" && count != 1 {
						t.Fatal("prepare retired resource before I/O")
					}
				})
			}
		})
	}
}

func TestStoryboardAssetResourceRaceOwnerScope(t *testing.T) {
	for _, scope := range []string{"shared-project", "shared-object", "shared-playback", "stale-admin-owner"} {
		t.Run(scope, func(t *testing.T) {
			a, b := storyboardDatabases(t, "postgres")
			if err := database.MigrateSchema(a.db); err != nil {
				t.Fatal(err)
			}
			resource := model.Resource{ID: "resource", UserID: "owner", Provider: "local", ObjectKey: "shared-object", Status: model.ResourceStatusReady}
			if err := a.db.Create(&resource).Error; err != nil {
				t.Fatal(err)
			}
			held := "other-owner"
			switch scope {
			case "shared-project":
				for _, row := range []any{&model.Asset{ID: "a", UserID: "owner"}, &model.Project{ID: "shared", UserID: "other-owner", Name: "shared"}, &model.ProjectAssetLink{ID: "link", ProjectID: "shared", AssetID: "a"}} {
					if err := a.db.Create(row).Error; err != nil {
						t.Fatal(err)
					}
				}
			case "shared-object":
				copy := resource
				copy.ID = "copy"
				copy.UserID = "other-owner"
				if err := a.db.Create(&copy).Error; err != nil {
					t.Fatal(err)
				}
			case "shared-playback":
				copy := resource
				copy.ID = "copy"
				copy.UserID = "other-owner"
				copy.ObjectKey = "other-original"
				copy.PlaybackObjectKey = resource.ObjectKey
				copy.PlaybackStatus = model.PlaybackStatusReady
				if err := a.db.Create(&copy).Error; err != nil {
					t.Fatal(err)
				}
			default:
				held = "owner"
				resource.UserID = "administrator"
			}
			err := a.WithStoryboardAssetOwners(context.Background(), []string{held}, func(*Repository) error {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				err := b.WithContext(ctx).DeleteAdminResources([]model.Resource{resource}, nil, []model.AdminAuditEvent{{}})
				if !storyboardContentionMatches("postgres", err) {
					t.Errorf("deletion failed to coordinate database owner scope %s: %v", scope, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var row model.Resource
			if err := a.db.First(&row, "id = ?", "resource").Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != model.ResourceStatusReady {
				t.Fatal("blocked scope changed resource")
			}
		})
	}
}

func TestStoryboardAssetResourceRaceDeletionAdmission(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"cleanup", "admin", "purge", "direct", "announcement-update", "announcement-discard"} {
				t.Run(method, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := database.MigrateSchema(a.db); err != nil {
						t.Fatal(err)
					}
					row := model.Resource{ID: "resource", UserID: "owner", Provider: "local", ObjectKey: "isolated-object", Status: model.ResourceStatusReady}
					if err := a.db.Create(&row).Error; err != nil {
						t.Fatal(err)
					}
					if method == "announcement-update" {
						if err := a.db.Create(&model.Announcement{ID: "announcement", ImageResourceID: row.ID}).Error; err != nil {
							t.Fatal(err)
						}
					}
					if method == "announcement-discard" {
						if err := a.db.Create(&model.AnnouncementImageDraft{UserID: "owner", ResourceID: row.ID}).Error; err != nil {
							t.Fatal(err)
						}
					}
					err := storyboardLock(t, a, context.Background(), []string{"owner"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
						defer cancel()
						r := b.WithContext(ctx)
						var err error
						switch method {
						case "cleanup":
							err = r.DeleteDetachedResources([]model.Resource{row}, nil)
						case "admin":
							err = r.DeleteAdminResources([]model.Resource{row}, nil, []model.AdminAuditEvent{{}})
						case "direct":
							err = r.DeleteResource("owner", row.ID)
						case "announcement-update":
							err = r.UpdateAnnouncementWithImage(&model.Announcement{ID: "announcement"}, "owner", "", &row, nil)
						case "announcement-discard":
							err = r.DiscardAnnouncementImageDraft("owner", &row, nil)
						default:
							err = r.DeleteAssetsAndResources("owner", nil, []string{row.ID}, nil, true)
						}
						if !storyboardContentionMatches(dialect, err) {
							t.Errorf("%s entered deletion before owner lock: %v", method, err)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					var actual model.Resource
					if err := a.db.First(&actual, "id = ?", row.ID).Error; err != nil {
						t.Fatal(err)
					}
					if actual.Status != model.ResourceStatusReady {
						t.Fatal("blocked admission changed resource")
					}
				})
			}
		})
	}
}

func TestStoryboardAssetOwnerLockLifecycleOrder(t *testing.T) {
	a, b := storyboardDatabases(t, "postgres")
	if err := a.db.AutoMigrate(&model.ProjectAssetLink{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&model.Asset{ID: "a", UserID: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	err := a.WithStoryboardAssetWrite("p", "a", func(tx *Repository) error {
		return tx.ProfileWriteTransaction(func(*Repository) error {
			var available bool
			if err := b.db.Raw("SELECT pg_try_advisory_lock(hashtext(?)::bigint)", "resource-lifecycle").Scan(&available).Error; err != nil {
				return err
			}
			if available {
				_ = b.db.Exec("SELECT pg_advisory_unlock(hashtext(?)::bigint)", "resource-lifecycle").Error
				t.Error("owner wrapper bypassed the existing lifecycle fence before profile writes")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

type storyboardOwnerRequirement interface{ RequireStoryboardAssetOwners([]string) error }

func TestStoryboardAssetOwnerLockDiscoveryRestart(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, _ := storyboardDatabases(t, dialect)
			if _, ok := any(a).(storyboardOwnerRequirement); !ok {
				t.Fatal("missing lock-inside owner scope recheck")
			}
			calls := 0
			err := storyboardLock(t, a, context.Background(), []string{"z"}, func(tx *Repository) error {
				calls++
				if err := tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", gorm.Expr("revision + 1")).Error; err != nil {
					return err
				}
				return any(tx).(storyboardOwnerRequirement).RequireStoryboardAssetOwners([]string{"a", "z"})
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("scope expansion did not restart callback: %d", calls)
			}
			var p model.Project
			if err := a.db.First(&p, "id = ?", "p").Error; err != nil {
				t.Fatal(err)
			}
			if p.Revision != 2 {
				t.Fatalf("aborted discovery leaked writes: %d", p.Revision)
			}
			calls = 0
			err = storyboardLock(t, a, context.Background(), []string{"z"}, func(tx *Repository) error {
				calls++
				return any(tx).(storyboardOwnerRequirement).RequireStoryboardAssetOwners([]string{fmt.Sprintf("new-%d", calls)})
			})
			var conflict *kernel.AppError
			if !errors.As(err, &conflict) || conflict.Status != 409 || calls != 3 {
				t.Fatalf("unstable scope not bounded: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestStoryboardAssetOwnerLock(t *testing.T) {
	if _, ok := any(New(nil)).(storyboardOwnerLocker); !ok {
		t.Fatal("missing WithStoryboardAssetOwners: competing writers cannot coordinate")
	}
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			t.Run("commit_and_rollback", func(t *testing.T) {
				a, b := storyboardDatabases(t, dialect)
				sentinel := errors.New("rollback")
				err := storyboardLock(t, a, context.Background(), []string{"owner"}, func(tx *Repository) error {
					if tx == a || tx.db.Statement.ConnPool == a.db.Statement.ConnPool {
						t.Error("callback escaped transaction")
					}
					if err := tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", 9).Error; err != nil {
						return err
					}
					return sentinel
				})
				if !errors.Is(err, sentinel) {
					t.Fatalf("rollback error: %v", err)
				}
				var p model.Project
				if err := b.db.First(&p, "id = ?", "p").Error; err != nil {
					t.Fatal(err)
				}
				if p.Revision != 1 {
					t.Fatal("failed callback leaked write")
				}
			})
			t.Run("same_owner_blocks_before_callback", func(t *testing.T) {
				a, b := storyboardDatabases(t, dialect)
				held, release := make(chan struct{}), make(chan struct{})
				done := make(chan error, 1)
				go func() {
					done <- storyboardLock(t, a, context.Background(), []string{"owner"}, func(tx *Repository) error {
						close(held)
						<-release
						return tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", 2).Error
					})
				}()
				<-held
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				called := false
				err := storyboardLock(t, b, ctx, []string{"owner"}, func(*Repository) error { called = true; return nil })
				close(release)
				if first := <-done; first != nil {
					t.Fatal(first)
				}
				if called || err == nil {
					t.Fatalf("competing callback entered before owner released: called=%v error=%v", called, err)
				}
				if !storyboardContentionMatches(dialect, err) {
					t.Fatalf("contention lost dialect-specific timeout semantics: %v", err)
				}
				// The failed transaction must release its connection and observe the winner.
				err = storyboardLock(t, b, context.Background(), []string{"owner"}, func(tx *Repository) error {
					var p model.Project
					if err := tx.db.First(&p, "id = ?", "p").Error; err != nil {
						return err
					}
					if p.Revision != 2 {
						t.Errorf("stale snapshot revision %d", p.Revision)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
			t.Run("reject_empty_and_nested", func(t *testing.T) {
				a, _ := storyboardDatabases(t, dialect)
				for _, owners := range [][]string{nil, {}, {""}, {"owner", " "}} {
					called := false
					err := storyboardLock(t, a, context.Background(), owners, func(*Repository) error { called = true; return nil })
					if err == nil || called {
						t.Fatalf("accepted invalid owners %q", owners)
					}
				}
				err := a.Transaction(func(tx *Repository) error {
					called := false
					err := storyboardLock(t, tx, context.Background(), []string{"owner"}, func(*Repository) error { called = true; return nil })
					if err == nil || called {
						t.Error("accepted nested owner-lock transaction")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
			t.Run("sql_order_and_logical_sort", func(t *testing.T) {
				a, _ := storyboardDatabases(t, dialect)
				var mu sync.Mutex
				var statements []string
				var lockKeys []int64
				capture := func(tx *gorm.DB) {
					mu.Lock()
					defer mu.Unlock()
					statements = append(statements, tx.Statement.SQL.String())
					if strings.Contains(tx.Statement.SQL.String(), "pg_advisory_xact_lock") {
						lockKeys = append(lockKeys, tx.Statement.Vars[0].(int64))
					}
				}
				if err := a.db.Callback().Raw().Before("gorm:raw").Register("storyboard_capture", capture); err != nil {
					t.Fatal(err)
				}
				defer a.db.Callback().Raw().Remove("storyboard_capture")
				err := storyboardLock(t, a, context.Background(), []string{"z", "a", "z"}, func(tx *Repository) error {
					mu.Lock()
					defer mu.Unlock()
					if dialect == "sqlite" {
						if len(statements) == 0 || !strings.Contains(strings.ToLower(statements[0]), "update assets set id = id where 1 = 0") {
							t.Errorf("SQLite first SQL not writer reservation: %v", statements)
						}
					} else {
						keys := []int64{}
						for _, owner := range []string{"a", "z"} {
							sum := sha256.Sum256([]byte("storyboard-assets:" + owner))
							keys = append(keys, int64(binary.BigEndian.Uint64(sum[:8])))
						}
						// Inspect actual held lock keys, not a reimplementation-only unit assertion.
						var got []int64
						err := tx.db.Raw("SELECT ((classid::bigint << 32) | objid::bigint) AS key FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' ORDER BY key").Scan(&got).Error
						if err != nil {
							return err
						}
						if len(got) != 2 {
							t.Errorf("deduplicated advisory keys = %v", got)
						}
						for _, key := range keys {
							found := false
							for _, v := range got {
								if key == v {
									found = true
								}
							}
							if !found {
								t.Errorf("fixed SHA-256 key missing: %d", key)
							}
						}
						var locks []string
						for _, s := range statements {
							if strings.Contains(s, "pg_advisory_xact_lock") {
								locks = append(locks, s)
							}
						}
						if len(locks) != 2 {
							t.Errorf("expected two logical-owner locks before callback: %v", statements)
						}
						if !reflect.DeepEqual(lockKeys, keys) {
							t.Errorf("locks not acquired in logical owner order: got %v want %v", lockKeys, keys)
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
			if dialect == "postgres" {
				t.Run("different_owner_not_global", func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					err := storyboardLock(t, a, context.Background(), []string{"a"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						return storyboardLock(t, b, ctx, []string{"b"}, func(*Repository) error { return nil })
					})
					if err != nil {
						t.Fatalf("different owners serialized: %v", err)
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyShotWriters(t *testing.T) {
	cases := []struct {
		name  string
		write func(*Repository) error
	}{
		{"SaveShot", func(r *Repository) error {
			return r.SaveShot(&model.Shot{ID: "s", ProjectID: "p", Title: "late"}, false)
		}},
		{"SaveShotWithRevision", func(r *Repository) error {
			return r.SaveShotWithRevision(&model.Shot{ID: "s", ProjectID: "p", Title: "late"}, &model.ShotRevision{ID: "revision-new", ShotID: "s"}, false)
		}},
		{"CreateShotArtifact", func(r *Repository) error {
			return r.CreateShotArtifact(&model.ShotArtifact{ID: "artifact-new", ShotID: "s", ProjectID: "p", UnitID: "unit", Type: "image", Selected: true})
		}},
		{"MarkShotArtifactsStale", func(r *Repository) error { return r.MarkShotArtifactsStale("s", time.Now()) }},
		{"UpsertShotAssetReference", func(r *Repository) error {
			return r.UpsertShotAssetReference(&model.ShotAssetReference{ID: "ref-new", ShotID: "s", AssetVersionID: "v", Role: "reference", Status: "linked"})
		}},
		{"UpsertShotAssetReferenceAndInvalidate", func(r *Repository) error {
			return r.UpsertShotAssetReferenceAndInvalidate("p", &model.ShotAssetReference{ID: "ref-new", ShotID: "s", AssetVersionID: "v", Role: "reference", Status: "linked"}, time.Now())
		}},
		{"DeleteShotAssetReferenceAndInvalidate", func(r *Repository) error {
			_, err := r.DeleteShotAssetReferenceAndInvalidate("p", "s", "ref", time.Now())
			return err
		}},
		{"DeleteProjectShot", func(r *Repository) error { return r.DeleteProjectShot("p", "s", time.Now()) }},
		{"ReplaceProjectUnitShots", func(r *Repository) error {
			return r.ReplaceProjectUnitShots("p", "unit", []model.Shot{{ID: "replacement", ProjectID: "p", UnitID: "unit"}}, nil, nil, []string{"s"})
		}},
	}
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := a.db.AutoMigrate(&model.ProjectUnit{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotArtifact{}, &model.ShotAssetReference{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.ProjectAssetLink{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.ProductionTaskLink{}, &model.ProjectAssetCandidate{}); err != nil {
						t.Fatal(err)
					}
					for _, row := range []any{&model.ProjectUnit{ID: "unit", ProjectID: "p"}, &model.Shot{ID: "s", ProjectID: "p", UnitID: "unit", Title: "original"}, &model.Asset{ID: "asset", UserID: "owner", PrimaryVersionID: "v"}, &model.AssetVersion{ID: "v", AssetID: "asset", Version: 1}, &model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "asset"}, &model.ShotAssetReference{ID: "ref", ShotID: "s", AssetVersionID: "v", Role: "character"}, &model.ShotArtifact{ID: "artifact", ShotID: "s", ProjectID: "p", Type: "image", Version: 1, Selected: true, Status: "ready"}} {
						if err := a.db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					// Transaction simulates confirmation's protected check window, NOT append API.
					// A writer must not even perform its decision reads before the owner fence.
					err := storyboardLock(t, a, context.Background(), []string{"owner"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
						defer cancel()
						err := tc.write(b.WithContext(ctx))
						if err == nil {
							t.Errorf("%s bypassed owner fence", tc.name)
						} else if !storyboardContentionMatches(dialect, err) {
							t.Errorf("unexpected contention result: %v", err)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := tc.write(b); err != nil {
						t.Fatalf("writer did not recover after fence: %v", err)
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyAssetVersionWriters(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"SaveCharacterVersion", "ConfirmProjectCharacterCandidate", "CommitAssetReferenceVersion", "CommitProjectAssetImage", "CreateAssetVersion", "UpdateAssetDomain", "UpsertAsset", "ReplaceAssets", "MoveUserAssetsToFolder", "DeleteAssetFolder"} {
				t.Run(method, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := a.db.AutoMigrate(&model.AssetVersion{}, &model.ProjectAssetLink{}, &model.AssetRepresentation{}, &model.CharacterVoiceBinding{}, &model.ProjectAssetCandidate{}, &model.Resource{}); err != nil {
						t.Fatal(err)
					}
					for _, row := range []any{&model.Project{ID: "shared", UserID: "other-owner", Name: "shared", Revision: 1}, &model.Asset{ID: "asset", UserID: "owner", PrimaryVersionID: "v", Category: "character"}, &model.AssetVersion{ID: "v", AssetID: "asset", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "asset"}, &model.ProjectAssetLink{ID: "shared-link", ProjectID: "shared", AssetID: "asset"}, &model.ProjectAssetCandidate{ID: "candidate", ProjectID: "p", Status: "pending_confirmation"}} {
						if err := a.db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					if method == "MoveUserAssetsToFolder" || method == "DeleteAssetFolder" {
						if err := a.db.AutoMigrate(&model.AssetFolder{}); err != nil {
							t.Fatal(err)
						}
						if err := a.db.Create(&model.AssetFolder{ID: "folder", UserID: "owner", Name: "folder", NameKey: "folder"}).Error; err != nil {
							t.Fatal(err)
						}
						if err := a.db.Model(&model.Asset{}).Where("id = ?", "asset").Updates(map[string]any{"folder_id": "folder", "payload_json": "{}"}).Error; err != nil {
							t.Fatal(err)
						}
					}
					if method == "CommitProjectAssetImage" {
						if err := a.db.Create(&model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady}).Error; err != nil {
							t.Fatal(err)
						}
					}
					write := func(r *Repository) error {
						asset := model.Asset{ID: "asset", UserID: "owner", PrimaryVersionID: "v2", Category: "character"}
						version := model.AssetVersion{ID: "v2", AssetID: "asset", Version: 2, DefinitionJSON: "{}"}
						switch method {
						case "SaveCharacterVersion":
							return r.SaveCharacterVersion("p", &asset, &version, nil, nil, "v")
						case "ConfirmProjectCharacterCandidate":
							return r.ConfirmProjectCharacterCandidate(&model.ProjectAssetCandidate{ID: "candidate", ProjectID: "p", Status: "confirmed"}, &asset, &version, nil, nil, "v")
						case "CommitAssetReferenceVersion":
							return r.CommitAssetReferenceVersion("p", "v", &asset, &version, nil, nil)
						case "CommitProjectAssetImage":
							_, err := r.CommitProjectAssetImage("p", "v", &asset, &version, &model.AssetRepresentation{AssetVersionID: "v2", TaskID: "task", ResourceID: "image", Role: "primary", MediaType: "image"})
							return err
						case "MoveUserAssetsToFolder":
							return r.MoveUserAssetsToFolder("owner", []string{"asset"}, "")
						case "DeleteAssetFolder":
							return r.DeleteAssetFolder("owner", "folder")
						case "UpsertAsset":
							return r.UpsertAsset(&asset)
						case "ReplaceAssets":
							return r.ReplaceAssets("owner", []model.Asset{asset})
						case "CreateAssetVersion":
							return r.CreateAssetVersion(&version)
						default:
							return r.UpdateAssetDomain(&asset)
						}
					}
					// Lock only a different linked project's owner: asset owner alone is insufficient.
					err := storyboardLock(t, a, context.Background(), []string{"other-owner"}, func(*Repository) error {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
						defer cancel()
						err := write(b.WithContext(ctx))
						if err == nil {
							t.Errorf("%s bypassed shared project owner", method)
						} else if !storyboardContentionMatches(dialect, err) {
							t.Errorf("unexpected lock result: %v", err)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyReferenceRechecks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"direct", "invalidate", "replace"} {
				t.Run(method, func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := a.db.AutoMigrate(&model.ProjectUnit{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotArtifact{}, &model.ShotAssetReference{}, &model.AssetVersion{}, &model.ProjectAssetLink{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.ProjectAssetCandidate{}); err != nil {
						t.Fatal(err)
					}
					for _, row := range []any{&model.ProjectUnit{ID: "unit", ProjectID: "p"}, &model.Shot{ID: "s", ProjectID: "p", UnitID: "unit"}, &model.Asset{ID: "asset", UserID: "owner"}, &model.AssetVersion{ID: "v", AssetID: "asset", Version: 1}, &model.ProjectAssetLink{ID: "link", ProjectID: "p", AssetID: "asset"}} {
						if err := a.db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					// Actual caller validation sees the link, then a competitor removes it.
					if _, err := a.AssetVersionForProject("p", "v"); err != nil {
						t.Fatal(err)
					}
					committed := make(chan error, 1)
					go func() { committed <- b.db.Delete(&model.ProjectAssetLink{}, "id = ?", "link").Error }()
					if err := <-committed; err != nil {
						t.Fatal(err)
					}
					ref := model.ShotAssetReference{ID: "new", ShotID: "s", AssetVersionID: "v", Role: "reference"}
					var err error
					switch method {
					case "direct":
						err = a.UpsertShotAssetReference(&ref)
					case "invalidate":
						err = a.UpsertShotAssetReferenceAndInvalidate("p", &ref, time.Now())
					case "replace":
						ref.ShotID = "new-shot"
						err = a.ReplaceProjectUnitShots("p", "unit", []model.Shot{{ID: "new-shot", ProjectID: "p", UnitID: "unit"}}, nil, []model.ShotAssetReference{ref}, []string{"s"})
					}
					if err == nil {
						t.Fatal("writer trusted pre-lock linked version and inserted detached reference")
					}
					var count int64
					if err := a.db.Model(&model.ShotAssetReference{}).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Fatal("rejected reference leaked")
					}
				})
			}
		})
	}
}

// These tests exercise the existing read-only snapshot, not append confirmation.
func TestStoryboardAssetPreviewTransactionIsolation(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			err := a.WithStoryboardAssetSnapshot(context.Background(), func(tx *Repository) error {
				if dialect == "postgres" {
					var isolation, readonly string
					if err := tx.db.Raw("SHOW transaction_isolation").Scan(&isolation).Error; err != nil {
						return err
					}
					if err := tx.db.Raw("SHOW transaction_read_only").Scan(&readonly).Error; err != nil {
						return err
					}
					if isolation != "repeatable read" || readonly != "on" {
						t.Errorf("snapshot mode: %s/%s", isolation, readonly)
					}
				}
				var before, after model.Project
				if err := tx.db.First(&before, "id = ?", "p").Error; err != nil {
					return err
				}
				committed := make(chan error, 1)
				go func() { committed <- b.BumpProjectRevision("p") }()
				select {
				case err := <-committed:
					if err != nil {
						return err
					}
				case <-time.After(3 * time.Second):
					return errors.New("read-only snapshot blocked independent writer")
				}
				if err := tx.db.First(&after, "id = ?", "p").Error; err != nil {
					return err
				}
				if before.Revision != after.Revision {
					t.Error("snapshot mixed concurrent committed revision")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = a.WithStoryboardAssetSnapshot(context.Background(), func(tx *Repository) error {
				return tx.db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", 99).Error
			})
			if err == nil {
				t.Fatal("read-only transaction accepted write")
			}
			ctx, cancel := context.WithCancel(context.Background())
			err = a.WithStoryboardAssetSnapshot(ctx, func(tx *Repository) error {
				var p model.Project
				if err := tx.db.First(&p, "id = ?", "p").Error; err != nil {
					return err
				}
				cancel()
				return ctx.Err()
			})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if err := a.BumpProjectRevision("p"); err != nil {
				t.Fatalf("cancellation leaked transaction: %v", err)
			}
		})
	}
}

func TestStoryboardAssetPreviewSQLiteSameConnectionCleanup(t *testing.T) {
	for _, previous := range []int{0, 1} {
		t.Run(fmt.Sprintf("previous_%d", previous), func(t *testing.T) {
			a, _ := storyboardDatabases(t, "sqlite")
			// TEMP state belongs to the physical connection, proving it was returned and reused.
			if err := a.db.Exec("CREATE TEMP TABLE connection_marker (id INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
			if err := a.db.Exec(fmt.Sprintf("PRAGMA query_only = %d", previous)).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			err := a.WithStoryboardAssetSnapshot(ctx, func(tx *Repository) error {
				var count int
				if err := tx.db.Raw("SELECT count(*) FROM connection_marker").Scan(&count).Error; err != nil {
					return err
				}
				cancel()
				return ctx.Err()
			})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			var restored int
			if err := a.db.Raw("PRAGMA query_only").Scan(&restored).Error; err != nil {
				t.Fatal(err)
			}
			if restored != previous {
				t.Fatalf("query_only restored to %d, want %d", restored, previous)
			}
			var count int
			if err := a.db.Raw("SELECT count(*) FROM connection_marker").Scan(&count).Error; err != nil {
				t.Fatalf("connection was not reused: %v", err)
			}
		})
	}
	t.Run("failed_rollback_discards_connection", func(t *testing.T) {
		a, _ := storyboardDatabases(t, "sqlite")
		if err := a.db.Exec("CREATE TEMP TABLE discarded_marker (id INTEGER)").Error; err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("callback failure")
		err := a.WithStoryboardAssetSnapshot(context.Background(), func(tx *Repository) error {
			// Simulate a cleanup failure on the actual driver connection: cleanup's
			// ROLLBACK now fails because this transaction has already ended.
			conn := tx.db.Statement.ConnPool.(*sql.Conn)
			if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("callback error lost: %v", err)
		}
		var count int
		if err := a.db.Raw("SELECT count(*) FROM discarded_marker").Scan(&count).Error; err == nil {
			t.Fatal("failed-cleanup connection returned to pool")
		}
		if err := a.BumpProjectRevision("p"); err != nil {
			t.Fatalf("replacement connection unusable: %v", err)
		}
	})
}

func TestStoryboardAssetRevisionMonotonic(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			var stale model.Project
			if err := a.db.First(&stale, "id = ?", "p").Error; err != nil {
				t.Fatal(err)
			}
			ready, done := make(chan struct{}), make(chan error, 1)
			go func() { <-ready; done <- b.BumpProjectRevision("p") }()
			close(ready)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			stale.Name = "late settings"
			stale.Revision++
			if err := a.UpdateProject(&stale); err != nil {
				t.Fatal(err)
			}
			var stored model.Project
			if err := b.db.First(&stored, "id = ?", "p").Error; err != nil {
				t.Fatal(err)
			}
			if stored.Revision != 3 {
				t.Fatalf("late settings overwrote revision: got %d want 3", stored.Revision)
			}
			if !reflect.DeepEqual(stale.Revision, stored.Revision) {
				t.Fatalf("returned revision stale: %d vs %d", stale.Revision, stored.Revision)
			}
		})
	}
}
