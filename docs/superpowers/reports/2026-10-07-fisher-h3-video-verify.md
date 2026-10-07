# Fisher-H3 完整验证报告

结论：**PASS**。最终独立集成审查和当前输入面的 Runtime 构建/专项检查均通过，无未解决 CRITICAL/IMPORTANT。个人中转 JSON/SSE 恢复和负媒体时长问题均已关闭。

变更：`fisher-h3-video`；绑定分支：`codex/fisher-h3-video`；实现基线：`639c53e3d9970a109e6e1542572fc9c022642548`。工作区为已确认的隔离 worktree，主工作区原有修改没有纳入实现提交。采用 Comet full/autonomous/TDD/standard/full verify。

| 维度 | 结果 |
| --- | --- |
| 完整性 | PASS：8/8 任务、11/11 需求可定位，20 场景逐项核对 |
| 正确性 | PASS：69 项 Bun 检查、Go app/handler/protocol/server 专项通过，generation 包编译通过；关键失效分支没有第二次 POST |
| 一致性 | PASS：正式设计、proposal 与 delta spec 一致，无设计漂移 |
| 集成审查 | PASS：独立首审及受影响部分补查关闭全部发现 |

## 完整性与规格覆盖

OpenSpec 适配器的 status/instructions apply 确认 8/8 任务完成，4 类产物存在；strict validate 通过，设计 handoff 为 FRESH。11 项需求、20 个场景按真实入口和专项检查核对如下。

| 需求与场景 | 实现与检查依据 |
| --- | --- |
| 可保存渠道、缺 Key 拦截 | `use-config-store.ts` 的空 Key Fisher 预设、渠道添加按钮、既有 `assertVideoConfig`；`fisher-h3-config.test.ts`，另通过真实 dispatcher 的 Bun 内联检查确认缺 Key 在建立 transport 前失败 |
| 1080P 保留、拒绝 2K、固定比例 | `fisher-h3-video.ts` 与后端信任边界；Bun payload、实际 API 与 Go capability/plugin/invalid-input 检查 |
| 文生 JSON、提示词/普通时长、非法请求无 POST | Fisher payload 不发送旧 prompt/seed 等字段；Bun payload/API、Go invalid-input 的 create spy 与真实包参数检查 |
| 单独尾帧、独立混合额度 | 前端实际媒体 collector、图角色 helper、后台 request 角色与插件 content；Bun canvas-inputs/payload，Go stable-roles/legal-mixed/plugin 检查；Bun 内联调用真实 payload helper 验证 9 图 + 首尾帧 + 3 视频 + 3 参考音频 + 1 驱动全部 18 媒体保留 |
| 关键帧固定比例与参考 adaptive 拒绝 | Fisher 请求保留固定比例；Bun tail-only/fixed 与 Go invalid-input/plugin 分支覆盖 |
| 驱动 60 秒、两驱动/超长拒绝 | providerOptions 驱动 ID 与 audio role；Bun payload，Go plugin 驱动数量与宿主额度检查；已提供非有限时长也拒绝 |
| 音频模式、默认值、0/false 与冲突 | `generationMetadata` 合并渠道默认和局部控制，Fisher helper/宿主/插件执行约束；Bun defaults/controls/payload，Go defaults/validation/explicit-values 检查 |
| ID 绑定偏移、剩余不足拒绝 | 实际 UI 数字字段、请求内容项顶层 start_time_seconds；Bun payload/ui-state，Go stable-roles/invalid-input；文档说明 24 FPS 与未知时长需上游检查 |
| 创建/查询/成功 URL/终态错误 | 独立前端适配和真实声明式插件，复用已有资源保存；Bun API 与 Go response/任务执行检查；error.code=0 成功回归通过 |
| 不确定提交、X-Task-Id 与不重建 | 前端错误恢复、后台正文读取前持久化 ProviderRequestID、系统/个人中转与 CORS；Bun API/relay，Go header-polling/no-task-id/persistence/proxy/CORS；个人 2xx 缺编号与非 JSON 响应已转可恢复错误，真实 transport/provider 补查确认 POST=1、查询已有 ID 的 GET=1 |
| 参考价与无真实 Key 验收边界 | Fisher 专题说明价格、1920×1088、17n+5 帧和请求秒数计费；pending-test 明确真实生成与浏览器验收待完成 |

## 正确性、一致性与安全

配置的唯一正式技术设计是 `docs/openspec/changes/fisher-h3-video/design.md`，plan 和 handoff 引用一致，没有另建第二份技术设计。新增独立协议与包、复用传输/任务/资源、不迁移数据库、不改账户价格的六项设计决定均有对应实现；delta spec 与设计没有漂移。

实现遵循已有 channel transport 和登录态中转；Fisher 协议身份明确配置，不按域名猜测。已有 SSRF、来源与权限策略继续执行。任务编号过滤不接受追踪 ID、危险字符串或反射 Key；后台持久化失败保持不确定，缺插件拒绝创建，测试断言不重复 POST。没有真实密钥、本地数据库、上传素材或构建产物加入提交。

真实控件使用带标签的原生 select/number/checkbox/details，并保留画布滚轮边界。SSR 与实际按钮 handler 验证默认值、显式 0/false、偏移清除；会话清空同步重置 Fisher 参数，历史显式恢复保留。浏览器布局、明暗主题和键盘实际操作尚未运行，不能以 SSR 替代。

## 检查证据

- TDD 与任务独立审查：change 的 `.comet/*-evidence.md`、`root-task-review.md`、`plugin-task-review.md`；原发现均经有界修复与一次针对性复查。
- 首次正式构建：Comet Runtime 执行 `bun run --cwd web build`，退出 0；日志 `.comet/checks/1a97a6f4-fc98-40d1-80cd-b27390ea7003.log`。构建仅有体积较大 chunk 的提示。
- 集成检查入口：`node web/scripts/check-fisher-h3.mjs`，顺序运行前端 Fisher/邻近测试与后端 app/handler/protocol/generation/server 相关测试，任一失败即返回非零。
- 最终正式构建：同一 Runtime build 命令退出 0，日志 `.comet/checks/7fd4115a-d4ca-4a1a-952a-984831381941.log`，包含类型检查和生产打包；没有构建错误，仅 chunk 体积提示。
- 最终 Runtime verify：`node web/scripts/check-fisher-h3.mjs` 退出 0，日志 `.comet/checks/ad620a48-f2f9-4659-bc7b-97434b7f3448.log`。Bun 15 文件、69 pass/0 fail、218 断言；Go app 4.949s、handler 5.303s、protocol 2.637s、server 2.911s 均通过，generation 0.698s 编译通过且此筛选无独立测试。
- 最终实现提交：`8202f2f3`；`git diff --check` 通过。无新依赖或锁文件变化；原主工作区状态已复核，原有 AGENTS/其他 Comet 状态和未跟踪个人资料没有进入这两个实现提交。
- 最终独立集成审查：`2026-10-07-fisher-h3-video-integration-review.md`，结论 PASS。个人中转缺正文编号及 SSE 分支按同一 IMPORTANT 返回 Build 修复，负媒体时长建议一并修复；针对性补查全部关闭，没有接受偏差。

尚未提供真实 Key，未发起付费生成或素材上传。真实输出效果、媒体上传与账号价格、浏览器交互见 `docs/content/docs/progress/pending-test.mdx`，属于既定交付边界。
