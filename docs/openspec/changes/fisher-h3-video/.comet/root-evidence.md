# 主会话 TDD 证据

## 前端请求合同（task a6ae1330-0a08-41b7-b286-05dba38ec546）

- RED：`cd web; bun test test/fisher-h3-video.test.ts`，退出 1，4 失败/1 通过。请求映射尚未实现，四个合法合同场景失败；输入校验测试尚不能单独证明已实现。
- GREEN：同一命令退出 0，5 通过/0 失败，25 断言。覆盖1080P、固定比例、尾帧与九图混合、驱动60秒/控制默认及0false、ID偏移及视频时长边界。
- RED：`cd web; bun test test/fisher-h3-api.test.ts`，退出 1，4 失败。实际适配尚未接入，合法 POST、严格任务编号、已有任务查询和错误终态场景失败。
- GREEN：`cd web; bun test test/fisher-h3-api.test.ts test/fisher-h3-video.test.ts test/video-api-contracts.test.ts`，退出 0，18 通过/0 失败，68 断言。无效参数不 POST、request_id 不代任务编号、创建错误携带 X-Task-Id 只查询已有任务、无 ID 不重发、HTTP200 task.error/cancelled 均失败，相邻官方 MiniMax/Seedance 合同正常。
- RED：`cd web; bun test test/fisher-h3-relay.test.ts`，退出 1，1 失败，缺少显式协议身份响应断言。
- GREEN：同一命令退出 0，1 通过/0 失败。Fisher 中转携带协议身份，官方 MiniMax 不携带该身份。

检查使用测试桩与本地真实请求构建；没有使用真实 API Key、没有付费请求、没有浏览器运行证据。

- 密钥反射 RED：`bun test test/fisher-h3-api.test.ts`，4 通过/1 失败，上游把测试 Key 回传为 X-Task-Id 时被接受。
- 密钥反射 GREEN：拒绝包含当前 Key 的任务编号；同一专项命令 5 通过/0 失败。
- 检查过程中曾因断言期待原始 Axios 文案而失败；现有 response 工具会把 502 转换为“创建结果不确定”，修正测试预期后通过，无产品行为回退。

## 独立任务审查修复

- error.code=0 RED：`bun test test/fisher-h3-api.test.ts`，5 通过/1 失败，成功响应误判 failed。
- 局部音频覆盖 RED：`bun test test/fisher-h3-defaults.test.ts`，1 失败，原有 mode/denoise 默认字段被丢弃。
- GREEN：`bun test test/fisher-h3-api.test.ts test/fisher-h3-defaults.test.ts test/fisher-h3-video.test.ts`，12 通过/0 失败，45 断言。
- 修复依据：仅非零业务错误或无 code 的错误消息视为失败；两条调用路径共用 generationMetadata 的 Fisher 嵌套 audio_control 合并，保留未覆盖的默认值。
- 一次针对性复查 PASS，见 root-task-review.md；`bun run --cwd web typecheck` 通过。

- 非有限驱动时长建议修复 RED：`bun test test/fisher-h3-video.test.ts`，4 通过/1 失败，NaN 绕过长度校验。
- GREEN：同命令 5 通过/0 失败、29 断言；拒绝非有限、负值和超过 60 秒的已提供驱动时长，未知时长仍遵循现有 0/缺省合同。
- 集成检查入口 `node web/scripts/check-fisher-h3.mjs` 顺序运行 Bun 与 Go 专项及邻近检查，任一失败即退出失败。
