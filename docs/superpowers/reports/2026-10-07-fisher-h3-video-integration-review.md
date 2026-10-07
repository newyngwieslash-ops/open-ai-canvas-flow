# Fisher-H3 最终独立集成审查

最终结论：**PASS**。首轮 1 项 IMPORTANT 及 1 项 SUGGESTION 已经同一次有界修复、独立补查关闭；没有剩余 IMPORTANT/CRITICAL。真实 Key 和浏览器验收限制仍保留。

## 审查边界与输入

- 初始受审提交：`a582c531b00dafa9f8e28619888ca7df064d3c15`；基线：`639c53e3d9970a109e6e1542572fc9c022642548`；分支：`codex/fisher-h3-video`。
- 独立审查者未参与产品实现，未修改产品代码、测试、Comet 状态或任务勾选，未提交 Git、启动开发服务或调用付费上游。本文件为独立审查产物。
- 阅读实际提交差异、`tasks.md`、正式设计与 Fisher 行为合同、四份实施 evidence，以及 `root-task-review.md` / `plugin-task-review.md` 的原发现和修复结论。重点审查正确性、安全、边界和跨层调用；主会话另负责规格场景、设计覆盖及正式 Runtime 验证。
- 仓库没有 `.codegraph/`，使用源码和调用方搜索。检查范围包含插件产物与注册、渠道预设、真实画布及创作页输入、metadata、前后端请求构建、系统/个人中转、任务持久化、轮询和资源保存。

## IMPORTANT：个人中转丢失 HTTP 200 缺正文编号时的任务头恢复

初始位置：`backend/internal/handler/custom_proxy.go:174–180`；关联 `web/src/services/api/channel-transport.ts:54`、`web/src/services/api/video-provider-fisher.ts:23–32`。

上游 Fisher 创建返回 HTTP 200、有效 JSON `{"request_id":"trace-only"}`（或 `{}`）并附 `X-Task-Id: accepted-one` 时，个人中转验证 JSON 合法后原样返回 200。虽然中转已复制有效任务头，但共享 transport 仅返回 `response.data`。Fisher provider 检测缺少正文 `task_id` 后抛普通 Error，该错误没有 Axios 响应头，因此无法恢复并查询已受理任务。系统中转同类情况已转换结构化 502，个人中转仍遗漏。

独立复现使用真实 `createVideoTransport`、`createFisherH3VideoTask` 及 `videoResponseTools`，仅将 Axios 请求替换为无网络响应桩：

```json
{"status":200,"headers":{"x-task-id":"accepted-one"},"data":{"request_id":"trace-only"}}
```

实际结果：

```json
{"error":"Fisher-H3 未返回 task_id，创建结果待核对，请勿直接重发","posts":1,"gets":0}
```

该结果证明编号没有到达恢复流程；未发生第二次 POST。现有专项只覆盖个人中转 503 头恢复和系统中转 HTTP 200 缺编号，没有覆盖个人中转的这个成功状态分支。

建议：对明确 Fisher 协议的创建响应，在成功状态正文缺少有效 `task_id` 时复用系统中转的结构化非 2xx 恢复方案，保留过滤后的任务头；补真实个人中转检查，覆盖 `{}`、只有 `request_id` 和危险任务 ID，并确认只 POST 一次。不得通过追踪 ID 或自动重建解决。

## SUGGESTION：后端非法负媒体时长与前端校验不一致

位置：`backend/internal/app/provider_fisher.go:197`、`:238`。驱动只检查已知时长大于 60000；视频只对正时长校验。负 `durationMs` 因此不会被这两个守卫拒绝，而前端 Fisher helper 已明确拒绝负值。建议后端在保留未知 0/缺省合同的同时，拒绝已提供的非法负时长。当前正常 UI 无法产生该值，本轮没有证明实际 API 请求能绕过其余通用校验，故不列为 IMPORTANT。

## 其余已核查结论

- 提供商、模型和 Base URL 独立；真实安装包与生成 manifest 一致性有专项检查，缺插件拒绝遗留协议回退。预设不含 Key；直接请求与工作台使用既有配置就绪检查及登录态后端路径。
- 480P / 768P / 1080P 不映射 2K；整数时长、固定/自适应比例、提示词、各角色独立数量和 ID 关联在创建前校验。真实 canvas / creation 提交携带首尾帧、驱动和按媒体 ID 保存的视频偏移。
- 音频默认值按驱动存在性解析；嵌套默认值合并及显式 0/false 修复已经落到共享 metadata 路径。前端已拒绝非有限或负驱动时长。
- 后台从有效响应头取得编号后先持久化，再查询；持久化失败进入不确定状态，缺编号不以 `request_id` 替代。已保存编号与未知提交状态使用现有路由保护，未发现新的自动重建分支。
- 系统中转对 HTTP 200 缺有效正文编号返回可恢复错误；CORS 允许协议身份头并暴露任务头，明确来源策略没有扩大。任务编号过滤阻止路径注入、控制字符、过长值及当前密钥反射。
- 查询读取 `task.content.url`，failed/cancelled 终止，`error.code=0` 不覆盖成功；下载及资源持久化继续使用现有宿主流程。
- UI 复用真实共享控件，具有可见标签、原生键盘控件和画布事件边界；条件时长遵守渠道范围交集。新建、切换、删除会话后的草稿清空已同步清空 Fisher 状态，历史恢复保持原参数；视频删除后的偏移恢复入口仍可见。
- 前次工作包 IMPORTANT 均可在最终代码定位到修复；本轮发现为另一条真实中转分支，不重复采纳已关闭问题。

## 验证与限制

- 本审查独立执行上述真实 transport/provider 的无网络复现；初始提交范围 `git diff --check` 通过。
- 主会话交接已有最终 43 项 Bun、五个 Go 包专项及前端 build 通过证据；工作包 evidence 记录对应 RED/GREEN 和有界复查。本审查引用这些证据，不将它们描述为审查者重新执行；正式 Runtime 验证仍由主会话记录。
- 没有真实 Key，没有浏览器验收，没有实际生成、媒体可访问性或计费效果证据。SSR 与源码审查只证明静态控件/状态逻辑；这些运行缺口已在产品 `pending-test.mdx` 留存。

## 后续有界复查

首轮没有其它已证实阻断项。原实现者已修复明确 Fisher 创建的成功 JSON 缺编号分支，并增加六个真实个人中转场景；负媒体时长建议也已补守卫及 RED/GREEN。

补查同一恢复问题时确认，`writeCustomRelayResponse` 的成功 `text/event-stream` 分支先于新 JSON 编号守卫发送 200。有效 `X-Task-Id` 随成功响应丢在 transport 边界，仍无法恢复。独立以真实 transport/provider、HTTP 200、SSE 字符串及有效任务头复现，结果仍是“未返回 task_id”，POST=1、GET=0。此项属于原 IMPORTANT 尚未覆盖的响应分支，已交原实现者在当前有界修复中处理；没有展开另一轮整体验证。

### 有界补查结论：PASS

补查针对初始提交上四个产品/检查文件的修复差异：`backend/internal/handler/custom_proxy.go`、`fisher_proxy_test.go`、`backend/internal/app/provider_fisher.go`、`provider_fisher_test.go`。补查时 HEAD 仍为初始实现提交，修复在工作树内；后续提交由主会话负责。本审查未改变上述文件。

明确 Fisher POST 创建的成功非 JSON 响应现于流式分支发送头之前返回结构化 502。JSON 响应正文脱敏后检查有效 `task_id`，无编号及危险编号同样返回 502。此前过滤的 `X-Task-Id` 均保留；无有效头时不会伪成功或重建。合法 Fisher 编号、非 Fisher JSON 和非 Fisher SSE 仍保持成功响应。

独立 Go 补查命令：

```text
go test ./internal/app ./internal/handler -run 'TestFisherCustomMissingBodyTaskIDReturnsRecoverableError|TestFisherInvalidInputStopsBeforeCreate|TestFisherLegalMixedCapacityAndControlDefaults|TestFisherRelayPreservesTaskHeaderAndRedactsSecret|TestCustomRelaySuccessJSON|TestCustomRelayStream' -count=1
```

最终实际结果：app `ok 2.707s`、handler `ok 3.110s`，退出 0。个人中转九场景通过，包括 202 无编号、HTTP 200 追踪编号/危险正文编号、Key 任务头过滤、合法编号、非 Fisher JSON、Fisher SSE 有头/无头及非 Fisher SSE；每个场景仅一次上游 POST。相邻真实流式中转检查也运行通过。驱动/视频负时长在 BuildCreate 之前拒绝；合法混合输入的 0/缺省未知时长仍接受。此前该命令运行在修复尚未完成的 SSE RED 阶段，两个真实 SSE 场景实际失败（HTTP 200、POST=1），没有把它写成通过。

独立前端闭环使用真实 `createVideoTransport`、`createFisherH3VideoTask`、`pollFisherH3VideoTask`，Axios 传输桩改为抛带 `AxiosHeaders` 任务头的结构化 502；仅输出媒体下载使用无网络响应桩。实际结果：

```json
{"task":{"id":"accepted-one","provider":"fisher-h3","model":"MiniMax-H3"},"state":{"status":"completed","result":{"url":"https://media.example.com/result.mp4"}},"posts":1,"gets":1,"pollRequest":"https://api.work-fisher.com/v2/query/video_generation/accepted-one"}
```

成功轮询响应含 `error.code=0`。该闭环证明新的非 2xx 恢复语义到达实际 transport/provider，查询使用已有编号，未二次创建。第一次前端补查因审查脚本读取了大写请求头键而断言报错，实际 transport 会规范为小写；修正审查脚本后上述断言全部通过，未修改产品实现。

最终 `git diff --check` 退出 0。原 IMPORTANT 的 JSON/SSE 两个响应分支及负时长建议均关闭；此次补查没有扩大到另一轮整体验证。
