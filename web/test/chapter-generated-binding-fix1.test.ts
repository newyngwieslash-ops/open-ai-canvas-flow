import { expect, test } from "bun:test";
import { apiClient } from "@/services/api/request";
import { chapterStoryboardFromGenerationTask } from "@/pages/projects/detail/project-chapter-ai";
import { saveGeneratedChapterStoryboard } from "@/pages/projects/detail/chapter-storyboard-production";

test("over-capacity generated row saves the user's seventh-version choice and cancellation writes nothing", async () => {
 const original=apiClient.defaults.adapter;
 const versions=Array.from({length:7},(_,i)=>`v${i+1}`);
 const resolution={taskId:"t",baseline:{},rows:[{rowIndex:0,assetVersionIds:versions,candidates:versions.map((v)=>({assetId:v,name:v,assetVersionId:v,category:"prop",bindable:true,missingImage:true,defaultSelected:false,issues:[]})),issues:[{kind:"capacity_exceeded",message:"choose at most six"}]}]};
 const writes:any[]=[];
 apiClient.defaults.adapter=async(config)=>{if(config.method==="put")writes.push(JSON.parse(config.data));return {config,status:200,statusText:"OK",headers:{},data:{code:0,data:resolution,msg:""}};};
 const result=chapterStoryboardFromGenerationTask({id:"t",type:"canvas_text",status:"succeeded",prompt:"",attempts:1,createdAt:"",updatedAt:"",resultJson:JSON.stringify({rows:[{plotDescription:"door"}]})});
 const detail={project:{id:"p"},assets:[],shots:[]} as never;
 try {
  await saveGeneratedChapterStoryboard(detail,"u",result,async()=>[ ["v7"] ] as never);
  expect(writes[0].shots[0].assetVersionIds).toEqual(["v7"]);
  writes.length=0;
  await expect(saveGeneratedChapterStoryboard(detail,"u",result,async()=>false)).rejects.toMatchObject({name:"AbortError"});
  expect(writes).toHaveLength(0);
 }finally{apiClient.defaults.adapter=original;}
});
