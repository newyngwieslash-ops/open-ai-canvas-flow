# RunningHub 视频生成实施指引

按项目现状分阶段落地 RunningHub 视频生成能力。阶段 A 零代码（平台已有完整支持），阶段 B 起才涉及代码改动。每步给出改动文件、验证方式和完成标准。

前置阅读：[RunningHub 开放 API 调研](../design/runninghub-api-research.md)（API 端点、认证、上传、坑点）。

## 现状结论

- **工作流视频生成已内置**：接口类型 `runninghub-workflow-video`（`backend/internal/model/models.go:85`）、执行器 `backend/internal/app/workflow_provider*.go`、设置页 `web/src/pages/settings/runninghub-settings-pane.tsx`、画布切换面板 `web/src/components/canvas/canvas-config-node-panel.tsx` 均已实现，走 v1 `POST /task/openapi/create` + `nodeInfoList` 模式。
- **未覆盖**：RunningHub v2「标准模型直调」（`POST /openapi/v2/{vendor}/{model}/{task}`，300+ 视频模型免拼 ComfyUI JSON），需要阶段 B 新增。
- 图生视频、首尾帧、参考图/视频/音频/蒙版上传、任务轮询恢复、结果落资源存储，现有链路全部支持。

---

## 阶段 A：零代码启用工作流视频生成

目标：用一个 RunningHub 平台上的视频工作流（或 AI App）在画布跑通图生视频。

### A1. 准备 RunningHub 平台侧

1. 注册 runninghub.cn（或国际站 runninghub.ai），创建两把 API Key：
   - **积分 API Key**：拉取工作流参数、提交任务、查询结果都用它。提交固定用这把 Key；填错成企业级上传 Key 会报「企业版余额不足」（`workflow_provider.go:193` 有专门提示）。
   - **素材上传 API Key（企业级）**：仅上传参考图/视频/音频/蒙版时需要；纯文生视频可留空。
2. 在 RunningHub 平台准备一个**视频**工作流：
   - 方式一：用平台「AI API」里的官方视频工作流模板，复制得到自己的 `workflowId`；
   - 方式二：在 ComfyUI 调好工作流（含文生视频/图生视频分支），通过平台「工作流 API」上传，得到 `workflowId`；
   - 方式三：用平台 AI 应用（AI App），拿 `webappId`。
3. 记下工作流里关键节点的参数：prompt 所在节点 ID/字段名、参考图 `LoadImage` 节点、时长/分辨率等可调字段。v1 模式必须通过 `nodeInfoList` 替换节点参数，所以这些节点信息就是后续字段映射的原料。

### A2. 在影策设置页配置

入口：设置 → 「RunningHub 工作流」（`web/src/pages/settings/index.tsx:24`）。前置条件：插件页启用「RunningHub 工作流」插件（内置 ID `runninghub-workflow-provider`，`web/src/lib/plugins/builtin/workflows.ts:4`；未启用时画布和后端都会拒绝，校验在 `backend/internal/app/workflow_plugins.go:109`）。

按顺序：

1. 打开总开关，Base URL 选中国站或国际站。
2. 填积分 API Key、（有参考素材时）企业级上传 Key。
3. 「工作流用途」选**视频**；「类型」选 Workflow（或 App）；填 workflowId（或 webappId）。
4. 点「拉取参数」——前端走 `POST /runninghub/workflow-info`（`web/src/services/api/runninghub.ts:10`），后端代理调 RunningHub 管理接口拉回 ComfyUI JSON 与字段列表，合并保存进本地配置（`runninghub-settings-pane.tsx:113`）。拉取成功后自动进入字段映射。
5. 「测试画布」标签页填 prompt 和参考图直接试跑（`web/src/components/workflow-test-workbench.tsx:138`，走真实后端任务链路）。这一步通过再进画布，避免在画布上反复试错。

### A3. 字段映射（决定生成质量的关键步）

字段映射把画布输入接到工作流节点，每条映射 = `nodeId + fieldName + source`（类型定义 `web/src/stores/config-workflow-fields.ts:6`）。拉取参数后编辑器会自动推断（`workflowFieldsFromManagement`），通常只需检查：

| 画布输入 | source 取值 | 说明 |
| --- | --- | --- |
| 提示词 | `prompt` | 必映射到主 prompt 节点；未映射时后端会自动猜测 text 节点兜底（`workflow_provider_values.go:848`），但别依赖兜底 |
| 参考图 | `referenceImage` | 图生视频入口；后端先传 RunningHub 上传接口换文件名再填节点（`workflow_provider.go:236`） |
| 视频时长 | `videoSeconds` / `duration` | |
| 比例 / 分辨率 / 宽高 | `aspectRatio` / `resolution` / `sizeWidth` / `sizeHeight` | |
| 生成音频开关 | `videoGenerateAudio` | |
| 负面提示词 | 映射到负向 prompt 节点，值留空 | |

完整 source 枚举见 `backend/internal/app/workflow_provider_values.go:331-362`。媒体类字段（source 为 referenceImage/referenceVideo/referenceAudio/mask 或 fieldType IMAGE/VIDEO/AUDIO）会触发上传逻辑并要求企业级 Key（`runninghub-settings-pane.tsx:47` 的提示同理）。

**完成标准**：测试画布跑通一次视频生成，产物出现在任务结果里。

### A4. 画布上使用

1. 画布节点面板 → 生成配置 → 提供方切到「RunningHub」（`canvas-config-node-panel.tsx:249`）。
2. 选已保存的视频工作流/App；面板按当前节点模式过滤条目（视频模式只列 video 条目，`canvas-config-node-panel.tsx:127`）。
3. 连线参考图（图生视频）或直接输入提示词（文生视频）→ 生成。

数据流（排障用）：画布 `runBackendGenerationTask`（`web/src/services/api/generation-task.ts:74`）→ `POST /api/tasks` → 后端 worker `processTask` → `runWorkflowProviderTask`（`workflow_provider.go:71`）→ 上传媒体 → `POST /task/openapi/create`（或 `/task/openapi/ai-app/run`）→ 轮询 `POST /task/openapi/outputs`（2.5s 步进，总超时 1 小时，`workflow_provider_outputs.go:95`、`provider.go:104`）→ 下载产物 → 落资源存储写回任务结果。

任务恢复：worker 重启后按 `resumedProviderRequestID` 直接续轮询，不重复提交（`workflow_provider.go:105`）。

---

## 阶段 B：接 v2 标准模型直调（需要写代码）

目标：不拼 ComfyUI JSON，直接调 RunningHub 托管的 300+ 官方视频模型端点（Seedance、可灵、海螺、Vidu 等）。v2 形态与现有 `provider_video.go` 提交-轮询模式同构，但每个模型是独立路径 `POST /openapi/v2/{vendor}/{model}/{task}`，与声明式插件「单 provider 单 path 模板」的假设冲突，所以**走宿主执行器扩展**而非声明式插件。

### B1. 新增接口类型

- `backend/internal/model/models.go`：在 `ChannelInterfaceRunningHub*` 旁加 `ChannelInterfaceRunningHubV2Video = "runninghub-v2-video"`。
- `backend/internal/generation/registry.go`：加入官方声明式/工作流接口名单。
- `backend/internal/app/provider.go:185` 附近的分派：新增 `isRunningHubV2Interface` 判断，进入新执行器。

### B2. 新增 v2 执行器

新文件 `backend/internal/app/workflow_provider_v2.go`（或 `provider_runninghub_v2.go`），复用现有基础设施：

- 认证：v2 用 header `Authorization: Bearer <Key>`（v1 是 body `apiKey` 字段，不能混用），复用 `s.runningHubJSON` 需增加 header 模式或新写薄封装。
- 提交：`POST {root}/openapi/v2/{vendor}/{model}/{task}`，vendor/model/task 存在模型配置里（渠道模型名直接存端点路径，如 `kling-v3.0-pro/image-to-video`）。
- 轮询：`POST {root}/openapi/v2/query`，body `{"taskId"}`；`SUCCESS`/`FAILED`（带 errorCode/errorMessage）/其他=进行中；结果取 `results[].url`，`usage.consumeMoney` 可记账。
- **坑**：taskId 可能是数字或字符串，解析要双形态兼容（调研文档 §4）。
- 图生视频：图片直接传公网 URL 或 data URI；仅本地文件才需要 `POST /openapi/v2/media/upload/binary`（Bearer 认证，与 v1 上传接口不同）。`requiresPublicMediaUrls` 语义按此设计。
- 轮询循环、媒体水合、结果落存储复用 `provider_video_polling.go` / `resource_generated.go` 现成链路。

### B3. 前端接入

- 模型能力默认值：`backend/internal/app/model_capability_defaults.go` 加 runninghub-v2-video 档位默认。
- 渠道/模型配置：视频模型协议下拉来自 `GET /plugins/catalog`（`web/src/services/api/plugin-catalog.ts:35`），需要把 v2 接口类型暴露进目录（或在渠道预设里加快捷项）。
- 连接测试：`web/src/lib/model-connection-test.ts:64` 分发行。

### B4. 验证

```bash
cd backend && go test ./...
cd web && bun run build && bun test src/lib  # 涉及前端纯函数时
```

- 后端契约测试参考 `backend/internal/protocol/metaso_minimax_video_test.go` 的模式，为 v2 执行器补提交/轮询/双形态 taskId 用例。
- 浏览器验收：系统渠道（或个人渠道）建 runninghub-v2-video 模型 → 画布文生视频 + 图生视频各一次 → 确认任务记录、资源存储、账单流水。
- 文档同步：`docs/content/docs/backend/backend-database.mdx`（若有表字段变化）、`http-api.mdx`（若暴露新接口）、本文档补「已实施」标注。

---

## 排障速查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 提交报「企业版余额不足」 | 提交 Key 填成了企业级上传 Key | 换积分 API Key（`workflow_provider.go:193`） |
| 上传参考素材 401 ApiKey verification failed | 企业级 Key 无效或与站点不匹配 | 核对 Key 与 Base URL 同站（`workflow_provider.go:304`） |
| 缺少 workflowId 或 webappId | 条目只存了 workflowJson 没存 ID | 重新拉取参数 |
| 生成卡在排队 | RunningHub 侧排队或积分不足 | 查平台控制台；后端轮询 1 小时超时 |
| 字段映射改了不生效 | 生成时后端重新拉工作流 JSON 推断覆盖 | 显式保存字段映射，确认拉取参数后的映射被合并保留（`runninghub-settings-pane.tsx:136`） |
| 拉取参数失败但工作流能用 | 兼容网关没有管理接口 | 依赖已保存字段映射继续提交（`workflow_provider.go:115`） |
