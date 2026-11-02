---
change: add-project-asset-batch-generation
design-doc: docs/superpowers/specs/2026-11-02-project-asset-batch-generation-design.md
base-ref: 8de9ffc9df5e0e5c969406eb271868a9cc4e66d0
archived-with: 2026-11-02-add-project-asset-batch-generation
---

# 项目多个资产单候选批量生成实施计划

> **面向执行者：** 请按任务顺序执行本计划，每项任务勾选与 OpenSpec `tasks.md` 通过 `comet-task` 标识一一映射。

**目标：** 为项目角色、场景和道具资产提供服务端可追踪、可恢复的单候选批量生成能力（Schema V62 两表模型、清单预检与整批重验、幂等提交、停止未提交与取消已提交分离、安全重试与逐项手动采用）。

**架构：** 后端新增 `ProjectAssetGenerationBatch` 与 `ProjectAssetGenerationBatchItem` 两张表及唯一索引，通过 `preflight` 计算确定性摘要与费用边界，提交时整批重验并通过 `submissionKey == Task.CreationSubmissionID` 原子绑定任务；前端在项目资产工作台提供多资产勾选、预检确认、持久批次恢复、停止/取消与逐项采用交互。

**技术栈：** Go 1.25、Gin、GORM、SQLite/PostgreSQL、React 19、TypeScript、TanStack Query、Bun Test。

---

## 1. 持久合同与预检

### Task 1.1: 新增 batch/item 模型、唯一索引及 Schema V62 迁移

- [x] 1.1 新增 batch/item 模型、唯一索引及迁移；SQLite/PostgreSQL 测试验证同键重放/异参冲突、重复资产和安全快照，同步 backend-database.mdx。 <!-- comet-task:5e9d2106-b310-4c1a-8ad4-71380b90b5f7 -->

**涉及文件：**
- 修改：`backend/internal/model/models_project.go`
- 修改：`backend/internal/database/migrations.go`
- 新增：`backend/internal/repository/project_asset_batch.go`
- 修改：`docs/content/docs/backend/backend-database.mdx`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

### Task 1.2: 实现预检/提交/恢复 API 与整清单重验

- [x] 1.2 实现预检/提交/恢复 API 与整清单重验；测试覆盖权限、基准、能力、额度、费用未知及价格变化，并同步 HTTP API 和错误 reason。 <!-- comet-task:3d83510f-baa1-4787-adc7-cd81d6190866 -->

**涉及文件：**
- 新增：`backend/internal/app/project_asset_batch.go`
- 修改：`backend/internal/service/aliases_types.go`
- 修改：`backend/internal/handler/project_asset_routes.go`
- 修改：`backend/internal/handler/openapi.yaml`
- 修改：`docs/content/docs/backend/http-api.mdx`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

### Task 1.3: 实现安全输入快照与资源引用保护

- [x] 1.3 实现安全输入快照与资源引用保护；资源测试覆盖 pending 项删除竞争、停止后清理和历史资产保留，核对不保存明文密钥。 <!-- comet-task:643a1576-3af5-4ba0-bd44-ccef46a6a634 -->

**涉及文件：**
- 修改：`backend/internal/repository/project_reference_image.go`
- 修改：`backend/internal/app/project_asset_batch.go`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

## 2. 派发恢复与取消

### Task 2.1: 复用队列/额度并原子绑定 item 与唯一任务提交身份

- [x] 2.1 复用队列/额度并原子绑定 item 与唯一任务提交身份；并发及故障注入测试覆盖重复领取、响应丢失、重启和禁止重复收费。 <!-- comet-task:e83061eb-ff18-48ae-9b50-7d7a39eed43b -->

**涉及文件：**
- 修改：`backend/internal/app/project_asset_batch.go`
- 修改：`backend/internal/repository/project_asset_batch.go`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

### Task 2.2: 实现状态聚合、部分失败重试资格和待核对状态

- [x] 2.2 实现状态聚合、部分失败重试资格和待核对状态；测试覆盖部分成功、费用不确定禁止重发、重试历史及每项最多一活跃任务。 <!-- comet-task:7efafe22-c714-49f5-a212-0d89b5321f45 -->

**涉及文件：**
- 修改：`backend/internal/app/project_asset_batch.go`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

### Task 2.3: 分离停止未提交与取消已提交 API

- [x] 2.3 分离停止未提交与取消已提交 API；竞争测试覆盖 stop/领取边界、取消不支持/未确认、迟到完成和成功候选保留，同步取消语义文档。 <!-- comet-task:f249ea1a-3b17-43dd-a9f9-c06175716114 -->

**涉及文件：**
- 修改：`backend/internal/app/project_asset_batch.go`
- 修改：`backend/internal/handler/project_asset_routes.go`
- 修改：`docs/content/docs/backend/http-api.mdx`
- 测试：`backend/internal/app/project_asset_batch_test.go`

---

## 3. 真实批量界面

### Task 3.1: 实现真实项目资产多选、清单预检、费用确认和幂等提交

- [x] 3.1 实现真实项目资产多选、清单预检、费用确认和幂等提交；组件/API 测试覆盖三分类、重复点击、无效项、模型不支持及版本过期。 <!-- comet-task:d1a7506c-57f0-45ec-afd6-b3e01de4cb68 -->

**涉及文件：**
- 修改：`web/src/services/api/projects.ts`
- 新增：`web/src/pages/projects/detail/project-asset-batch.ts`
- 新增：`web/src/pages/projects/detail/project-asset-batch-modal.tsx`
- 修改：`web/src/pages/projects/detail/assets.tsx`
- 测试：`web/test/project-asset-batch.test.tsx`

---

### Task 3.2: 实现持久批次进度/恢复、逐项错误/重试与两种取消操作

- [x] 3.2 实现持久批次进度/恢复、逐项错误/重试与两种取消操作；组件测试覆盖刷新、换账号、部分成功及不确定项，更新功能/pending-test。 <!-- comet-task:e3340764-79e6-4526-9343-c3ee38c69531 -->

**涉及文件：**
- 修改：`web/src/pages/projects/detail/project-asset-batch-modal.tsx`
- 修改：`docs/content/docs/overview/features.mdx`
- 修改：`docs/content/docs/progress/pending-test.mdx`
- 测试：`web/test/project-asset-batch.test.tsx`

---

### Task 3.3: 接入既有单候选预览采用

- [x] 3.3 接入既有单候选预览采用；测试确认无批量自动采用、CAS 冲突保留候选、采用重放不重复版本且声音/参考不变。 <!-- comet-task:5ef0d794-d820-421e-99d1-585da634cc89 -->

**涉及文件：**
- 修改：`web/src/pages/projects/detail/project-asset-batch-modal.tsx`
- 测试：`web/test/project-asset-batch.test.tsx`

---

## 4. 跨层验收

### Task 4.1: 真实页面可控任务 Mock 全闭环验证

- [x] 4.1 在真实页面用可控任务 mock 验证三个资产各一候选、部分失败、断线重载、停止/取消及手动采用，记录明暗主题/滚动/键盘证据；无收费授权不调真实模型。 <!-- comet-task:8cfa5850-9c6e-41ca-8adb-26533c10fbf3 -->

**涉及文件：**
- 测试：`web/test/project-asset-batch.test.tsx`

---

### Task 4.2: 专项测试、类型检查与迁移恢复演练记录

- [x] 4.2 执行最小相关测试与必要类型/构建检查，演练数据库迁移及跨实例故障恢复；记录实际结果和费用对账未覆盖项，不宣称未执行的端到端通过。 <!-- comet-task:d35585bb-8e5f-494e-8b67-467a28ce9730 -->

**涉及文件：**
- 修改：`docs/content/docs/progress/pending-test.mdx`
