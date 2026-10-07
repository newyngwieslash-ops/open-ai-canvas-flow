---
change: fisher-h3-video
design-doc: docs/openspec/changes/fisher-h3-video/design.md
base-ref: 639c53e3d9970a109e6e1542572fc9c022642548
archived-with: 2026-10-07-fisher-h3-video
---

# Fisher-H3 实施计划

<!-- comet-task-authority: docs/openspec/changes/fisher-h3-video/tasks.md -->

执行配置：自主执行、TDD、标准独立审查；语言中文。规格和正式设计是行为与落点依据，不新建依赖、数据表或任务系统。

## 插件及能力

<!-- comet-task-ref:72abd7c7-a9e6-4141-b88d-343d256d5a6c -->
新增独立声明式插件与工作流，由现有脚本生成目录和安装包。先添加真实插件载入/请求映射失败检查，再生成插件使其通过。插件范围由一个实现者负责。

<!-- comet-task-ref:dba57f72-d5ff-4e6e-9ef3-e33879178c6d -->
前端实现者负责能力及空 Key 渠道预设；后端实现者同步能力默认值。依赖插件 ID 为 fisher-h3-video。先验证缺少预设/能力的 RED，再验证分辨率、额度和 Key 行为的 GREEN。参考价格仅说明，不改实际账户计费。

## 输入和 UI

<!-- comet-task-ref:02117ce2-6692-46cc-9fdc-07e68f3d6291 -->
前端实现者负责现有画布/创作页帧选择和实际提交集合；后端实现者负责角色恢复与边界校验。首尾帧选项复用 videoStartFrameNodeId/videoEndFrameNodeId，以实际媒体 ID 关联；普通参考独立计数。验证 tail-only、混合和超限。

<!-- comet-task-ref:5bd4c7be-cd3c-4f85-b013-5a58349b0926 -->
驱动与控制参数使用 providerOptions["fisher-h3-video"] 中的 driveAudioId 和 audio_control。默认值按字段存在性处理；前端 UI 按驱动决定时长范围。先检查默认、0/false、冲突及驱动 ID 存在性，再补实现。

<!-- comet-task-ref:bc057669-f32b-4943-b2e4-7f2fcfe63c03 -->
视频偏移使用同一命名空间下 videoStartTimes，以媒体 ID 为键，映射到内容项 start_time_seconds；验证非负有限数、原素材和剩余时长 2–15 秒。前端控件、直接映射和后端转换分别验证。

## 协议与付费边界

<!-- comet-task-ref:a6ae1330-0a08-41b7-b286-05dba38ec546 -->
主会话负责 web/src/lib/fisher-h3-video.ts 纯校验/映射及 web/src/services/api/ 中的 Fisher 适配、分派、合同和测试。前端 UI 实现者使用该 helper，不修改它。复用现有 transport、媒体解析与响应保存。先运行最小 Bun RED，再 GREEN，非法参数必须在 POST 前失败，不截断或裁剪。

<!-- comet-task-ref:7e3648c5-3671-4009-a27c-b521a37f7134 -->
后端实现者负责 backend/internal/app 与 handler 的任务、角色、校验、中转鉴权及响应头。复用已有 ProviderRequestID 恢复；创建只接受 task_id，保留有效 X-Task-Id 并查询，不自动重发不确定的 POST。专项 Go RED/GREEN 包括错误响应与读取失败。

## 集成与交付

<!-- comet-task-ref:b50a3969-3691-47dd-94e6-1ba438c3f89d -->
主会话整合及专题文档；标准模式下独立审查有风险的协议/输入/任务工作包，按要求修复一次复查。最终集成审查留给 Verify，不在 Build 重复。运行相关 Bun/Go 检查和前端构建，记录实际证据；不使用真实 Key，不启动 dev server。真实上游生成记录待验收。

任务实施可按文件所有权并行，验收仍逐 task ID；实现者不提交和勾选任务。主会话统一保存协调记录和提交。回退可禁用新增插件和渠道，不删除已有任务/素材。
