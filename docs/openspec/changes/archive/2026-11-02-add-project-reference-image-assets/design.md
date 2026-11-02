# Design

## Context

动机见 proposal.md，验收合同见 specs/project-reference-image-assets/spec.md。首版实现已完成，正在独立审查修复与回归收尾；以下为实施合同，不表示最终验收通过。

设计基线中的自动绑定、普通照片误标三视图、角色版本并发及默认任务列表容量边界，是本次模式隔离、图片角色区分、版本 CAS 和目标任务查询要解决的问题，不再作为当前实现缺陷的描述。

本 change 的规范统一位于 `docs/openspec/changes/add-project-reference-image-assets/`；HTTP 与数据说明同步到 `docs/content/docs/`，验证证据集中记录在 `docs/content/docs/progress/pending-test.mdx`，不维护第二套合同根。

## Goals / Non-Goals

**Goals:** 在现有资产/任务/资源边界内分离原图、候选、成品；数据库事务承担 CAS、幂等和引用一致性；前端只编排，不负责决定归属或采用合法性。

**Non-Goals:** 不新建表列，不新建候选存储，不增加兼容字段层；不扩展到 material/other、自动识图、多图融合、批量或拆分；不创建独立 HTML；实施阶段必须运行专项测试，无真实模型条件时不得声称端到端通过。

## Decisions

### 1. 接口合同与责任分层

以下新增路由是本版实施合同，均在 `/api` 下，成功信封 data 为 `{ asset }`；handler 只解参，service/app 校验，repository 原子写入。

| 接口 | 输入与行为 |
| --- | --- |
| `POST /projects/:id/asset-entities` | category=environment/prop、非空 name、文字 definition、可选 referenceImage、可选 imageResourceId；原子创建实体/首版本/项目关联；角色走现有 create 接口 |
| `PUT /projects/:id/assets/:assetId/reference-image` | 非空 resourceId、expectedVersionId；本版不做清除，CAS 新建定义版本，保留成品 |
| `POST /projects/:id/assets/:assetId/image-candidates/:taskId/adopt` | 非空 expectedVersionId；结果资源由服务端读取任务，不接受客户端任意资源替换 |

现有角色 create 请求同步支持 `referenceImage: { resourceId }`、`imageResourceId`、`imageRole: portrait | turnaround_sheet`，默认 portrait；`folderId` 经项目归属校验后与首版本/项目关联原子保存。portrait 只写 primary；明确 turnaround_sheet 才双角色引用同一图。统一入口复用角色能力而不是复制角色模型，environment/prop 使用现有视觉资产版本结构。摘要增加可选 `referenceImage`。

新增写接口及角色编辑校验项目属于用户、资产属于用户且关联该项目、分类/基准版本有效。普通 definition 更新不得注入或清除受保护 referenceImage；服务端合并可编辑字段并保留保留字段。角色编辑携带 expectedVersionId；所有角色版本提交使用数据库旧主版本条件，而非仅进程互斥。

错误继续使用统一信封及机器可读 reason；版本不匹配返回 HTTP 409、reason=`project_asset_version_changed`，前端按 reason 分支，不解析 msg。

### 2. 存储原图，不把原图当表现

采用 `AssetVersion.DefinitionJSON.referenceImage.resourceId`，拒绝仅存前端 URL 或另建引用表。资源必须归当前用户、ready、image，并满足现有 MIME/尺寸检查。设置参考创建不可变新版本；保留当前表现、声音和其他定义；历史版本继续保护原图。提交时锁定/重新检查资源，防止校验与物理删除竞争。验证现有 JSON 引用遍历确实识别此嵌套字段及历史版本，不能只凭扫描代码存在便宣称通过。

直接使用成品仅绑定选图；参考图生成先持久化原图，再使用返回的新版本作为生成基准。生成失败不撤销已经明确保存的设定/参考，不覆盖已有成品。新建失败不得留下半个实体；上传后尚未引用的资源遵循既有资源生命周期，不自动删除用户素材。

### 3. 复用图片任务，显式 manual adoption

扩展现有图片任务输入而非新增生成通道：`adoptionMode: manual | auto`，缺省 auto，未知值拒绝。服务端校验并固化项目、目标资产、基准版本、参考来源 `metadata.referenceSource: original | current | none`（两端字段同名）、单参考资源与输出语义。沿用既有 operation（角色/场景道具）但统一解析 adoptionMode，不让客户端 metadata 绕过校验。

original 从基准定义读取，current 从基准 primary 读取，none 不传参考；选择 current 不覆写已存原图。创建阶段强校验模型支持参考输入（系统模型及自定义渠道均不得静默忽略），只允许一张参考、一个输出，能力未知按不支持处理。失败必须发生在入队/收费上游调用之前。

manual 完成仅保存资源和 Task.ResultJSON 候选，不调用自动绑定。worker、媒体恢复及 `reconcileCharacterTurnaroundTasks` 均统一走模式分支，避免刷新偷偷采用。旧 auto 路径保留，包括缺省调用；角色自动版本写入也获得并发保护。拒绝用生成成功立即改资产再“撤销”的方案，因为它会短暂污染成品且无法可靠回滚。

### 4. 候选以任务为事实来源

候选由任务中心列表/详情读取，不新增 ProjectAssetCandidate 记录。任务输出增加可安全暴露的候选信息：目标资产、基准版本、`state: pending_confirmation | adopted`、可选 adoptedVersionId。成功还必须有一张已物化 ready image，未下载完成不能采用。

恢复接口固定为 `GET /api/tasks?projectId=...&assetImageCandidates=true&assetId=...&pageSize=50&beforeTaskId=...`，assetId 可选、首屏不传游标，后续传上一页末任务 ID。返回 `TaskSummary[]`，新增 `assetImageCandidate`，用 `previewUrl` 预览。数据库 limit 前按用户/项目/可选目标资产及候选范围过滤，包含已结束任务；按 `created_at DESC,id DESC` keyset 分页，少于 pageSize 结束。非法/越权游标及与 activeOnly 组合均拒绝，不能先截断最近任务再前端筛选。详情只返回安全投影，不暴露密钥输入。前端弹窗刷新后重查，并区分生成中、失败、待确认、已采用及版本已过期。采用状态以任务关联的表现/版本为准，必要时更新 ResultJSON 投影；不能先标 adopted 再独立提交版本。

刷新期间在途检测和完成轮询另用 `GET /api/tasks?projectId=...&activeOnly=true&assetId=...&pageSize=1`，返回活动任务摘要数组。服务端校验用户、项目、资产归属及项目资产关联，数据库 limit 前完成目标资产及活动状态过滤，不能先取全项目最近 100 条再前端过滤。该查询与成功候选 keyset 分离，不携带 `assetImageCandidates=true` 或 `beforeTaskId`；活动结束后重查成功候选，空活动列表不代表成功或已采用。

### 5. 采用事务与幂等

采用顺序：鉴权与项目关联 → 校验任务所有者/项目/目标/manual/成功单结果 → 检查结果资源归属与就绪 → 在事务内检查同任务已有采用 → 比较任务基准=request.expectedVersionId=当前主版本 → 创建版本/表现并 CAS 推进主版本 → 刷新关联项目 revision。角色保留声音及 definition；场景/道具生成输出只 primary，角色生成沿用三视图输出并另有 turnaround_sheet，不新增 portrait 生成；portrait 仅用于普通成品直用。

用已有 task_id/role 关联及唯一约束实现幂等；重复请求先确认同任务确实已采用到同目标，再返回当前资产摘要及可关联的原 adoptedVersionId，不再次推进，也不把前端回退到旧资产版本。跨实例竞争依赖数据库条件与约束，不依赖 mutex。事务失败整体回滚；冲突保留任务候选、不重新生成、不覆盖最新资产。

## Risks / Trade-offs

- [旧读取补偿误自动绑定 manual] → worker、recovery、reconcile 共用模式解析并分别补回归测试。
- [角色编辑整块替换 definition 丢原图或绕过归属] → 保留受保护字段，引用只经专用校验路径修改，CAS 覆盖全部相关版本写入。
- [候选被列表截断或只存在弹窗] → task center 目标过滤/分页及详情恢复；大量历史任务场景验收。
- [资源删除与新引用竞争] → 事务内资源检查/锁及历史 JSON 引用保护，SQLite/PostgreSQL 分别验证。
- [旧调用兼容与安全增强冲突] → 只保留缺省 auto 行为；并发冲突不能继续后写覆盖，返回可读冲突而不重发上游请求。
- [旧 Skill 与当前 CLI 的产物布局不一致] → 使用当前 CLI 的配置根包装命令及正常 guard，不调用旧版脚本绕过校验。

## Migration Plan

无 DDL 或数据回填。先实现后端合同、模式隔离和安全测试，再接真实前端；实现阶段更新 HTTP API、backend-database JSON 说明和 pending-test，保留用户既有改动。上线前确认所有完成/恢复入口理解 manual。

回退优先关闭新入口、保留理解 manual 的后端。不可直接回退到不认识 manual 的旧 worker：它可能把未采用候选自动绑定；必要时先停 worker 并处理待完成 manual 任务，保留任务/原图/版本数据，不做破坏性清理。

## 待验证项

已有自动验证不等同最终验收：前端 18 项弹窗及 18 项 API/helper/既有回归共 36 项通过，构建通过（仅 chunk 大小警告）；后端原八组与五组 review 回归及旧回归综合 suite 三包通过，后端构建通过。原四项后端、两项前端及新增活动任务投影 P2 已独立复核关闭，仍在补充自动化覆盖缺口。详细证据、命令与耗时见 `.comet/verification-report.md` 及 `docs/content/docs/progress/pending-test.mdx`。

真实登录态 DOM/截图已部分检查新建弹窗、三分类、场景选择、用途切换、空输入禁用和原图/成品分区；未提交、上传或付费调用，暗色、窄屏及完整交互仍未验收。本地运行后端 OpenAPI 尚无新接口，本轮未重启，须重启后再真实联调。SQLite 确定性交错覆盖不能替代 PostgreSQL/多实例真实并发；未执行真实付费生成，不声称端到端或所有 spec 场景通过。
