package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"strings"
	"time"
	"testing"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestStoryboardAssetResourceRaceQueuedRemoteRequiresEvidence(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		var deletes atomic.Int32
		server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.Method==http.MethodDelete{deletes.Add(1);http.Error(w,"injected remote failure",503);return};_,_=w.Write([]byte("retained"))}));defer server.Close()
		dbs:=profileConcurrencyDatabases(t,dialect);db:=dbs[0];if err:=database.MigrateSchema(db);err!=nil{t.Fatal(err)}
		svc:=&Service{repo:repository.New(db),dataDir:t.TempDir()}
		resource:=model.Resource{ID:"remote",UserID:"owner",Provider:s3Provider,Endpoint:server.URL,Bucket:"test",ObjectKey:"object",Status:model.ResourceStatusDeleting}
		intent:=model.ResourceDeletionIntent{ID:"intent",UserID:"owner",AssetIDsJSON:"[]",ResourceIDsJSON:`["remote"]`,Status:"pending"}
		object:=model.ResourceDeletionObject{ID:"job",IntentID:"intent",ResourceIDsJSON:`["remote"]`,Variant:"original",Provider:s3Provider,Endpoint:server.URL,Bucket:"test",ObjectKey:"object",Status:"pending"}
		job:=model.ResourceDeletionJob{ID:"job",UserID:"owner",ResourceID:"remote",Provider:s3Provider,Endpoint:server.URL,Bucket:"test",ObjectKey:"object",Status:model.ResourceDeletionStatusPending,NextAttemptAt:time.Now()}
		// Historical queued state is deliberately missing proof. Never fabricate a
		// settled remote producer merely to force the worker past its safety check.
		for _,row:=range []any{&resource,&intent,&object,&job}{if err:=db.Create(row).Error;err!=nil{t.Fatal(err)}}
		read:=func()[]byte{tables:=[]any{&[]model.Resource{},&[]model.ResourceDeletionIntent{},&[]model.ResourceDeletionObject{},&[]model.ResourceProducerOperation{},&[]model.Project{},&[]model.ProjectUnit{},&[]model.Shot{},&[]model.ShotRevision{},&[]model.ShotAssetReference{},&[]model.ShotArtifact{},&[]model.Asset{},&[]model.AssetVersion{},&[]model.AssetRepresentation{},&[]model.ProjectAssetLink{},&[]model.WorkflowInstance{},&[]model.WorkflowStepInstance{},&[]model.WorkflowStepTask{},&[]model.ProductionTaskLink{},&[]model.ProjectAssetCandidate{},&[]model.AdminAuditEvent{}};for _,row:=range tables{if err:=db.Order("id").Find(row).Error;err!=nil{t.Fatal(err)}};encoded,_:=json.Marshal(tables);return encoded}
		var beforeJob model.ResourceDeletionJob;if err:=db.First(&beforeJob,"id = ?","job").Error;err!=nil{t.Fatal(err)}
		before:=read();started:=time.Now();svc.drainResourceDeletionJobs(1)
		if deletes.Load()!=0||string(before)!=string(read()){t.Fatal("worker deleted remote object or changed retained rows without evidence")}
		if err:=db.First(&job,"id = ?","job").Error;err!=nil{t.Fatal(err)}
		if job.Status!=model.ResourceDeletionStatusPending||job.Attempts!=1||job.LeaseOwner!=""||job.LeaseExpiresAt!=nil||!strings.Contains(job.LastError,"物理结算证据不足")||!job.NextAttemptAt.After(job.UpdatedAt){t.Fatalf("worker did not schedule safe retry: %+v",job)}
		if job.UpdatedAt.Before(started.Add(-time.Microsecond))||job.UpdatedAt.After(time.Now().Add(time.Microsecond)){t.Fatal("retry timestamp outside worker interval")}
		beforeJob.Attempts=1;beforeJob.LastError=job.LastError;beforeJob.NextAttemptAt=job.NextAttemptAt;beforeJob.UpdatedAt=job.UpdatedAt
		if !reflect.DeepEqual(beforeJob,job){t.Fatalf("retry changed unexpected complete job fields: before=%+v after=%+v",beforeJob,job)}
		response,err:=http.Get(server.URL+"/test/object");if err!=nil{t.Fatal(err)};defer response.Body.Close();if response.StatusCode!=200{t.Fatal("retained remote object unavailable")}
	})}
}

func TestStoryboardAssetResourceRaceSharedPhysicalFullState(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		for _,both:=range []bool{false,true}{t.Run(map[bool]string{false:"retained",true:"all-deleting"}[both],func(t *testing.T){
			dbs:=profileConcurrencyDatabases(t,dialect);db:=dbs[0];if err:=database.MigrateSchema(db);err!=nil{t.Fatal(err)}
			svc:=&Service{repo:repository.New(db),dataDir:t.TempDir()}
			path:=filepath.Join(svc.dataDir,"resources","shared");if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)};if err:=os.WriteFile(path,[]byte("shared"),0600);err!=nil{t.Fatal(err)}
			rows:=[]model.Resource{{ID:"alias-a",UserID:"user-1",Provider:"local",ObjectKey:"shared",Status:model.ResourceStatusReady},{ID:"alias-b",UserID:"user-2",Provider:"local",ObjectKey:"./shared",Status:model.ResourceStatusReady}}
			for i:=range rows{createClosedLocalFixtureResource(t,svc,&rows[i])}
			var before []model.Resource;if err:=db.Order("id").Find(&before).Error;err!=nil{t.Fatal(err)}
			// Unrelated storyboard rows must survive physical lifecycle unchanged.
			for _,row:=range []any{&model.Project{ID:"p",UserID:"user-1",Revision:9},&model.Asset{ID:"a",UserID:"user-1",PrimaryVersionID:"v"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1}}{if err:=db.Create(row).Error;err!=nil{t.Fatal(err)}}
			snapshot:=func(lifecycle bool)[]byte{tables:=[]any{&[]model.Project{},&[]model.ProjectUnit{},&[]model.Shot{},&[]model.ShotRevision{},&[]model.ShotAssetReference{},&[]model.ShotArtifact{},&[]model.Asset{},&[]model.AssetVersion{},&[]model.AssetRepresentation{},&[]model.ProjectAssetLink{},&[]model.WorkflowInstance{},&[]model.WorkflowStepInstance{},&[]model.WorkflowStepTask{},&[]model.ProductionTaskLink{}};if lifecycle{tables=append(tables,&[]model.Resource{},&[]model.ResourceDeletionJob{},&[]model.ResourceDeletionIntent{},&[]model.ResourceDeletionObject{},&[]model.ResourceProducerOperation{})};for _,table:=range tables{if err:=db.Order("id").Find(table).Error;err!=nil{t.Fatal(err)}};encoded,err:=json.Marshal(tables);if err!=nil{t.Fatal(err)};return encoded}
			domainBefore:=snapshot(false)
			if both {
				for _,row:=range rows{if err:=svc.repo.DeleteAssetsAndResources(row.UserID,nil,[]string{row.ID},nil,true);err!=nil{t.Fatal(err)}}
			}else if err:=svc.repo.DeleteDetachedResources(rows[:1],nil);err!=nil{t.Fatal(err)}
			svc.drainResourceDeletionJobs(8)
			var after []model.Resource;if err:=db.Order("id").Find(&after).Error;err!=nil{t.Fatal(err)}
			want:=before[1:];if both{want=[]model.Resource{}}
			if !reflect.DeepEqual(want,after){t.Fatalf("physical alias full resource rows differ: want=%+v got=%+v",want,after)}
			if string(domainBefore)!=string(snapshot(false)){t.Fatal("physical deletion changed unrelated complete domain rows")}
			bytes,err:=os.ReadFile(path);if both{if !os.IsNotExist(err){t.Fatalf("all-deleting object accessible: %v",err)}}else if err!=nil||string(bytes)!="shared"{t.Fatalf("retained alias bytes changed: %v",err)}
			var objects []model.ResourceDeletionObject;if err:=db.Find(&objects).Error;err!=nil{t.Fatal(err)};if len(objects)==0{t.Fatal("missing lifecycle object evidence")};status:="completed";if !both{status="retained"};for _,object:=range objects{if object.Status!=status{t.Fatalf("object expected %s: %+v",status,object)}}
			complete:=snapshot(true);svc.drainResourceDeletionJobs(8);if string(complete)!=string(snapshot(true)){t.Fatal("settled worker replay changed full domain/lifecycle snapshot")}
		})}
	})}
}
