# Fisher-H3 后端实施证据

工作目录：隔离 worktree；未提交，未修改任务状态。只使用模拟上游和独立测试数据库，没有真实 Key 或付费请求。

## 实际 RED

首次命令：`cd backend && go test ./internal/app ./internal/handler -run TestFisher -count=1`。

测试成功编译后因缺失行为失败：

- `TestFisherCapabilityEnvelope`：实际仍是 9 图片、0 视频/音频、1–15 秒和通用分辨率。
- `TestFisherRequestStableRolesAndOptions`：驱动仍为 reference_audio，视频偏移与音频默认值未映射。
- `TestFisherInvalidInputStopsBeforeCreate`：13 个非法输入都到达 BuildCreate（calls=1），包括模型、文本、时长、分辨率、失效 ID、参考数量、adaptive、偏移和音频模式。
- `TestFisherHeaderTaskContinuesPolling`：503、响应读取提前关闭、无效 JSON 三种响应都 POST=1、GET=0，丢失了 X-Task-Id。
- `TestFisherSystemProxyAuthorization`：创建与查询路径未授权。
- `TestFisherRelayPreservesTaskHeaderAndRedactsSecret`：响应保留了正文脱敏，但丢失任务响应头。

第二个针对性 RED：`go test ./internal/handler -run TestFisherSystemRejectsInvalidDuration -count=1`。60 秒无驱动的原始系统中转请求通过授权，测试失败。

第三个针对性 RED：`go test ./internal/app -run TestFisherMissingPluginNeverUsesLegacyCreate -count=1`。显式空注册表时走进遗留视频路径，返回“外部服务地址无效”，而不是插件缺失。

## 最终 GREEN

最终命令：

```text
cd backend
go test ./internal/app ./internal/handler ./internal/generation -run 'TestFisher|TestProviderRequest|TestPostJSON|TestAuthorizeSystemProxy|TestCustomRelay|TestBillingFailure|TestBeginTaskRoute|TestDeclarativeTextStreaming' -count=1
```

结果：app、handler 通过；generation 包通过编译（该正则在此包没有测试），插件缺失回归由 app 测试实际覆盖。`git diff --check -- backend` 通过。

后续增加的专项检查覆盖真实 Fisher 插件的后端转换→BuildCreate 映射、合法 11 图片/4 音频/60 秒混合、native 与驱动默认值、显式 0/false、ProviderRequestID 落库及数据库失败时停止、无 ID/只有 request_id 时不轮询且不重建、原始中转协议标识校验、危险 ID 和追踪编号拒绝。三个响应头恢复场景最终使用实际 Fisher 插件执行，均 POST=1、GET=1，并返回模拟任务终态错误。

新增原始中转测试最初使用真实域名作为目标，环境解析触发既有保留地址防护，未到达参数检查；改为明确允许的 127.0.0.1 模拟目标后通过。测试不会连接该目标；非法参数在连接前失败。没有放宽生产出站防护。

## 任务贡献与文件

- `dba57f72-d5ff-4e6e-9ef3-e33879178c6d`：`model_capability_defaults.go` 同步 Fisher 总容量、10000 字符、分辨率、比例和 4–60 秒包络。
- `02117ce2-6692-46cc-9fdc-07e68f3d6291`、`5bd4c7be-cd3c-4f85-b013-5a58349b0926`、`bc057669-f32b-4943-b2e4-7f2fcfe63c03`：`provider_fisher.go`、`provider_protocol_request.go` 按实际媒体 ID 恢复首尾帧、驱动、偏移和控制参数；Fisher 前置校验独立计数及已知素材时长。
- `7e3648c5-3671-4009-a27c-b521a37f7134`：`provider_protocol.go`、`provider_http_client.go`、`task_billing.go` 复用已有任务编号、路由不确定状态与账务核对；有效头在读正文前持久保存，失败不隐瞒；Fisher 不把 request_id 作为任务编号。
- 同一任务的中转贡献：`handler/auth.go`、`handler/custom_proxy.go`、`handler/security.go` 支持明确 Fisher 协议的创建/查询、原始入参检查、响应头保存和密钥脱敏。`service/aliases_funcs.go` 只添加应用层函数的导出别名。
- 经主代理授权补充 `generation/registry.go` 的 Fisher 声明式提供商身份，缺失插件时阻止遗留协议回退。
- 检查文件：`app/provider_fisher_test.go`、`handler/fisher_proxy_test.go`。

## 交接与限制

- 经主代理后续授权补充 `backend/cmd/server/main.go` CORS，允许 X-Canvas-Upstream-Protocol 并向浏览器暴露 X-Task-Id；对应真实中间件检查已通过。
- 已知参考视频时长在提交前校验；未知时长保留上游检查，不猜测。
- 普通参考音频没有凭空增加 15 秒上限；只限制独立驱动音频已知时长不超过 60 秒。
- 真实上游生成、输出媒体及账户计费需要用户填入 Key 后验收。

## 独立审查后的中转闭环修复

发现：系统中转创建返回 HTTP 200，但正文没有有效 task_id（只有 request_id 或无效 JSON），同时有有效 X-Task-Id。前端 transport 的成功分支不提供响应头给 provider，导致不能进入已有错误响应恢复流程。

实际 RED 命令：`go test ./internal/handler ./cmd/server -run 'TestFisherSystemMissingBodyTaskIDReturnsRecoverableError|TestFisherCORSCarriesProtocolAndTaskID' -count=1`。

- 完整 `proxySystemRequestPath` + 独立 SQLite/service + 模拟上游：两个场景均 POST=1、HTTP 200、X-Task-Id=existing-fisher-task，预期可恢复错误的检查失败。
- 真实 CORS 中间件 OPTIONS：缺少请求协议标识允许头和任务编号暴露头，检查失败。

修复：Fisher 创建成功状态下没有有效正文 task_id 时返回结构化 502，保留已过滤的 X-Task-Id，并把费用保留为待核对；前端可进入已有 Axios 错误头恢复后查询旧任务。其他协议响应不受此条件影响。新增 CORS 合同仍遵守既有来源检查。

最后 GREEN 命令：

```text
go test ./internal/app ./internal/handler ./internal/generation ./cmd/server -run 'TestFisher|TestProviderRequest|TestPostJSON|TestAuthorizeSystemProxy|TestCustomRelay|TestBillingFailure|TestBeginTaskRoute|TestDeclarativeTextStreaming|TestAllowedOrigin|TestParseCORSPolicy' -count=1
```

结果：app、handler、cmd/server 全部通过；generation 包通过编译，插件缺失测试在 app 实际运行。`git diff --check -- backend` 通过。完整系统中转测试只为隔离 HTTP 响应合同在测试数据库禁用积分，没有写入项目账号配置或生产数据库。

## 最终集成审查返回 Build 后的有界修复

自定义中转也存在有效 JSON 缺少有效 task_id 的 HTTP 2xx 成功分支，导致前端不能消费成功响应头。新增 `TestFisherCustomMissingBodyTaskIDReturnsRecoverableError`，实际调用 custom handler 和 TLS 模拟上游。

实际 RED：`go test ./internal/handler -run TestFisherCustomMissingBodyTaskIDReturnsRecoverableError -count=1`。仅 request_id、有危险 task_id、任务头含 Key 时均 POST=1、返回 200；无头的 accepted 响应 POST=1、返回 202。合法任务编号和非 Fisher 协议控制用例原本通过。

修复仅限定明确的 Fisher 协议 POST 创建：复用系统中转的 task_id 检查，在正文脱敏后将缺少有效编号的成功 JSON 转为结构化 502，保留已过滤的 X-Task-Id。无有效任务头时同样返回不确定失败，不产生第二次 POST。解析实际 URL 路径识别创建，继续保留现有出站安全校验、Key 过滤和其他协议的响应行为。

同轮非阻塞边界补充：已有非法输入表增加 negative_driver、negative_video。实际 RED 命令 `go test ./internal/app -run '^TestFisherInvalidInputStopsBeforeCreate/(negative_driver|negative_video)$' -count=1` 中，两者都到达 BuildCreate（calls=1）。最小修复拒绝已知负驱动时长，参考视频非零时长均执行 2–15 秒检查；0/缺省仍表示未知。合法混合输入检查增加缺省时长视频，证明未知时长仍接受。

最终 GREEN：`go test ./internal/app ./internal/handler -run 'TestFisher|TestCustomRelay|TestAuthorizeCustomRelay|TestSystemProxy' -count=1`，app、handler 均通过。custom 响应检查的六种场景全部通过，分别确认只有一次上游 POST、有效恢复头保留、Key 头过滤、无头不伪成功、合法编号成功以及非 Fisher 语义保持。

同一恢复问题的 SSE 补查：成功的 text/event-stream 响应会在正文 task_id 检查前返回 200。真实 custom handler 检查增加 Fisher SSE 有头、无头及非 Fisher SSE 对照。

实际 RED：`go test ./internal/handler -run 'TestFisherCustomMissingBodyTaskIDReturnsRecoverableError/(fisher_sse|other_protocol_sse)' -count=1`。两个 Fisher 场景均 POST=1、返回 200/SSE；有头场景保留 existing-fisher-task，但无法触发前端的错误头恢复。非 Fisher SSE 对照通过。

最小修复在流式响应发送前拒绝明确 Fisher 创建的非 JSON 成功响应，返回结构化 502；已有安全任务头保留，无任务头也不伪成功，不重建付费任务。其他协议 SSE 仍原样成功返回。

SSE 修复 GREEN：`go test ./internal/handler -run 'TestFisher|TestCustomRelay|TestAuthorizeCustomRelay|TestSystemProxy' -count=1`，handler 通过（4.430 秒），含九种真实 custom 响应场景。`git diff --check -- backend/internal/handler/custom_proxy.go backend/internal/handler/fisher_proxy_test.go docs/openspec/changes/fisher-h3-video/.comet/backend-evidence.md` 通过。
