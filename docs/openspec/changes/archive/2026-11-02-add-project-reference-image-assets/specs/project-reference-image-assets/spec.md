# Spec Delta

## Purpose

为项目角色、场景和道具提供统一的单参考图创作能力，明确原始参考、生成候选与已采用成品之间的边界，使刷新恢复、资源归属、版本并发和失败处理均具有可验证的行为合同。

## ADDED Requirements

### Requirement: 统一手动创建与两种图片用途

系统 SHALL 提供 character、environment、prop 的统一新建入口，区分直接使用成品与参考图生成。名称去除首尾空白后 MUST 非空；参考图生成 SHALL 接收文字要求和模型选择，不从图片自动推断名称或设定。

#### Scenario: 三类资产均可手动创建
- **WHEN** 用户分别以角色、场景、道具分类提交有效名称和文字设定
- **THEN** 服务端建立对应实体、首版本及项目关联，返回可继续编辑的资产

#### Scenario: 空名称不能持久化
- **WHEN** 名称仅含空白
- **THEN** 请求失败且不创建资产、版本或项目关联

### Requirement: 单图来源与直用语义

系统 SHALL 支持本地上传或素材库选择一张图片。普通照片直用 MUST 仅生成 primary 表现；仅明确声明三视图的角色图片才能同时具有 turnaround_sheet 表现。第一版 SHALL 限制单参考和单输出，不执行自动识图、融合、批量或拆分。

#### Scenario: 普通照片不是三视图
- **WHEN** 用户直用一张普通角色照片，未显式选择三视图
- **THEN** 资产有 primary 表现且无 turnaround_sheet 表现，不发起生成任务

#### Scenario: 明确三视图
- **WHEN** 用户显式声明所选角色成品是三视图，或在已有角色成品已绑定后切换“包含角色三视图”并再次保存成品资产
- **THEN** 同一图片可以作为 primary 和 turnaround_sheet（或按当前勾选更新是否包含 turnaround_sheet），弹窗保留打开且同步三视图勾选初始状态，不拆分成多张图片

#### Scenario: 多图被拒绝
- **WHEN** 请求包含多张参考或要求多个输出
- **THEN** 服务端拒绝请求而非静默截取第一张

### Requirement: 原图与成品分离且引用受保护

系统 SHALL 将受校验原图保存为版本定义的 referenceImage.resourceId，并在资产摘要中返回 referenceImage；设置原图 MUST 要求非空 resourceId、expectedVersionId，本版不支持清除。设置原图 SHALL 保留当前成品与历史版本，后续采用不得把原图替换成生成结果。

#### Scenario: 替换原图不覆盖成品
- **WHEN** 用户以当前基准版本设置另一张有效参考图
- **THEN** 系统创建新版本，原成品保持不变，摘要返回新的参考图，历史原图仍被历史版本引用

#### Scenario: 原图不能被物理误删
- **WHEN** 资源被当前或历史版本的 referenceImage 引用并收到删除请求
- **THEN** 系统保留资源并返回引用来源，不因它不是成品表现而删除

#### Scenario: 非法资源或清除请求
- **WHEN** 上传未完成、资源不是图片、属于另一用户，或设置请求任一必填 ID 为空
- **THEN** 请求失败，不产生新版本或部分引用

### Requirement: 参考生成能力与来源必须明确

带参考图的生成请求 MUST 在任务创建时校验模型参考图能力，不支持或无法确认时拒绝，不得静默退化为文生图。已有资产重生成 SHALL 明确选择原图或当前成品；无参考时 SHALL 保留文生图入口。

#### Scenario: 不支持参考图的模型
- **WHEN** 用户选择仅支持文生图的模型并提交参考图
- **THEN** 服务端在入队和上游调用前拒绝，前端解释能力不匹配

#### Scenario: 原图与当前图各自可选
- **WHEN** 资产同时有原图和当前成品，用户明确选择其中之一重生成
- **THEN** 任务使用所选单图，记录来源和基准版本，不修改已保存原图

#### Scenario: 无参考仍可生成
- **WHEN** 用户明确选择无参考并提供文字要求
- **THEN** 系统使用原有文生图能力，不虚构参考资源

### Requirement: 显式手动候选与旧调用隔离

现有生成接口 SHALL 接受 adoptionMode 为 manual 或 auto；缺省等同 auto，未知值 MUST 拒绝。manual 任务成功只保存候选与图片资源，不绑定资产；完成、恢复、项目读取补偿路径 MUST 遵守同一模式。失败或取消 SHALL 保留原资产。

#### Scenario: 手动成功但尚未采用
- **WHEN** manual 任务完成并保存单张图片
- **THEN** 候选状态为 pending_confirmation，资产主版本、成品与原图不变

#### Scenario: 恢复不能自动采用
- **WHEN** manual 任务经媒体恢复或项目读取补偿重新处理
- **THEN** 仍只保留候选，不自动创建资产成品版本

#### Scenario: 老调用行为不变
- **WHEN** 既有调用未传 adoptionMode 或明确传 auto
- **THEN** 继续按原有生成后自动绑定路径处理，而不是等待手动确认

#### Scenario: 失败与未知模式
- **WHEN** 生成失败、取消，或请求携带未知 adoptionMode
- **THEN** 原资产不变；未知模式在创建任务前被拒绝

### Requirement: 候选由任务中心恢复

系统 SHALL 从 task center 读取候选，不把弹窗内存或本地缓存当作唯一来源。候选 SHALL 暴露 pending_confirmation 或 adopted 状态，已采用时可返回 adoptedVersionId；读取 MUST 按用户、项目和目标资产隔离。

#### Scenario: 刷新后继续采用
- **WHEN** 用户关闭弹窗并刷新页面，再打开同一资产
- **THEN** 可从任务中心找回成功但未采用候选，并继续预览/采用；不得只查询活动任务或仅依赖最近固定数量任务

#### Scenario: 已采用候选恢复
- **WHEN** 候选已经采用后刷新
- **THEN** 显示 adopted，能够关联已采用版本，不能把它展示为新的待确认候选

#### Scenario: 刷新期间检测目标资产在途任务
- **WHEN** 用户刷新后重新打开资产，或等待该资产任务完成
- **THEN** 使用独立 `GET /api/tasks?projectId=...&activeOnly=true&assetId=...&pageSize=1` 查询；服务端 MUST 校验用户、项目及资产归属和项目资产关联，并在数据库 limit 前过滤目标资产和活动任务，不得取全项目最近 100 条后由前端筛选
- **AND** 活动查询不与 `assetImageCandidates=true` 或成功候选 `beforeTaskId` keyset 混用；结束后重新读取成功候选，活动列表为空不代表生成成功或已采用

### Requirement: 采用校验与原子版本提交

采用接口 MUST 校验任务归属、项目、目标资产、manual 模式、基准版本、成功状态及唯一就绪图片结果；资源只能取自该任务结果。采用 SHALL 通过 CAS 原子创建版本并绑定成品，重复采用同一任务不重复建版；版本冲突返回 HTTP 409 与 reason project_asset_version_changed。

#### Scenario: 成功采用
- **WHEN** 当前主版本等于任务基准和请求 expectedVersionId，且用户确认有效候选
- **THEN** 仅创建一个新版本、绑定成品、保留 referenceImage 和其他定义，候选变为 adopted

#### Scenario: 并发修改或双候选竞争
- **WHEN** 生成后资产被编辑，或同一基准的另一个候选先被采用
- **THEN** 旧候选采用返回指定 409，保留候选和最新资产，不强制覆盖或重新发起收费生成

#### Scenario: 跨用户跨项目或伪造结果
- **WHEN** 请求引用他人任务、另一项目/资产任务、失败任务或任意非任务结果资源
- **THEN** 服务端拒绝且不创建版本、不泄漏他人资源

#### Scenario: 重复采用与事务失败
- **WHEN** 同一候选重复采用，或提交过程中发生写入失败
- **THEN** 重复请求返回既有采用结果；写入失败整体回滚，不留下半绑定版本

### Requirement: 角色编辑保留受保护数据

角色编辑 SHALL 保留 referenceImage 等服务端受保护字段以及未被编辑的表现与声音绑定，不允许通用 definition 请求绕过参考图校验；修改 SHALL 使用版本 CAS 防止与采用互相覆盖。

#### Scenario: 编辑名称不丢原图
- **WHEN** 带原图和声音绑定的角色修改名称或文字设定
- **THEN** 新版本保留原图、声音与未修改表现，客户端遗漏保留字段不导致清除

#### Scenario: 编辑与采用并发
- **WHEN** 角色编辑和采用使用相同基准版本并发提交
- **THEN** 最多一个成功，另一操作返回版本冲突，不出现后写覆盖
