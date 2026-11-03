---
change: add-project-reference-image-assets
design-doc: docs/superpowers/specs/2026-10-22-project-reference-image-assets-design.md
base-ref: c9656bc9ce9aa805f61e2e6107bbbe8e279231da
archived-with: 2026-11-02-add-project-reference-image-assets
---

# 项目参考图生成资产实现计划

状态：初版实现与独立审查修复已完成，已发现问题复审关闭，正在补充自动化覆盖及待环境验收。前后端专项和构建已有通过证据；PostgreSQL/多实例真实并发、真实付费生成及完整浏览器交互尚未验证，本地运行后端需重启加载新接口。证据见 `docs/openspec/changes/add-project-reference-image-assets/.comet/verification-report.md`。完整可追踪清单见 `docs/openspec/changes/add-project-reference-image-assets/tasks.md`；规范为同 change 的 `specs/project-reference-image-assets/spec.md`。不自动提交或归档，不创建产品 HTML，不改项目配置。当前分支保留所有无关修改，后端与前端以固定接口分工，专项测试及标准审查记录实际证据。

## 1. 资产合同与版本安全

- 完成本节接口与版本安全实施及对应专项验证，逐项对照 OpenSpec tasks 第 1 组（`270b073e-1d9a-42a4-8eb5-ff7c6ff89dfe`、`365a8f22-15b0-4003-8ae1-2e114fca2351`、`c6920fc6-59e5-4c3f-a6b8-6faff062a0f4`、`f9c8ed30-291c-4ce6-9e8b-4dfdc57e4eee`）。

在项目资产 handler/app/repository 与 projects API 层实现手动实体创建、设置单参考、采用候选三接口，统一 data={asset}。角色 create 支持 referenceImage、imageResourceId、imageRole 默认 portrait、folderId 原子关联；摘要返回 referenceImage。角色编辑保留受保护字段并强化 CAS。设置参考要求 resourceId/expectedVersionId 非空，不支持清除。

验收：三分类、空名称、外项目文件夹、跨账号资源、未就绪/非图片、普通照片仅 primary、明确三视图、编辑保留原图/声音、历史资源删除保护。随实现更新 HTTP API 和数据库 JSON 合同专题，不改表列。

## 2. 任务候选与采用

- 完成任务模式、恢复及采用事务，执行 OpenSpec tasks 第 2、3 组安全与并发测试（`9a21330a-39fd-4c8b-baac-9ae76aef25b2`、`ad0d8000-44c4-4002-b811-11f77a7e8588`、`1c43241e-b936-479d-8c8e-608245989a01`、`db8716a4-5cae-4498-9cd2-e802d04b2a36`、`f9e005ae-8fdc-4437-bc7a-ef9f34d34232`、`601f7124-a98d-4179-8cf5-012fb1fa98d9`、`0d32f0a1-f5e5-4fe1-904c-bb9e07081334`）。

扩展现有图片任务 adoptionMode=manual|auto（缺省 auto，未知值拒绝），单参考单输出；参考模型能力不支持必须在创建时失败。manual 在完成、媒体恢复、读取补偿均不自动绑定。采用从成功任务取唯一结果，校验项目/目标/基准/归属，事务 CAS+幂等；冲突 409 reason=project_asset_version_changed。

验收：失败/取消不覆盖、双采用/双候选/编辑竞争、事务回滚、伪造任务资源拒绝、旧 auto 回归、无参考文生图。同步任务与采用 API 文档。

## 3. 候选查询与真实前端

- 完成候选分页和真实页面接入，验证刷新恢复与账号隔离，对照 OpenSpec tasks 第 4 组（`baaf7bd2-ac10-49d7-9734-8cc09a3d4f8c`、`961f49ba-2c5a-4a88-8026-493544e4d8a4`、`ec90dbeb-aabe-4bcb-bf52-fc53939b5cbe`、`10d75f77-d9ce-4670-a30a-99879bc2165a`、`2ac729e2-c54c-4851-ab6f-7ed503e75359`）。

任务中心增加 `assetImageCandidates=true`、可选 assetId、pageSize=50、beforeTaskId，返回 TaskSummary[] 的 assetImageCandidate 与 previewUrl；limit 前过滤、created_at/id 倒序 keyset，非法游标与 activeOnly 组合拒绝。统一新建入口、单图上传/库选择、两用途、名称/文字要求/模型筛选；已有资产显式原图/当前图/无参考，预览后采用。

验收：超过默认任务容量和同时间戳翻页、刷新恢复 pending_confirmation/adopted、账号隔离、重复点击、409 提示、失败不显示已保存。补前端专项测试，增量更新功能/pending-test 文档，不覆盖用户已有修改。

## 4. 集成与证据

- 执行 OpenSpec tasks 第 5 组集成验收，记录真实结果与未验证项，不把文档校验视作产品通过（`09aaff2b-5921-4ad3-9389-488e530982b8`、`076147bf-d2f8-47c6-ab73-08aba468e66e`、`a25c0ee5-ca5f-439b-a745-8146df981e27`）。

实施时运行最小充分后端专项测试、SQLite/PostgreSQL 并发与资源引用验证，前端 `bun test <新增专项文件>`、`bun run build`。真实路由验收三分类两用途、明暗主题、弹窗滚动/键盘焦点、刷新恢复及采用；真实模型参考输入需单独验证。缺少环境或凭证时记待验证，不以静态文档/独立 HTML 宣称通过。

回退先关闭新入口并保留识别 manual 的后端；禁止让旧 worker 自动采用历史 manual 任务。规划与状态统一在项目配置的 docs/openspec 根，使用正常 Comet guard 和真实验证证据推进。
