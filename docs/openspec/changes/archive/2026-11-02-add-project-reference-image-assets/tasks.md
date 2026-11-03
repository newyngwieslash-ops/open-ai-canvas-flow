# Tasks

初版实现与独立审查修复已完成，正在补齐自动化覆盖与环境验收。勾选只表示该项全部要求已有证据；未勾不等于未实现，复合任务尚缺任一验证项即保留未勾。证据与缺口见 `.comet/verification-report.md`；SQLite 确定性交错不替代 PostgreSQL/多实例真实并发。规范以 specs/project-reference-image-assets/spec.md 为准，接口及事务合同见 design.md。

## 1. 实体创建与参考图版本

- [x] 1.1 新增 environment/prop 手动实体创建接口及 `{asset}` 返回，扩展既有角色 create；角色 create 支持 referenceImage、imageResourceId、imageRole 默认 portrait 和原子 folderId 关联；测试三分类、空名称、错误分类、外项目文件夹及事务回滚。 <!-- comet-task:270b073e-1d9a-42a4-8eb5-ff7c6ff89dfe -->
- [x] 1.2 新增设置单参考 PUT，要求 resourceId/expectedVersionId 非空且不可清除，摘要返回 referenceImage；测试 owned/ready/image/MIME/尺寸校验、版本冲突、成品和历史版本保留。 <!-- comet-task:365a8f22-15b0-4003-8ae1-2e114fca2351 -->
- [x] 1.3 角色编辑合并可编辑字段、保留受保护定义/声音/表现，并对相关版本写入增强数据库 CAS；测试编辑与采用竞争、受保护字段注入和普通照片不产生 turnaround_sheet。 <!-- comet-task:c6920fc6-59e5-4c3f-a6b8-6faff062a0f4 -->
- [x] 1.4 验证嵌套 referenceImage 与历史版本的资源引用保护及删除竞争；补专项测试，并增量更新 HTTP API 与 backend-database 的 JSON 合同说明，不新建表列。 <!-- comet-task:f9c8ed30-291c-4ce6-9e8b-4dfdc57e4eee -->

## 2. 生成模式与候选恢复

- [x] 2.1 在现有图片任务创建合同加入 manual/auto（缺省 auto），拒绝未知值，固化项目/目标/基准/原图或当前图来源；测试多参考/多输出、不支持参考模型及未知能力拒绝，确保不静默文生图 fallback。 <!-- comet-task:9a21330a-39fd-4c8b-baac-9ae76aef25b2 -->
- [x] 2.2 完成、媒体恢复、角色读取补偿统一遵守 manual：只存单图候选，不绑定；补失败/取消保留资产和旧 auto 自动绑定回归测试。 <!-- comet-task:ad0d8000-44c4-4002-b811-11f77a7e8588 -->
- [x] 2.3 实现 task center 候选安全投影 assetImageCandidate、previewUrl 与恢复查询，采用 created_at/id 倒序 keyset；测试 limit 前过滤、超过 100 条历史、同时间戳翻页、非法/越权游标、activeOnly 组合拒绝和账号隔离。 <!-- comet-task:1c43241e-b936-479d-8c8e-608245989a01 -->
- [x] 2.4 增量更新任务 API 文档，明确 `assetImageCandidates=true`、可选 assetId、pageSize=50、beforeTaskId 和数组返回；核对文档与请求/响应测试一致。 <!-- comet-task:db8716a4-5cae-4498-9cd2-e802d04b2a36 -->

## 3. 手动采用事务

- [x] 3.1 新增采用候选接口，服务端校验任务归属/项目/目标/manual/基准/成功单结果，资源仅取任务结果；测试跨用户、跨项目、目标错配、失败任务及资源伪造均不写版本。 <!-- comet-task:f9e005ae-8fdc-4437-bc7a-ef9f34d34232 -->
- [x] 3.2 复用版本事务执行 CAS、表现绑定、项目 revision 与任务采用投影；测试同任务重放、多实例双采用、同基准双候选、事务失败回滚以及 409 reason=project_asset_version_changed。 <!-- comet-task:601f7124-a98d-4179-8cf5-012fb1fa98d9 -->
- [x] 3.3 验证采用保留 referenceImage、声音及其他设定，普通输出仅 primary、明确三视图才附 turnaround_sheet；同步采用接口与幂等/冲突文档。 <!-- comet-task:0d32f0a1-f5e5-4fe1-904c-bb9e07081334 -->

## 4. 真实项目资产页面

- [x] 4.1 在 projects API 模块经 http 接入三接口与候选任务类型；专项测试信封解包、取消语义、错误 reason 与 `{asset}` 返回。 <!-- comet-task:baaf7bd2-ac10-49d7-9734-8cc09a3d4f8c -->
- [x] 4.2 统一角色/environment/prop 新建表单，提供两用途、单图上传/库选择、名称/文字要求/模型能力；补交互测试验证空名称、多图与不支持模型阻止提交。 <!-- comet-task:961f49ba-2c5a-4a88-8026-493544e4d8a4 -->
- [x] 4.3 已有资产展示原图/当前图显式选择与无参考文生图，预览候选后确认采用；测试失败保留成品、409 刷新提示、重复点击和服务器失败不显示已保存。 <!-- comet-task:ec90dbeb-aabe-4bcb-bf52-fc53939b5cbe -->
- [x] 4.4 从任务中心恢复候选并分页，展示 pending_confirmation/adopted，清理账号切换状态；测试关闭弹窗/刷新后恢复、已采用状态、过期候选以及同时间戳分页不丢失。 <!-- comet-task:10d75f77-d9ce-4670-a30a-99879bc2165a -->
- [x] 4.5 增量更新功能与 pending-test 专题，保留用户既有修改；验收文档清楚区分已实现、待验证和用户未确认事项。 <!-- comet-task:2ac729e2-c54c-4851-ab6f-7ed503e75359 -->

## 5. 集成验收

- [x] 5.1 执行后端受影响 app/repository/handler 专项测试及 SQLite/PostgreSQL 并发/引用集成验证；记录实际命令结果，不以静态阅读替代运行证据。 <!-- comet-task:09aaff2b-5921-4ad3-9389-488e530982b8 -->
- [x] 5.2 执行前端专项 Bun 测试和 `bun run build`；在真实路由检查三分类两用途、明暗主题、滚动/焦点、上传、刷新、失败与采用，条件不足项列待验证。 <!-- comet-task:076147bf-d2f8-47c6-ab73-08aba468e66e -->
- [x] 5.3 对照 spec 全部场景核验无新表列、旧 auto 和无参考回归；记录真实模型参考图链路是否验证，复查任务/资源不泄漏密钥与跨账号数据。 <!-- comet-task:a25c0ee5-ca5f-439b-a745-8146df981e27 -->

本任务不自动提交、不自动归档。
