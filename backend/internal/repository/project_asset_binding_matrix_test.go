package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

// This snapshot includes deleted rows and historical revisions, unlike the
// preview reader which intentionally requires every requested shot to exist.
type storyboardWriterState struct {
	Projects []model.Project
	Units []model.ProjectUnit
	Shots []model.Shot
	Revisions []model.ShotRevision
	References []model.ShotAssetReference
	Artifacts []model.ShotArtifact
	Assets []model.Asset
	Versions []model.AssetVersion
	Representations []model.AssetRepresentation
	Links []model.ProjectAssetLink
	Resources []model.Resource
	Instances []model.WorkflowInstance
	Steps []model.WorkflowStepInstance
	Tasks []model.WorkflowStepTask
	Production []model.ProductionTaskLink
	Candidates []model.ProjectAssetCandidate
}

func storyboardWriterSnapshot(t *testing.T, r *Repository) storyboardWriterState {
	t.Helper()
	var result storyboardWriterState
	value:=reflect.ValueOf(&result).Elem()
	for i:=0;i<value.NumField();i++ {
		if err:=r.db.Order("id").Find(value.Field(i).Addr().Interface()).Error;err!=nil{t.Fatal(err)}
	}
	return result
}

func TestStoryboardAssetConcurrencyWorkflowProgressCAS(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect);if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
		instance:=model.WorkflowInstance{ID:"wf",ProjectID:"p",UnitID:"u",Revision:1}
		step:=model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",Status:model.WorkflowStepStatusRunning,OutputJSON:"{}"}
		for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&instance,&step}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
		instance.Revision=2
		if err:=a.RegisterWorkflowTaskOutput(&step,nil,&instance,"p",&model.WorkflowStepTask{ID:"tl",WorkflowStepID:"step",TaskID:"task"},nil,nil,nil);err!=nil{t.Fatal(err)}
		before:=storyboardWriterSnapshot(t,b)
		step.Status=model.WorkflowStepStatusFailed;step.Error="stale failure"
		if err:=b.UpdateWorkflowProgress(&step,nil,&instance,"p");err==nil{t.Error("stale workflow progress overwrote committed output")}
		if after:=storyboardWriterSnapshot(t,b);!reflect.DeepEqual(before,after){t.Error("rejected stale progress changed complete state")}
	})}
}

func TestStoryboardAssetConcurrencyWriterFullOrders(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		a,b:=storyboardDatabases(t,dialect)
		if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
		now:=time.Date(2026,1,2,3,4,5,0,time.UTC)
		a.db.Config.NowFunc=func()time.Time{return now};b.db.Config.NowFunc=func()time.Time{return now}
		for _,method:=range []string{"shot","revision","reference_status","reference_add","reference_invalidate","reference_delete","artifact","stale","shot_delete","replace","unit_update","unit_delete","project_delete","version","reference_image","image","version_create","category","upsert","candidate","workflow","link","unlink"}{t.Run(method,func(t *testing.T){
			for _,first:=range []string{"writer","confirmation"}{t.Run(first,func(t *testing.T){
				// Reset only this test's dedicated schema, preserving no external data.
				var empty storyboardWriterState;value:=reflect.ValueOf(&empty).Elem()
				for i:=value.NumField()-1;i>=0;i--{if err:=a.db.Session(&gorm.Session{AllowGlobalUpdate:true}).Delete(value.Field(i).Addr().Interface()).Error;err!=nil{t.Fatal(err)}}
				rows:=[]any{
					&model.Project{ID:"p",UserID:"owner",Name:"matrix",Revision:1},
					&model.ProjectUnit{ID:"u",ProjectID:"p",SourceText:"original"},
					&model.Shot{ID:"s",ProjectID:"p",UnitID:"u",Title:"original",CurrentRevisionID:"r"},
					&model.ShotRevision{ID:"r",ShotID:"s",Version:1,ActionBeatsJSON:"[]"},
					&model.Asset{ID:"a",UserID:"owner",Kind:"entity",Category:model.AssetCategoryCharacter,PrimaryVersionID:"v",PayloadJSON:"{}"},
					&model.AssetVersion{ID:"v",AssetID:"a",Version:1,DefinitionJSON:"{}"},
					&model.AssetVersion{ID:"v2",AssetID:"a",Version:2,DefinitionJSON:"{}"},
					&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"},
					&model.ShotAssetReference{ID:"manual",ShotID:"s",AssetVersionID:"v",Role:"character",Status:"manual"},
					&model.ShotArtifact{ID:"out",ProjectID:"p",UnitID:"u",ShotID:"s",RevisionID:"r",Type:"image",Version:1,Status:"ready",Selected:true,MetadataJSON:"{}"},
				}
				assetWrite:=method=="version"||method=="reference_image"||method=="image"||method=="version_create"||method=="category"||method=="upsert"||method=="candidate"
				if assetWrite {rows=append(rows,&model.Project{ID:"q",UserID:"other",Name:"shared",Revision:7},&model.ProjectAssetLink{ID:"lq",ProjectID:"q",AssetID:"a"},&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady})}
				if method=="candidate" { rows=append(rows,&model.ProjectAssetCandidate{ID:"candidate",ProjectID:"p",Status:"pending_confirmation"}) }
				if method=="workflow" { rows=append(rows,&model.WorkflowInstance{ID:"wf",ProjectID:"p",UnitID:"u",Status:model.WorkflowStatusActive,Revision:1},&model.WorkflowStepInstance{ID:"step",WorkflowInstanceID:"wf",StepKey:"storyboard",Status:model.WorkflowStepStatusRunning,OutputJSON:"{}"}) }
				if method=="link"{rows=append(rows,&model.Asset{ID:"b",UserID:"owner",PrimaryVersionID:"vb",PayloadJSON:"{}"},&model.AssetVersion{ID:"vb",AssetID:"b",Version:1,DefinitionJSON:"{}"})}
				for _,row:=range rows{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
				if method=="unlink"{if err:=a.db.Delete(&model.ShotAssetReference{},"id = ?","manual").Error;err!=nil{t.Fatal(err)}}
				before:=storyboardWriterSnapshot(t,a)
				var want storyboardWriterState;encoded,_:=json.Marshal(before);if err:=json.Unmarshal(encoded,&want);err!=nil{t.Fatal(err)}
				added:=model.ShotAssetReference{ID:"added",ShotID:"s",AssetVersionID:"v2",Role:"reference",Status:"linked",CreatedAt:now}
				if first=="confirmation"{want.References=append([]model.ShotAssetReference{added},want.References...);want.Artifacts[0].Status="stale";want.Artifacts[0].Selected=false;want.Projects[0].Revision++}
				shot:=before.Shots[0];shot.Title="changed"
				revision:=model.ShotRevision{ID:"r2",ShotID:"s",Version:2,ActionBeatsJSON:"[]",CreatedAt:now}
				ref:=model.ShotAssetReference{ID:"writer-ref",ShotID:"s",AssetVersionID:"v",Role:"reference",Status:"linked",CreatedAt:now}
				artifact:=model.ShotArtifact{ID:"writer-out",ProjectID:"p",UnitID:"u",ShotID:"s",RevisionID:"r",Type:"image",Version:2,Status:"ready",Selected:true,MetadataJSON:"{}",CreatedAt:now,UpdatedAt:now}
				if method=="workflow"{artifact.TaskID="task"}
				unit:=before.Units[0];unit.SourceText="changed"
				asset:=before.Assets[0];asset.PrimaryVersionID="v3"
				version:=model.AssetVersion{ID:"v3",AssetID:"a",Version:3,DefinitionJSON:"{}",CreatedAt:now,UpdatedAt:now}
				representation:=model.AssetRepresentation{ID:"rep",AssetVersionID:"v3",ResourceID:"image",TaskID:"image-task",Role:"primary",MediaType:"image",MetadataJSON:"{}",CreatedAt:now}
				stale:=func(){for i:=range want.Artifacts{want.Artifacts[i].Status="stale";want.Artifacts[i].Selected=false}}
				switch method{
				case "shot":want.Shots[0].Title="changed"
				case "revision":want.Shots[0].Title="changed";want.Shots[0].CurrentRevisionID="r2";want.Revisions=append(want.Revisions,revision);stale();want.Projects[0].Revision++
				case "reference_status":for i:=range want.References{if want.References[i].ID=="manual"{want.References[i].Status="changed"}}
				case "reference_add":want.References=append(want.References,ref)
				case "reference_invalidate":want.References=append(want.References,ref);stale();want.Projects[0].Revision++
				case "reference_delete":for i:=range want.References{if want.References[i].ID=="manual"{want.References=append(want.References[:i],want.References[i+1:]...);break}};stale();want.Projects[0].Revision++
				case "artifact":want.Artifacts[0].Selected=false;want.Artifacts=append(want.Artifacts,artifact)
				case "stale":stale()
				case "shot_delete","replace","unit_delete","project_delete":
					want.Shots=[]model.Shot{};want.Revisions=[]model.ShotRevision{};want.References=[]model.ShotAssetReference{};want.Artifacts=[]model.ShotArtifact{};want.Projects[0].Revision++
					if method=="replace"{want.Shots=[]model.Shot{{ID:"replacement",ProjectID:"p",UnitID:"u",CreatedAt:now,UpdatedAt:now}}}
					if method=="unit_delete"||method=="project_delete"{want.Units=[]model.ProjectUnit{}}
					if method=="project_delete"{want.Projects=[]model.Project{};want.Links=[]model.ProjectAssetLink{}}
				case "unit_update":want.Units[0].SourceText="changed";stale();want.Projects[0].Revision++
				case "version","reference_image","image","candidate":
					want.Assets[0].PrimaryVersionID="v3";want.Versions=append(want.Versions,version);for i:=range want.Projects{want.Projects[i].Revision++}
					if method=="image"{want.Representations=append(want.Representations,representation)}
					if method=="candidate"{want.Candidates[0].Status="confirmed";want.Candidates[0].ResolvedAssetID="a"}
				case "version_create":want.Versions=append(want.Versions,version)
				case "category":want.Assets[0].Category=model.AssetCategoryEnvironment;for i:=range want.Projects{want.Projects[i].Revision++}
				case "upsert":want.Assets[0].Title="changed"
				case "workflow":
					want.Instances[0].Revision++
					if first=="writer" {want.Steps[0].Status=model.WorkflowStepStatusCompleted;want.Artifacts[0].Selected=false;want.Artifacts=append(want.Artifacts,artifact);want.Tasks=append(want.Tasks,model.WorkflowStepTask{ID:"tl",WorkflowStepID:"step",TaskID:"task",CreatedAt:now});want.Projects[0].Revision++}
				case "link":want.Links=append(want.Links,model.ProjectAssetLink{ID:"l2",ProjectID:"p",AssetID:"b",CreatedAt:now});want.Projects[0].Revision++
				case "unlink":if first=="writer"{want.Links=[]model.ProjectAssetLink{}}
				}
				write:=func(r *Repository)error{switch method{
				case "shot":return r.SaveShot(&shot,false)
				case "revision":return r.SaveShotWithRevision(&shot,&revision,false)
				case "reference_status":copy:=before.References[0];copy.Status="changed";return r.UpsertShotAssetReference(&copy)
				case "reference_add":return r.UpsertShotAssetReference(&ref)
				case "reference_invalidate":return r.UpsertShotAssetReferenceAndInvalidate("p",&ref,now)
				case "reference_delete":_,err:=r.DeleteShotAssetReferenceAndInvalidate("p","s","manual",now);return err
				case "artifact":return r.CreateShotArtifact(&artifact)
				case "stale":return r.MarkShotArtifactsStale("s",now)
				case "shot_delete":return r.DeleteProjectShot("p","s",now)
				case "replace":return r.ReplaceProjectUnitShots("p","u",[]model.Shot{{ID:"replacement",ProjectID:"p",UnitID:"u",CreatedAt:now,UpdatedAt:now}},nil,nil,[]string{"s"})
				case "unit_update":return r.UpdateProjectUnit(&unit,true)
				case "unit_delete":return r.DeleteProjectUnit("p","u")
				case "version":return r.SaveCharacterVersion("p",&asset,&version,nil,nil,"v")
				case "reference_image":return r.CommitAssetReferenceVersion("p","v",&asset,&version,nil,nil)
				case "image":_,err:=r.CommitProjectAssetImage("p","v",&asset,&version,&representation);return err
				case "version_create":return r.CreateAssetVersion(&version)
				case "category":copy:=before.Assets[0];copy.Category=model.AssetCategoryEnvironment;return r.UpdateAssetDomain(&copy)
				case "upsert":copy:=before.Assets[0];copy.Title="changed";return r.UpsertAsset(&copy)
				case "candidate":return r.ConfirmProjectCharacterCandidate(&model.ProjectAssetCandidate{ID:"candidate",ProjectID:"p",Status:"confirmed",ResolvedAssetID:"a",UpdatedAt:now},&asset,&version,nil,nil,"v")
				case "workflow":instance:=before.Instances[0];instance.Revision++;step:=before.Steps[0];step.Status=model.WorkflowStepStatusCompleted;return r.RegisterWorkflowTaskOutput(&step,nil,&instance,"p",&model.WorkflowStepTask{ID:"tl",WorkflowStepID:"step",TaskID:"task",CreatedAt:now},nil,nil,&artifact)
				case "link":copy:=before.Assets[1];_,err:=r.LinkProjectAsset(&copy,nil,&model.ProjectAssetLink{ID:"l2",ProjectID:"p",AssetID:"b",CreatedAt:now});return err
				case "unlink":return r.DeleteProjectAssetLink("p","a")
				default:return r.DeleteProject("owner","p",nil)
				}}
				conflict:=errors.New("complete baseline changed")
				startedAt:=time.Now().Add(-time.Microsecond)
				confirm:=func(r *Repository)error{return r.WithStoryboardAssetOwners(r.db.Statement.Context,[]string{"owner"},func(tx *Repository)error{
					if !reflect.DeepEqual(storyboardWriterSnapshot(t,tx),before){return conflict}
					if err:=tx.db.Create(&added).Error;err!=nil{return err}
					if err:=tx.db.Model(&model.ShotArtifact{}).Where("shot_id = ?","s").Updates(map[string]any{"status":"stale","selected":false}).Error;err!=nil{return err}
					if method=="workflow" {if err:=invalidateUnitWorkflowTx(tx.db,"p","u","storyboard",now);err!=nil{return err}}
					return tx.BumpProjectRevision("p")
				})}
				arrived,release:=make(chan struct{}),make(chan struct{});var once sync.Once
				if err:=a.db.Callback().Raw().Before("gorm:raw").Register("full_order_barrier",func(tx *gorm.DB){if tx.Statement.Context.Value("full-order-barrier")!=true{return};sql:=tx.Statement.SQL.String();if !strings.Contains(sql,"pg_advisory_xact_lock")&&!strings.Contains(sql,"UPDATE assets SET id = id WHERE 1 = 0"){return};once.Do(func(){close(arrived);select{case <-release:case <-time.After(4*time.Second):tx.AddError(context.DeadlineExceeded)}})});err!=nil{t.Fatal(err)}
				done:=make(chan error,1);go func(){delayed:=a.WithContext(context.WithValue(context.Background(),"full-order-barrier",true));if first=="writer"{done<-confirm(delayed)}else{done<-write(delayed)}}()
				select{case <-arrived:case <-time.After(5*time.Second):close(release);t.Fatal("writer did not reach owner barrier")}
				var firstErr error;if first=="writer"{firstErr=write(b)}else{firstErr=confirm(b)};close(release);secondErr:=<-done
				a.db.Callback().Raw().Remove("full_order_barrier")
				if firstErr!=nil{t.Fatal(firstErr)}
				if first=="writer"{if !errors.Is(secondErr,conflict){t.Fatalf("stale confirmation result: %v",secondErr)}}else if method=="workflow"{if !errors.Is(secondErr,gorm.ErrInvalidData){t.Fatalf("stale workflow did not reject changed instance: %v",secondErr)}}else if method=="unlink"{var appErr *kernel.AppError;if !errors.As(secondErr,&appErr)||appErr.Status!=400{t.Fatalf("unlink failed to observe newly committed reference: %v",secondErr)}}else if secondErr!=nil{t.Fatal(secondErr)}
				after:=storyboardWriterSnapshot(t,b)
				// Only these paths generate the project's timestamp with time.Now.
				// Bound that one value; every other timestamp remains exact.
				runtimeProjectTime:=method=="link"||method=="replace"||method=="unit_delete"||method=="version"||method=="reference_image"||method=="image"||method=="category"||method=="candidate"||(first=="confirmation"&&(method=="shot"||method=="reference_status"||method=="reference_add"||method=="artifact"||method=="stale"||method=="version_create"||method=="upsert"||method=="workflow"||method=="unlink"))
				if runtimeProjectTime { stamp:=after.Projects[0].UpdatedAt;if stamp.Before(startedAt)||stamp.After(time.Now().Add(time.Microsecond)){t.Fatalf("project timestamp outside writer interval: %v",stamp)};want.Projects[0].UpdatedAt=stamp }
				if assetWrite&&method!="version_create"&&method!="upsert" { stamp:=after.Projects[1].UpdatedAt;if stamp.Before(startedAt)||stamp.After(time.Now().Add(time.Microsecond)){t.Fatalf("shared project timestamp outside writer interval: %v",stamp)};want.Projects[1].UpdatedAt=stamp }
				for _,state:=range []*storyboardWriterState{&want,&after}{value:=reflect.ValueOf(state).Elem();for i:=0;i<value.NumField();i++{rows:=value.Field(i);for j:=0;j<rows.Len();j++{row:=rows.Index(j);for k:=0;k<row.NumField();k++{if stamp,ok:=row.Field(k).Interface().(time.Time);ok{row.Field(k).Set(reflect.ValueOf(stamp.UTC()))}}}}}
				if !reflect.DeepEqual(want,after){w,_:=json.Marshal(want);got,_:=json.Marshal(after);t.Fatalf("unexpected complete post-state\nwant=%s\ngot=%s",w,got)}
			})}
		})}
	})}
}
