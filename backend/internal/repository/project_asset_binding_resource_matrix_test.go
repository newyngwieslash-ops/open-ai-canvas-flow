package repository

import (
	"context"
	"errors"
	"reflect"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
)

// Missing-row corruption is injected explicitly: normal deletion admission
// rejects referenced resources. This verifies baseline handling, not permission
// for a normal writer to bypass deletion protection.
func TestStoryboardAssetResourceRaceMissingImageFullOrders(t *testing.T) {
	for _,dialect:=range []string{"sqlite","postgres"}{t.Run(dialect,func(t *testing.T){
		for _,first:=range []string{"missing","confirmation"}{t.Run(first,func(t *testing.T){
			a,b:=storyboardDatabases(t,dialect);if err:=database.MigrateSchema(a.db);err!=nil{t.Fatal(err)}
			for _,row:=range []any{&model.ProjectUnit{ID:"u",ProjectID:"p"},&model.Shot{ID:"s",ProjectID:"p",UnitID:"u"},&model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v"},&model.AssetVersion{ID:"v",AssetID:"a",Version:1},&model.AssetRepresentation{ID:"rep",AssetVersionID:"v",ResourceID:"image",MediaType:"image",Role:"primary"},&model.Resource{ID:"image",UserID:"owner",Kind:"image",Status:model.ResourceStatusReady},&model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}}{if err:=a.db.Create(row).Error;err!=nil{t.Fatal(err)}}
			before:=storyboardWriterSnapshot(t,a)
			baseline,err:=a.ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{t.Fatal(err)}
			conflict:=errors.New("baseline changed")
			confirm:=func()error{return b.WithStoryboardAssetOwners(context.Background(),[]string{"owner"},func(tx *Repository)error{current,err:=tx.ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{return err};if !reflect.DeepEqual(current,baseline){return conflict};return tx.UpsertShotAssetReference(&model.ShotAssetReference{ID:"ref",ShotID:"s",AssetVersionID:"v",Role:"character",Status:"linked"})})}
			missing:=func(){if err:=a.WithStoryboardAssetOwners(context.Background(),[]string{"owner"},func(tx *Repository)error{return tx.db.Delete(&model.Resource{},"id = ?","image").Error});err!=nil{t.Fatal(err)}}
			if first=="missing"{missing();if err:=confirm();!errors.Is(err,conflict){t.Fatalf("missing image did not conflict: %v",err)}}else{if err:=confirm();err!=nil{t.Fatal(err)};missing()}
			after:=storyboardWriterSnapshot(t,b);before.Resources=[]model.Resource{}
			if first=="confirmation"{if len(after.References)!=1||after.References[0].ID!="ref"||after.References[0].ShotID!="s"||after.References[0].AssetVersionID!="v"||after.References[0].Role!="character"||after.References[0].Status!="linked"{t.Fatal("wrong complete committed reference")};before.References=after.References}
			if !reflect.DeepEqual(before,after){t.Fatal("missing image changed unrelated full rows or removed valid version")}
			current,err:=b.ReadStoryboardAssetSnapshot("p","u",[]string{"s"});if err!=nil{t.Fatal(err)};if len(current.Resources)!=0||reflect.DeepEqual(current,baseline){t.Fatal("missing image omitted from baseline identity")}
		})}
	})}
}

func storyboardClosedLocalResource(t *testing.T, r *Repository, id string) {
	t.Helper()
	var row model.Resource
	if err:=r.db.First(&row,"id = ?",id).Error;err!=nil{t.Fatal(err)}
	path:=filepath.Join(t.TempDir(),row.ObjectKey)
	if err:=r.db.Delete(&row).Error;err!=nil{t.Fatal(err)}
	row.Status=model.ResourceStatusPending
	op,err:=r.BeginResourceProduction(&row,"original",true);if err!=nil{t.Fatal(err)}
	if err:=r.StartResourceProduction(op);err!=nil{t.Fatal(err)}
	if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(path,[]byte("fixture"),0600);err!=nil{t.Fatal(err)}
	if err:=r.FinishResourceProduction(op,true,true,"local-writer-returned");err!=nil{t.Fatal(err)}
	row.Status=model.ResourceStatusReady;if err:=r.SaveResource(&row);err!=nil{t.Fatal(err)}
}

// Existing admission-race tests prove the owner barrier. Here both serial
// orders additionally verify every persisted row and absence of deletion work.
func TestStoryboardAssetResourceRaceReferencedFullOrders(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, method := range []string{"direct", "cleanup", "admin", "purge"} {
				t.Run(method, func(t *testing.T) {
					for _, first := range []string{"deletion", "confirmation"} {
						t.Run(first, func(t *testing.T) {
							a, b := storyboardDatabases(t, dialect)
							if err := database.MigrateSchema(a.db); err != nil { t.Fatal(err) }
							resource := model.Resource{ID:"image", UserID:"owner", Provider:"local", ObjectKey:"shared-image", Status:model.ResourceStatusReady}
							for _, row := range []any{&resource, &model.Asset{ID:"a",UserID:"owner",PrimaryVersionID:"v"}, &model.AssetVersion{ID:"v",AssetID:"a",Version:1}, &model.ProjectAssetLink{ID:"l",ProjectID:"p",AssetID:"a"}, &model.Shot{ID:"s",ProjectID:"p",Title:"source-shot"}} {
								if err := a.db.Create(row).Error; err != nil { t.Fatal(err) }
							}
							storyboardClosedLocalResource(t,a,"image")
							// No resource reference exists until this real media writer commits.
							// The deletion-first branch therefore must succeed, not fail on an
							// unrelated pre-existing representation or missing producer proof.
							stamp:=time.Date(2026,1,2,3,4,5,0,time.UTC)
							artifact:=model.ShotArtifact{ID:"output",ProjectID:"p",ShotID:"s",ResourceID:"image",Type:"image",Status:"ready",Selected:true,MetadataJSON:"{}",CreatedAt:stamp,UpdatedAt:stamp}
							wantArtifact:=artifact;wantArtifact.Version=1
							confirm:=func()error{return a.CreateShotArtifact(&artifact)}
							remove := func() error {
								switch method {
								case "direct": return b.DeleteResource("owner","image")
								case "cleanup": return b.DeleteDetachedResources([]model.Resource{resource},nil)
								case "admin": return b.DeleteAdminResources([]model.Resource{resource},nil,[]model.AdminAuditEvent{{ID:"audit"}})
								default: return b.DeleteAssetsAndResources("owner",nil,[]string{"image"},nil,true)
								}
							}
							ledger:=func()[]any{
								rows:=[]any{&[]model.ResourceDeletionJob{},&[]model.ResourceDeletionIntent{},&[]model.ResourceDeletionObject{},&[]model.AdminAuditEvent{},&[]model.ResourceProducerOperation{}}
								for _,row:=range rows{if err:=a.db.Order("id").Find(row).Error;err!=nil{t.Fatal(err)}}
								return rows
							}
							before:=storyboardWriterSnapshot(t,a)
							beforeLedger:=ledger()
							started:=time.Now().Add(-time.Microsecond)
							if first=="deletion"{
								if err:=remove();err!=nil{t.Fatalf("unreferenced deletion must reach admission: %v",err)}
								staged:=storyboardWriterSnapshot(t,a)
								before.Resources[0].Status=model.ResourceStatusDeleting
								if at:=staged.Resources[0].UpdatedAt;at.Before(started)||at.After(time.Now().Add(time.Microsecond)){t.Fatal("deletion resource timestamp outside operation")}
								before.Resources[0].UpdatedAt=staged.Resources[0].UpdatedAt
								if !reflect.DeepEqual(before,staged){t.Fatal("deletion admission changed unrelated complete domain rows")}
								jobs:=ledger()
								for i:=0;i<4;i++{want:=1;if i==3&&method!="admin"{want=0};if reflect.ValueOf(jobs[i]).Elem().Len()!=want{t.Fatalf("wrong admitted ledger row count %d",i)}}
								job:=(*jobs[0].(*[]model.ResourceDeletionJob))[0]
								intent:=(*jobs[1].(*[]model.ResourceDeletionIntent))[0]
								object:=(*jobs[2].(*[]model.ResourceDeletionObject))[0]
								for _,at:=range []time.Time{job.CreatedAt,job.UpdatedAt,job.NextAttemptAt,intent.CreatedAt,intent.UpdatedAt,object.CreatedAt,object.UpdatedAt}{if at.Before(started)||at.After(time.Now().Add(time.Microsecond)){t.Fatal("ledger timestamp outside admission")}}
								if job.ID==""||intent.ID==""||object.ID!=job.ID{t.Fatal("missing linked deletion identity")}
								wantJob:=model.ResourceDeletionJob{ID:object.ID,UserID:"owner",ResourceID:"image",Provider:"local",ObjectKey:"shared-image",Status:model.ResourceDeletionStatusPending,NextAttemptAt:job.NextAttemptAt,CreatedAt:job.CreatedAt,UpdatedAt:job.UpdatedAt}
								wantIntent:=model.ResourceDeletionIntent{ID:intent.ID,UserID:"owner",AssetIDsJSON:"null",ResourceIDsJSON:`["image"]`,Status:"pending",CreatedAt:intent.CreatedAt,UpdatedAt:intent.UpdatedAt}
								wantObject:=model.ResourceDeletionObject{ID:job.ID,IntentID:intent.ID,ResourceIDsJSON:`["image"]`,Variant:"original",Provider:"local",ObjectKey:"shared-image",Status:"pending",CreatedAt:object.CreatedAt,UpdatedAt:object.UpdatedAt}
								if !reflect.DeepEqual(job,wantJob)||!reflect.DeepEqual(intent,wantIntent)||!reflect.DeepEqual(object,wantObject){t.Fatalf("wrong complete deletion ledger: job=%+v intent=%+v object=%+v",job,intent,object)}
								if method=="admin"{audit:=(*jobs[3].(*[]model.AdminAuditEvent))[0];if audit.CreatedAt.Before(started)||audit.CreatedAt.After(time.Now().Add(time.Microsecond))||!reflect.DeepEqual(audit,model.AdminAuditEvent{ID:"audit",CreatedAt:audit.CreatedAt}){t.Fatal("wrong full admin audit")}}
								producers:=*jobs[4].(*[]model.ResourceProducerOperation);wantProducers:=*beforeLedger[4].(*[]model.ResourceProducerOperation)
								if len(producers)!=1||len(wantProducers)!=1{t.Fatal("missing real producer evidence")}
								wantProducers[0].Fenced=true
								if at:=producers[0].UpdatedAt;at.Before(started)||at.After(time.Now().Add(time.Microsecond)){t.Fatal("producer fencing timestamp outside admission")}
								wantProducers[0].UpdatedAt=producers[0].UpdatedAt
								if !reflect.DeepEqual(wantProducers,producers){t.Fatal("deletion changed producer beyond fence")}
								if err:=confirm();!errors.Is(err,gorm.ErrRecordNotFound){t.Fatalf("late media write must reject non-ready resource: %v",err)}
								if !reflect.DeepEqual(staged,storyboardWriterSnapshot(t,a))||!reflect.DeepEqual(jobs,ledger()){t.Fatal("rejected late reference changed complete domain/producer/outbox/audit rows")}
							}else{
								if err:=confirm();err!=nil{t.Fatal(err)}
								before.Artifacts=append(before.Artifacts,wantArtifact)
								committed:=storyboardWriterSnapshot(t,a)
								if !reflect.DeepEqual(storyboardUTC(before),storyboardUTC(committed)){t.Fatal("media writer changed unrelated complete rows")}
								jobs:=ledger()
								var referenced *ResourceDeletionReferencedError
								if err:=remove();!errors.As(err,&referenced){t.Fatalf("expected reference rejection after valid producer settlement, got %v",err)}
								want:=[]ResourceDirectReference{{Kind:"镜头产物",ID:"output",Title:"source-shot",ResourceID:"image"}}
								if !reflect.DeepEqual(referenced.References,want){t.Fatalf("wrong reference sources: %+v",referenced.References)}
								if !reflect.DeepEqual(committed,storyboardWriterSnapshot(t,a))||!reflect.DeepEqual(jobs,ledger()){t.Fatal("reference rejection changed complete domain/producer/outbox/audit rows")}
								for i:=0;i<4;i++{if reflect.ValueOf(jobs[i]).Elem().Len()!=0{t.Fatalf("rejected deletion leaked ledger rows %d",i)}}
							}
						})
					}
				})
			}
		})
	}
}
