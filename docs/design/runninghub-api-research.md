# RunningHub 开放 API 调研（接入评估）

目的：评估把 RunningHub（runninghub.cn / runninghub.ai）接入影策工作台后端（Go + Gin），复用「提交生成任务 → 轮询状态 → 拿到视频结果 URL」模式。

调研日期：2026-10-09。

## 0. 重要说明：信息来源与限制

- **官方文档站 `doc.runninghub.cn` 在本次调研网络环境下不可达**（本机代理 fake-ip + TLS 握手被重置；jina 代理侧同样报 DNS 解析失败；Wayback 无快照）。因此**未能直接引用文档站页面原文**。
- 本笔记的所有字段名、路径、枚举值均从以下**可运行代码与官方仓库**交叉提取（这些代码本身就是对官方 API 的封装，且相互印证）：
  - 官方 ComfyUI 插件 [HM-RunningHub/ComfyUI_RH_APICall](https://github.com/HM-RunningHub/ComfyUI_RH_APICall)（`RH_ExecuteNode.py`、`RH_ImageUploaderNode.py`）
  - 官方 CLI [HM-RunningHub/RH_CLI](https://github.com/HM-RunningHub/RH_CLI)（`src/rh_cli/http.py`、`poll.py`、`media.py`、`app/client.py`、`catalog/data/capabilities.json`）
  - 社区 Go SDK [difyz9/runninghub-sdk-go](https://github.com/difyz9/runninghub-sdk-go)（无 license，仅作字段对照，不建议直接依赖）
  - 官方产品页 `https://www.runninghub.cn/call-api`、`https://www.runninghub.cn/call-api/search-api/standard-model`
- 接入编码前，建议在能正常访问的网络下核对 `doc.runninghub.cn` 的最新页面（站点可能已迁移），以官方文档为准。

## 1. API 总体形态：三套并存

RunningHub 对外有三种调用面，同一套任务/轮询模型：

| 调用面 | 提交 endpoint | 说明 | 适合影策 |
| --- | --- | --- | --- |
| **v1 ComfyUI 工作流任务** | `POST /task/openapi/create` | 传平台上的 `workflowId` + `nodeInfoList` 节点参数替换；也可传完整 workflow JSON（`workflow` 字段） | 可选（自定义工作流） |
| **v1 AI App 任务** | `POST /task/openapi/ai-app/run` | 传 `webappId`（平台上的 AI 应用模板）+ `nodeInfoList` | 可选 |
| **v2 OpenAPI（推荐）** | `POST /openapi/v2/run/workflow/{workflowId}`、`/openapi/v2/run/ai-app/{webappId}`、以及**标准模型直调** `POST /openapi/v2/{vendor}/{model}/{task}` | v2 统一了提交与查询，且新增"标准模型 API"——**不需要任何 ComfyUI JSON**，直接按模型端点传 prompt/图片参数 | **主推** |

Base URL：`https://www.runninghub.cn`（国内主站）；v2 前缀 `https://www.runninghub.cn/openapi/v2`。国际站为 `www.runninghub.ai`（本次未单独验证其 endpoint 差异）。

来源：RH_CLI `src/rh_cli/http.py:11-13`（`API_HOST = "https://www.runninghub.cn"`、`BASE_URL = f"{API_HOST}/openapi/v2"`）；RH_CLI `src/rh_cli/app/client.py:16-18`；Go SDK `runninghub/client.go:69`（默认 base）、`runninghub/endpoints_comfy.go:13/41`（`/task/openapi/create`）、`runninghub/endpoints_v2.go:31/54`（`/openapi/v2/run/ai-app/{id}`、`/openapi/v2/run/workflow/{id}`）。

## 2. 认证

- **v2 OpenAPI**：HTTP header `Authorization: Bearer <API_KEY>`。
- **v1 legacy 接口**：无认证 header，API Key 放在**请求体字段 `apiKey`**（accountStatus 例外，字段名是小写 `apikey`）。

来源：RH_CLI `src/rh_cli/http.py:46`（`"Authorization": f"Bearer {self.api_key}"`）；官方插件 `RH_ExecuteNode.py:1868-1878`（`data = {"workflowId": ..., "apiKey": ..., "nodeInfoList": ...}`）；`RH_ExecuteNode.py:1805`（`data = {"apikey": api_key}`）；Go SDK `client.go:112-113`（Bearer）、`endpoints_account.go:6`（"request body uses `apikey` (lowercase), not `apiKey`"）。

文档站历史上有「Host header 固定为 `www.runninghub.cn`」的说法（Go SDK 预留了 `WithHostOverride`，`client.go:46-47`），本次未能从文档原文确认；直连 `www.runninghub.cn` 时无需处理。

## 3. v2 标准模型直调（不需要 ComfyUI JSON）

这是对影策最友好的形态。平台上每个"标准模型 API"是一个形如 `{vendor}/{model}/{task}` 的端点，请求体就是该模型的业务参数（prompt、图片 URL、时长等），提交后返回 `taskId`。

### 3.1 提交

```
POST https://www.runninghub.cn/openapi/v2/{vendor}/{model}/{task}
Authorization: Bearer <API_KEY>
Content-Type: application/json

{"prompt": "...", "imageUrl": "https://...", "duration": "5"}
```

- 图片参数既可传**公网 URL**，也可传 `data:` URI（RH_CLI 对小文件内联 base64，大文件走上传接口换取 URL）；**不能传本地路径**。
- 端点目录：官方 CLI 自带快照 `RH_CLI/src/rh_cli/catalog/data/capabilities.json`（356 个端点，其中视频 198 个），运行时也可用 `/openapi/v2/aiapp/list` 查 AI App 列表。视频类端点举例（参数 key 为实际字段名）：
  - `rhart-video-v3.1-pro-official/image-to-video`：`prompt, imageUrl, lastImageUrl, negativePrompt, seed, aspectRatio, resolution, duration, generateAudio`
  - `kling-v3.0-pro/image-to-video`：`prompt, negativePrompt, firstImageUrl, lastImageUrl, duration, cfgScale, sound, multiShot, shotType`
  - `minimax/hailuo-2.3/image-to-video-pro`：`prompt, enablePromptExpansion, imageUrl`
  - `vidu/image-to-video-q3-pro`：`prompt, imageUrl, duration, resolution, audio`
  - 平台页面上还有 `seedance2.0/图生视频`、`seedance2.0/文生视频`、`万相3.0-参考生视频`、`MiniMax-H3 多模态参考生视频` 等（`https://www.runninghub.cn/call-api/search-api/standard-model`）。

来源：RH_CLI `src/rh_cli/model/client.py:63-68`（`POST {BASE_URL}/{endpoint}`）、`src/rh_cli/model/payload.py`（参数按 capabilities.json 的 `key/type` 组装）、`src/rh_cli/media.py:44-52`（URL 直传 / data URI / 上传）；Go SDK `endpoints_standard_model.go:32-46`（RunStandardModel）。

### 3.2 轮询与结果

```
POST https://www.runninghub.cn/openapi/v2/query
Authorization: Bearer <API_KEY>
Content-Type: application/json

{"taskId": "<创建返回的 taskId>"}
```

响应（v2 统一任务对象，Go SDK `types.go:83-93`）：

```json
{
  "taskId": "2016796569449795585",
  "status": "SUCCESS",
  "errorCode": "",
  "errorMessage": "",
  "results": [
    {"url": "https://.../result.mp4", "nodeId": "123", "outputType": "mp4"}
  ],
  "promptTips": "",
  "usage": {"consumeMoney": "0.45", "taskCostTime": "31"}
}
```

- 状态枚举（v2，RH_CLI `poll.py:50-64` 判定逻辑）：`SUCCESS` / `FAILED`（附 `errorCode`、`errorMessage`）/ 其他值视为进行中（CLI 每 5s 轮询一次，最长 1200s）。`results[].url` 是**可直接下载的结果文件 URL**（视频为 mp4；CLI 用 `outputType` 推断扩展名）。
- `usage.consumeMoney` 为本次消耗金额，可用于记账对账（RH_CLI `model/client.py:30-34`）。
- FAILED 时错误信息里可能含 `balance/insufficient/余额`（CLI 据此识别余额不足）。

来源：RH_CLI `src/rh_cli/poll.py:11`（`POLL_URL = f"{BASE_URL}/query"`）、`poll.py:50-64`；Go SDK `endpoints_v2.go:94`（`QueryTaskV2 → POST /openapi/v2/query`）、`types.go:76-93`。

### 3.3 v2 提交响应

提交接口的响应体同样是上述任务对象（Go SDK 将 RunWorkflow/RunAIApp/RunStandardModel 的响应统一建模为 `QueryV2Response`，`endpoints_standard_model.go:39`）；至少包含 `taskId`（也可能为字符串或数字两种形态——Go SDK 为此做了双格式兼容，`types.go:159-196`，影策解析时同样要兼容）。

### 3.4 其他 v2 接口

| 接口 | 方法与路径 | 说明 |
| --- | --- | --- |
| 计费预览 | `POST /openapi/v2/price-preview/{vendor}/{model}/{task}`（请求体同对应模型） | 提交前查价 |
| 文件上传 | `POST /openapi/v2/media/upload/binary`（multipart，form 字段名 `file`，Bearer 认证） | 返回 `data.download_url` |
| AI App 列表 | `POST /openapi/v2/aiapp/list` | 平台 AI 应用目录 |
| 队列状态 | `GET /openapi/v2/queue/status` | 并发上限/排队数 |
| API Key 管理 | `GET /openapi/v2/api-key/list` | 子 key、配额、过期 |
| 公共资源 | `POST /openapi/v2/...`（LoRA/checkpoint 等） | 供工作流引用 |

来源：Go SDK `endpoints_upload.go:12-14`、`endpoints_v2.go:57-94`、`endpoints_standard_model.go:88-109`；RH_CLI `media.py:11,26-42`。

## 4. v1 ComfyUI 工作流任务（legacy，仍可用）

### 4.1 提交

```
POST https://www.runninghub.cn/task/openapi/create
Content-Type: application/json

{
  "apiKey": "<API_KEY>",
  "workflowId": "<平台工作流 ID>",
  "nodeInfoList": [
    {"nodeId": "60", "fieldName": "image", "fieldValue": "<上传返回的 fileName 或 URL>"},
    {"nodeId": "63", "fieldName": "text",  "fieldValue": "prompt 文本"}
  ],
  "instanceType": "plus",
  "webhookUrl": "https://your.api/callback"
}
```

- `workflowId` 是用户在 RunningHub 平台上保存/托管的工作流 ID（平台页面"API"入口可获取），**不是**本地 workflow JSON 文件名；也可用 `workflow` 字段直接传完整 ComfyUI API JSON（Go SDK `types.go:144` 有该字段）。
- `nodeInfoList` 用来替换工作流中目标节点的输入；`instanceType` 可选更高配机型（官方插件示例 `"plus"`，即 RTX 4090 48G，`RH_ExecuteNode.py:1885-1887`）。
- 响应：`{ "code": 0, "msg": "success", "data": { "taskId": ..., "taskStatus": "QUEUED", "netWssUrl": "...", "clientId": "...", "promptTips": "..." } }`。

### 4.2 状态与结果

- v1 没有单独的"状态"轻量接口可直接用：官方插件用 `POST /task/openapi/outputs` 同时承担状态与结果查询（`RH_ExecuteNode.py:2047`）：
  - 请求 `{"apiKey": ..., "taskId": ...}`。
  - 排队中 → `code != 0, msg = "APIKEY_TASK_IS_QUEUED"`；运行中 → `msg = "APIKEY_TASK_IS_RUNNING"`（`data.netWssUrl` 可选提供实时进度 WebSocket）；完成 → `code = 0` 且 `data` 为数组，每项含 `fileUrl`、`fileType`、`nodeId`、`consumeMoney`、`consumeCoins`、`taskCostTime`；失败 → `data.failedReason{exceptionType, node_name, exception_message, ...}`（Go SDK `types.go:208-234`）。
  - 即 v1 的**状态语义编码在 `msg` 字段里**，且 **HTTP 200 不代表任务成功**，必须判 `code` + `msg` + `data` 形态——与影策 `http.ts` 信封约定不同，接入时需要独立适配层。
- 文档还提供 `POST /task/openapi/status`（被 Go SDK 标注 deprecated）与 `POST /task/openapi/outputs` 并存；`POST /task/openapi/cancel` 取消任务；`POST /api/openapi/getJsonApiFormat` 把平台工作流转成 ComfyUI API JSON（Go SDK `endpoints_comfy.go:9/58/71/88/129`）。
- **结果 URL**：`outputs` 返回的 `fileUrl` 直接可下载，无需再签名。

### 4.3 v1 文件上传（图生视频的参考图）

```
POST https://www.runninghub.cn/task/openapi/upload
Content-Type: multipart/form-data

apiKey=<API_KEY>&fileType=image     # 表单字段
file=<二进制>                        # 文件字段
```

响应：`{ "code": 0, "msg": "success", "data": { "fileName": "<文件名>" } }`；把 `fileName` 填进 `nodeInfoList` 对应节点的 `fieldValue` 即完成"上传参考图"。`fileType` 支持 image/video/audio 等输入类型（官方插件 `RH_ImageUploaderNode.py:111-127`；RH_CLI `media.py:12,55-70`，CLI 里 `fileType: "input"`）。

### 4.4 余额（v1）

```
POST https://www.runninghub.cn/uc/openapi/accountStatus
Content-Type: application/json

{"apikey": "<API_KEY>"}
```

响应 `data`：`{ "remainCoins": "...", "currentTaskCounts": "...", "remainMoney": ..., "currency": ..., "apiType": ... }`（注意字段名是小写 `apikey`；官方插件 `RH_ExecuteNode.py:1805`；Go SDK `endpoints_account.go:5-13`、`types.go:97-103`）。

## 5. 文/图生视频官方模板（不拼 ComfyUI JSON）——有，两条路

1. **标准模型 API（推荐）**：平台上「AI API → 标准模型」目录按 `文生视频 / 图生视频 / 参考生视频` 分类提供 300+ 官方维护的模型端点（产品页标注视频 322 个模型），直接 `POST /openapi/v2/{endpoint}`，请求体即业务参数。官方 CLI 的 `capabilities.json` 就是该目录的机器可读快照（`--task "图生视频"` 可自动选端点）。
2. **工作流托管 / AI App**：用户在平台编辑器里把任意 ComfyUI 工作流（或打包成 AI App/WebApp）保存后得到 `workflowId` / `webappId`，API 侧用 `nodeInfoList` 替换节点参数即可调用，不需要自己拼 JSON。AI App 的可替换节点清单可通过 `GET /api/webapp/apiCallDemo?apiKey=...&webappId=...` 获取（返回 `nodeInfoList` 与可复制的 curl 示例，RH_CLI `app/client.py:42-80`；Go SDK `endpoints_ai_app.go:32-39`）。

对应接口名：v2 为 `run/workflow`、`run/ai-app`、标准模型直调；v1 为 `task/openapi/create`（工作流）、`task/openapi/ai-app/run`（AI App）。

## 6. Go SDK 现状

- **无官方 Go SDK**。GitHub 官方组织 `HM-RunningHub` 只维护 Python（`ComfyUI_RH_APICall`、`ComfyUI_RH_OpenAPI`、`RH_CLI`，均 Apache-2.0 / MIT）。
- 社区 Go SDK：
  - [difyz9/runninghub-sdk-go](https://github.com/difyz9/runninghub-sdk-go)：覆盖最全（v1+v2、上传、等待、webhook），**无 LICENSE 文件，不可默认安全引用**；本次仅用作字段对照。
  - [Friday-fighting/runninghub_tools](https://github.com/Friday-fighting/runninghub_tools)、[housirvip/runninghub-manager](https://github.com/housirvip/runninghub-manager)：低星、无 license。
- 结论：影策后端按第 3/4 节的路径**自写一个薄 Go client**（参考 AGENTS.md 既有 provider 分层）比引入无 license 依赖更稳妥。

## 7. 真实请求/响应示例（代码级实证）

以下不是文档站原文，而是官方代码中构造/消费的真实结构（引用处见前文）。

**v2 提交（图生视频，`POST /openapi/v2/{model}/image-to-video`）**

```json
{"prompt": "镜头缓缓推进", "imageUrl": "https://cdn.example/first.png", "duration": "5"}
```

**v2 提交响应 / 轮询响应（`POST /openapi/v2/query`）**

```json
{"code": 0, "msg": "success", "data": {
  "taskId": "2016796569449795585", "status": "RUNNING", "errorCode": "", "errorMessage": "",
  "results": [], "promptTips": ""
}}
```

完成后 `status: "SUCCESS"`、`results: [{"url": "https://...mp4", "outputType": "mp4"}]、`usage.consumeMoney` 有值。注意：外层信封 `code/msg/data` 的存在与否在不同端点间可能有差异（RH_CLI 直接解顶层字段并兼容 `taskId/task_id`、`data.taskId` 两种位置，`model/client.py:23-27`），**影策适配层需按 `code==0` + 宽松解析 `data` 编写**。

**v1 创建（`POST /task/openapi/create`）与轮询（`POST /task/openapi/outputs`）**：见 4.1 / 4.2。

## 8. 对影策接入的建议（供决策）

1. **优先接 v2 标准模型 API**：与现有 `provider_video.go` 的"提交→轮询→URL 结果"模式同构；`/openapi/v2/query` 单一查询端点、状态枚举简单、结果直出 URL。
2. 认证统一走 `Authorization: Bearer`，API Key 存用户浏览器本地（与现有用户自带 Key 的安全约定一致，AGENTS.md §5），通过任务创建请求提交到自部署后端中转，不落日志。
3. 参考图（图生视频）：本地文件先 `POST /openapi/v2/media/upload/binary` 换 `download_url` 再进任务参数；已有公网 URL / data URI 可直接传。
4. 建议在 `internal/provider` 下新增 `provider_runninghub.go`（协议组包 + 解包），不改通用 `request.ts`/HTTP 中转面；错误处理要区分"提交失败（code!=0）"与"任务失败（status=FAILED, errorCode/errorMessage）"，并把 `usage.consumeMoney` 记入任务记录以便对账。
5. 轮询间隔参考官方 CLI（5s，超时 1200s）；如需实时进度，v1 提供 `netWssUrl` WebSocket（排队/运行中），v2 未确认等价物——接入时如需进度条再评估。
6. 落地前必须复核：`doc.runninghub.cn` 最新文档（本次网络不可达）、国际站 `.ai` 与 `.cn` 的 Key/额度是否互通、`taskId` 数字/字符串双形态、外层信封是否所有端点一致。
