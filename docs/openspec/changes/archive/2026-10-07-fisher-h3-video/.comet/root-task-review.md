# 独立工作包审查：前端协议与中转身份

范围：task `a6ae1330-0a08-41b7-b286-05dba38ec546` 及 task `7e3648c5-3671-4009-a27c-b521a37f7134` 的前端中转身份贡献。审查当前真实源代码、规格、正式设计、diff、TDD 记录与专项测试；未审查审查者自己实现的插件；未修改产品代码或 Comet 状态。

## IMPORTANT：成功响应中的零错误码被误判失败

位置：`web/src/services/api/video-provider-fisher.ts:38`。

当前 `if (state.error || ...)` 根据错误对象存在性判断终态失败。MiniMax V2 成功任务可能保留 `error: {code: 0}`；该对象不能覆盖明确的 `status: succeeded` 和正常 URL。当前代码返回失败并丢失已完成的视频。相邻官方 MiniMax 合同也已有成功任务携带零错误码的例子。

最小复现：调用 `pollFisherH3VideoTask`，transport GET 返回：

```json
{"task":{"status":"succeeded","error":{"code":0},"content":{"url":"https://example.com/result.mp4"}}}
```

当前实际输出：`{status: "failed", error: "Fisher-H3 视频生成失败"}`，没有进入 `videoResultFromUrl`。

建议：依据失败/取消状态或真实非零错误码/错误消息判断；空对象或只有数值/字符串零错误码不能失败。补零错误码成功及真实错误仍失败的专项检查。

## IMPORTANT：局部音频覆盖丢弃已配置默认值

位置：`web/src/services/api/video.ts:49`。

Fisher option namespace 使用浅合并，调用方只覆盖嵌套音频控制的一个字段时，整个渠道 `defaultOptions.audio_control` 被替换，剩余字段重新套用服务默认。这会改变用户明确配置的音频模式与去噪值，尤其可能把 native 改为 lock_source。

最小复现：

1. Fisher 渠道的模型默认值为 `defaultOptions={audio_control:{mode:"native",denoise_strength:0.2}}`。
2. 提交一条驱动音频，RequestOptions 只设置 `fisherH3={driveAudioId:"driver",audio_control:{add_drive_as_reference:false}}`。
3. 当前真实 dispatcher 的模拟 POST 输出为 `{mode:"lock_source",denoise_strength:0,add_drive_as_reference:false}`。

期望保留 `{mode:"native",denoise_strength:0.2,add_drive_as_reference:false}`。建议 Fisher 的嵌套控制字段逐字段合并，保留显式 0/false；补走 `createVideoGenerationTask` 的模型默认值加局部覆盖检查。此次没有要求改造其他提供商的默认参数语义。

## SUGGESTION：已提供的非有限媒体时长未被明确拒绝

位置：`web/src/lib/fisher-h3-video.ts:77`、`:93`。

驱动时长仅判断 `>60000`，参考视频时长使用 truthy 守卫，因而 `durationMs: NaN` 两处都会跳过边界检查。当前纯 helper 可实际接受 NaN 驱动及 60 秒输出。建议区分缺少/约定未知时长与已提供的非法数字，对后者显式校验有限值。该问题在 JSON 任务链中还受后端边界约束，因此列为建议。

## 现有验证

执行：

```powershell
bun test test/fisher-h3-video.test.ts test/fisher-h3-api.test.ts test/fisher-h3-relay.test.ts test/video-api-contracts.test.ts
```

实际结果：20 pass、0 fail、71 assertions。上述两个 IMPORTANT 场景另用 Bun 内联调用当前函数和 dispatcher 复现；仅传输桩，无网络或付费请求。

既有测试覆盖 Fisher 请求映射、独立额度、普通与驱动时长、尾帧、偏移、0/false、task_id、错误 X-Task-Id 恢复且单次 POST、中转身份、相邻官方 MiniMax/Seedance 合同；未发现新的重复 POST、已创建 task ID 反射密钥或独立协议身份回退问题。

结论：需要修复两个 IMPORTANT 后进行一次针对性复查。本文件属于工作包审查，不替代最终集成验收。

## 一次针对性复查：根协议工作包 PASS

两个 IMPORTANT 均已解决：轮询以实际错误码/消息判断，`code:0` 的成功任务正常下载；直接 dispatcher 通过 `generationMetadata` 的 Fisher 专属嵌套字段合并保留已配置音频默认值。

执行 `bun test test/fisher-h3-api.test.ts test/fisher-h3-video.test.ts test/fisher-h3-defaults.test.ts`：12 pass、0 fail、45 assertions。另重复最初真实 dispatcher 复现，模拟 POST 实际输出为 `{mode:"native",denoise_strength:0.2,add_drive_as_reference:false}`，符合原配置。未进行网络或付费请求。

原 SUGGESTION 中的视频非有限时长守卫也已修正；驱动 NaN 时长仍未显式拒绝，作为非阻塞建议保留。复查检查了 `AxiosHeaders` 类判断修正，不改变响应头恢复语义。此复查限于原发现与对应改动，不是最终集成审查。

## 后端工作包首次独立审查

范围：task `7e3648c5-3671-4009-a27c-b521a37f7134`、后端对能力/角色/音频/偏移任务的贡献；包含 app 请求转换、Fisher 边界校验、HTTP 响应与持久化、账务不确定分支、handler 系统/自定义中转、service 导出别名和 generation 的缺插件保护。不含审查者自己的插件源码。

### IMPORTANT：系统中转 HTTP200 的头编号恢复未到达客户端

位置：`backend/internal/handler/auth.go:489`、`:510`（首次审查快照）。

系统 Fisher 创建响应正文无 `task_id`，但带有效 `X-Task-Id` 时，系统中转仅保存该头并识别日志/账务中的编号，最后仍按上游 HTTP200 原样返回。正文可能无效 JSON，也可能是只含错误/追踪编号的有效 JSON。前端 channel transport 只返回 response.data；Fisher provider 在发现缺少正文 task_id 时抛普通 Error，该 Error 不携带 Axios response headers，已取得的任务编号因此无法恢复和查询。

复现输入：上游响应 `HTTP200`、`X-Task-Id: accepted-one`、正文 `broken` 或 `{}`。沿 auth 的最后 `c.Data(200, ..., body)` 及现有 transport/provider 数据流，客户端缺少 task_id 后报错且无法保留 accepted-one。自定义中转的无效 JSON 分支返回 502，因此不能只用 custom relay 测试替代此系统路径。

建议：Fisher 系统创建无有效正文 task_id 时，把可恢复头保留并向客户端发出明确结构化非2xx响应，或在 Fisher 专用传输面保留成功响应头；不重复 POST。补系统 proxy 的真实响应检查和客户端头恢复检查。

### IMPORTANT：跨源请求和响应头 CORS 合同缺失

位置：`backend/cmd/server/main.go:220`、`:245`。

新的前端 Fisher 传输总是携带 `X-Canvas-Upstream-Protocol`，但 CORS Allow-Headers 未包含该名字。前端和后端不同源且使用明确允许的 Origin 时，浏览器预检会拒绝请求。另一方面，CORS 未暴露 `X-Task-Id`，浏览器即使收到头也不能通过 Axios 读取它，错误响应恢复因而失败。

建议：在现有明确 Origin 策略内加入协议请求头，并显式 Expose-Headers 允许读取任务头；不扩大允许 Origin。补 OPTIONS 及普通响应头合同检查。该落点最初超出后端子任务所有权，现已由主会话授权补充。

### 其他审查结论

未发现其他阻塞项：Fisher 按实际 ID 建立角色、独立额度和音频默认，保持 0/false；创建前检查模型/文本/条件时长/固定与自适应比例/偏移及已知视频时长；响应头在读正文前通过已有 ProviderRequestID 保存，数据库失败转不确定并停止；无 ID 的超时/5xx/非法响应不回退 request_id 或重建；缺插件拒绝遗留回退。现有测试证据涵盖真实插件的请求转换及单 POST 后的头恢复。

首次结论：上述两个 IMPORTANT 已反馈主会话，等待完成修复后一次针对性复查。当前未声明系统路径或浏览器跨源运行验收通过。

## 后端一次针对性复查：PASS

原两个 IMPORTANT 已修复。Fisher 系统 POST 的 HTTP200 正文缺少有效 task_id 时，宿主返回结构化 502并保留过滤后的 X-Task-Id，账务保持待核对；不依赖前端成功响应头解析，不再丢失已有编号。CORS 允许 X-Canvas-Upstream-Protocol 并暴露 X-Task-Id，原明确来源策略保持不变。

执行 `go test ./internal/handler ./cmd/server -run 'TestFisherSystemMissingBodyTaskIDReturnsRecoverableError|TestFisherCORSCarriesProtocolAndTaskID|TestAllowedOrigin|TestParseCORSPolicy' -count=1`：handler、cmd/server 均通过（4.814s、3.295s）。系统检查使用完整 proxySystemRequestPath、隔离 SQLite/service 和模拟上游，验证非法 JSON/只有 request_id 的 HTTP200 均转换 502、保留任务头且只 POST 一次；CORS 检查通过真实中间件的 OPTIONS/GET，包含邻近来源规则回归。

本次只复查原发现及修复分支，没有扩展为最终集成审查；没有浏览器或真实付费上游证据。

## UI 与提交状态工作包首次独立审查

范围：task `dba57f72-d5ff-4e6e-9ef3-e33879178c6d`、`02117ce2-6692-46cc-9fdc-07e68f3d6291`、`5bd4c7be-cd3c-4f85-b013-5a58349b0926`、`bc057669-f32b-4943-b2e4-7f2fcfe63c03` 的前端贡献。检查预设、能力、真实 canvas controls、创作台、媒体 ID、后台提交前预检、确认指纹与历史恢复，不含插件源码。

### IMPORTANT：切换创作会话后残留旧 Fisher 媒体参数

位置：`web/src/pages/create/index.tsx:823`、`:872`、`:903`。

新会话、选择历史会话及删除当前会话后切换 fallback，均清空提示词、attachments 和 draftReferences，却没有清空新增加的 fisherMetadata。上一草稿的首尾帧、driveAudioId、videoStartTimes 因而残留。用户在新的空会话中提交普通提示词时，会被已失效媒体 ID 拦截，或者 UI 继续显示旧驱动对应的 4–60 秒范围。

操作复现：Fisher 草稿附加驱动并选中它，点击新会话（或历史会话），为新会话输入普通提示词后发送。实际状态转换把附件置空而保留驱动 ID；已检查 `validateFisherH3Submission` 会拒绝此状态的专项用例。该问题来自真实页面 handler 的遗漏，并非期望允许失效 ID 提交。

建议：上述已经清空草稿的三条转换路径同步 `setFisherMetadata({})`；`restoreMessageDraft` 仍从历史 settings 显式恢复。无需为三个简单 setter 新建状态抽象。

### SUGGESTION：删除带偏移的视频后无法在单项控件中清除失效偏移

位置：`web/src/components/fisher-h3-reference-controls.tsx:36`。

失效驱动和帧 ID 会显示可清除的失效选项，视频偏移只渲染当前 videos 数组。删除曾配置偏移的视频后，旧 ID 保留在 videoStartTimes，预检正确拒绝，但旧 ID 对应控件已经隐藏。用户只能清空全部素材/整个草稿，或重新加入原视频才能移除旧配置。建议在存在失效偏移时给出单项清除入口，或者明确的仅重置视频偏移按钮，避免删除其他合法素材。

### 验证与其余结论

执行 frontend-evidence 中的八文件专项及邻近命令：38 pass、0 fail、109 assertions。真实组件 SSR 显示 native 输入、首尾帧/驱动/偏移和 0/false；条件 UI 时长尊重声明范围/枚举交集；两条后台提交路径在 paid POST 前执行 Fisher 校验；普通非法时长保持原值等待拒绝；历史 settings 保留与恢复 Fisher 参数；确认指纹包含有效选项。未发现其它阻塞项。

对 collector 的 `promptOnly` 首尾帧过滤另做了手工 metadata 复现，但尚未证明该输入能由当前实际选择 UI 产生，因此不列为阻塞发现。浏览器交互/明暗布局没有在本次审查运行，静态组件结果不替代浏览器验收。

首次结论：一个 IMPORTANT 待修复后一次针对性复查；单项偏移清除为非阻塞建议。

## UI 一次针对性复查：PASS

原 IMPORTANT 的三条草稿清空路径均补上 `setFisherMetadata({})`：新建会话、选择会话、删除当前会话切换 fallback。历史恢复仍从 `settings.fisherMetadata` 显式恢复，未清除历史配置。逐项检查真实页面 handler；这些一行 setter 未新增状态抽象，也未声称运行浏览器会话切换验收。

偏移恢复建议也已修复：只要保存的 videoStartTimes 有条目，真实共享组件就显示“重置视频起始偏移”按钮，即使 videos 已为空。按钮清空偏移字典，沿已有 update 合并保留 driveAudioId、audio_control 和其他 providerOptions。

执行 `bun test test/fisher-h3-controls.test.tsx test/fisher-h3-ui-state.test.ts`：7 pass、0 fail、26 assertions。真实组件 handler 检查覆盖移除视频后的恢复入口及驱动/音频设置保留；邻近 UI 状态检查通过。本轮限于原发现及对应改动，不是最终集成或浏览器布局审查。

主会话另报告驱动非有限/负值/超上限 durationMs 建立 RED/GREEN 后已修正；这是独立的根协议补充，留待 Verify 集成覆盖，没有在本轮扩展重审。
