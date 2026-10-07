# Comet Design Handoff

- Change: fisher-h3-video
- Phase: design
- Mode: compact
- Context hash: 97420bcd455542cdd73a065b0591c72fabe5661a85a4529b59a1a889fc6fecf8

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/fisher-h3-video/proposal.md

- Source: docs/openspec/changes/fisher-h3-video/proposal.md
- Lines: 1-41
- SHA256: c1ba1133aabde2b8da58fa2e79382c575657f413bb673f7564f2754af825e6c6

```md
# Proposal

## Why

用户需要在影策中使用 Fisher 的 MiniMax-H3 视频生成服务，并在接入后自行填写 API Key。现有 MiniMax V2 协议虽然已有创建和查询链路，但采用官方分辨率与参考限制，直接换成 Fisher Base URL 会错误发送 2K、限制尾帧及混合参考，且缺少独立驱动音频控制。

## What Changes

- 增加可发现、可配置的 Fisher-H3 视频提供商，默认 Base URL 为 `https://api.work-fisher.com`，上游模型名精确为 `MiniMax-H3`；API Key 不预填，复用现有渠道密钥输入和登录态后端中转。
- 声明 Fisher 专属能力：480P / 768P / 1080P、八种固定比例、普通 4–15 秒，以及独立驱动音频存在时的 4–60 秒。
- 支持文生视频、单独首帧、单独尾帧、首尾帧、参考图片/视频/音频与关键帧混合；提供驱动音频选择、`audio_control` 和参考视频起始偏移。
- 复用任务创建、轮询、结果下载及持久化；按 Fisher 契约保留明确错误，提交结果不确定时不自动重新创建付费任务。
- 提供每秒 0.15 / 0.30 / 0.45 元的参考价格及配置说明，实际渠道报价沿用现有配置。
- 同步接入文档和待验收清单，留下针对真实协议与边界的最小可运行检查。

## Capabilities

### New Capabilities

- `fisher-h3-video`: Fisher 视频渠道配置、参数能力、媒体角色、音频控制和异步生成生命周期。

### Modified Capabilities

无。现有规格目录只包含素材绑定和同步规格；本需求拥有独立的提供商行为边界。

## Impact

- `plugin-packages/` 中的声明式提供商、模型工作流与目录生成入口。
- `web/src/` 的模型渠道配置、视频能力、参数 UI、媒体角色及请求适配。
- `backend/internal/app/`、`backend/internal/protocol/` 与必要的中转鉴权边界，保证前后台同一契约。
- `docs/content/docs/` 中的模型接入说明与 `progress/pending-test.mdx`。
- 不新增依赖、不迁移数据库，不改变官方 MiniMax 提供商的既有能力，不写入现有账号或生产数据库。
- 本次不建设上游任务列表/删除管理页面、回调接收服务、素材 OSS SPI、主备用凭据轮换、2K、Context-IR 或再生成能力；这些不是生成渠道配置的必要部分。

## Resolved Brief

- **目标**：用户选择 Fisher-H3、填写自己的 Key 后，可沿现有生成任务链路完成受支持的视频生成。
- **范围边界**：一个提供商在既有设置、画布和创作生成路径中的配置、参数、请求、查询和结果保存；保留当前主工作区的无关修改。
- **关键未知项**：未提供可调用 Key，不能验证真实上游账号能力与付费生成；以用户提供的服务契约建立可运行的请求与响应检查，真实验收留待填写 Key 后完成。
- **验收草案**：无 Key 可保存配置；普通文生和尾帧请求正确；帧与参考可混合；驱动音频允许 60 秒并保留显式 0/false；无驱动的长任务、2K、非法比例和超限素材在提交前失败；成功读取 `task.content.url`；终态错误可见，超时不重建任务。
- **不拆分理由**：配置、能力、媒体映射与轮询共同组成同一生成链路，必须按同一协议验收。

```

## docs/openspec/changes/fisher-h3-video/design.md

- Source: docs/openspec/changes/fisher-h3-video/design.md
- Lines: 1-83
- SHA256: 01aec5bc88a911a90c13a331f3d4b9e2747a241f4264ed78d5702f15c4f02d1b

[TRUNCATED]

```md
---
comet_change: fisher-h3-video
role: technical-design
canonical_spec: openspec
---

# Design

## Context

动机及行为范围见 `proposal.md` 与 `specs/fisher-h3-video/spec.md`。本文件是唯一正式技术设计，用户已选择 A 确认本方案。

- 项目已有声明式 provider、workflow、插件目录和渠道参数能力机制；`plugin-packages/generate-catalog.mjs` 是官方插件产物的生成入口。
- 官方 MiniMax 插件贡献 `minimax-video`，默认官方地址，校验普通 4–15 秒、尾帧依赖首帧、禁止关键帧和普通参考混合，并强制关键帧比例为 adaptive。
- 前端 `video-provider-minimax.ts` 还有直接生成调用路径：分辨率会映射到 768P/2K，素材会 slice，时长会裁剪；不能把这些行为原样用于 Fisher。
- 后端通过 `provider_protocol_request.go` 从任务输入建立统一请求；图片已承载角色，普通视频/音频角色目前固定。厂商扩展已有 `providerOptions` 通道，网络、鉴权和媒体持久化由宿主管理。
- 用户选择隔离 worktree，分支 `codex/fisher-h3-video`，交付目标是 `sync/upstream-1.6.1`。本次 Comet 命令使用已安装的 Git 2.53；系统 Git 2.34 缺少 worktree 的 `-z` 功能。

## Goals / Non-Goals

**Goals:**

- 最大限度使用既有 provider/workflow 注册、渠道保存和异步任务运行能力。
- Fisher 参数在 UI、前端直连适配和后台任务适配中保持一致，所有付费创建路径校验同一行为合同。
- 无 Key 可以保存配置；填写 Key 后无需用户自行修改源代码或拼装 content。

**Non-Goals:**

- 不为 Fisher 新建独立生成页面、网络客户端、任务队列或报价系统。
- 不通过主机名猜测把官方 MiniMax 渠道改成 Fisher，也不改写当前账号或生产数据库。
- 上游管理 API、回调、备用凭据等边界以 proposal 为准。

## Decisions

### 1. Fisher 使用独立提供商身份

协议 ID 为 `fisher-h3-video`，展示名为 Fisher-H3，工作流/模型预设为 `MiniMax-H3`，Base URL 为 `https://api.work-fisher.com`。通过现有插件目录与渠道模型设置发现和选择。在现有添加渠道流程内提供 Fisher-H3 预设，预填地址、模型与协议，Key 留空；管理员模型编辑可选择相同协议。

复用 MiniMax V2 的 HTTP 路径、鉴权和响应语义，但使用 Fisher 专属参数和校验。直接替换官方默认地址虽然改动少，却会使其他 MiniMax 渠道受 Fisher 扩展影响；通过 URL 判断能力也会隐藏协议身份，因此不采用。

### 2. 生成走现有传输与任务宿主

主要数据流：渠道/模型选择 → 能力及媒体输入 → 当前生成服务 → 登录态后端中转或后台任务宿主 → Fisher 创建 → 同 Key 查询 → 现有下载和资源保存。

后台使用声明式插件请求构建与响应映射；前端直接调用路径复用现有 video transport 和响应工具，仅补 Fisher 请求适配。画布和创作页主要提交后端生成任务，模型连接测试调用前端视频接口，两条路径都执行 Fisher 校验。不预先建设新的适配框架。

### 3. 扩展信息沿现有 metadata 和 providerOptions 传递

驱动音频是明确角色的独立输入；三个普通参考音频与一条驱动音频分别计数。驱动选择、音频控制和按媒体 ID 绑定的视频偏移沿现有 `providerOptions["fisher-h3-video"]` 传递；首尾帧沿现有选择元数据传递。请求转换按稳定 ID 设置角色及偏移，不能按数组下标猜测。明确选中的帧与驱动加入实际提交素材集合；删除或替换造成引用失效时阻止提交，重试保留原选项。

复用画布和创作页真实参考/参数区域，仅在 Fisher 被选中时展示帧角色、驱动选择、模式、去噪、驱动作为参考和视频起始偏移。共享控件只承担这两个实际入口的重复逻辑。字段默认值按驱动存在与否解析，显式 0/false 以存在性判断保留，不引入数据库表。

### 4. 边界由请求构建前验证

分辨率、时长、文本、比例、素材数量、角色、音频模式和已知媒体时长在付费 POST 前验证。后端重复执行信任边界校验，防止绕过 UI。关键帧与普通参考独立计数，tail-only 合法；固定比例绝不因存在关键帧而被替换。

不沿用官方前端的 slice、时长裁剪或 2K 映射。必要时修正共享入口中的计数/角色保留逻辑，但通过提供商能力限定行为，不扩大所有视频协议的输入范围。

通用能力只表达总容量：11 张图片、4 条音频、3 条视频和 4–60 秒范围，避免合法混合输入被通用选择器提前截断。Fisher 校验再分别限制 9 张参考图、各 1 张首尾帧、3 条参考音频与 1 条驱动；无驱动只接受 4–15 秒。UI 按实际驱动限制时长，提交前拒绝非整数输入。已知视频时长须验证原素材与偏移后剩余均为 2–15 秒，未知时长复用媒体探测或保留上游明确校验。

分辨率说明保留原生尺寸对齐，1080P 的 16:9 为 1920×1088，不将通用估算尺寸显示成实际输出保证。标准参考价使用既有价格/说明能力，不改账户实际计费策略。

### 5. 提交不确定沿现有任务恢复能力处理

创建只接受真正 task ID；追踪 request_id 不代替 task_id。上游错误中的有效 `X-Task-Id` 必须在宿主中转和任务运行链路保留，通过已有 ProviderRequestID 状态持久化后查询已有任务。响应体读取失败也不能丢失已取得的任务编号。无 ID 的超时/5xx/无效 JSON 不触发重新创建；持久化失败不得伪装成安全可重新提交。中转鉴权只开放本协议所需的创建与查询路径，正常查询和取消信号沿用现有机制，不新增上游任务管理 API。

### 6. 验证和交付

在既有测试目录添加最小专项检查。Bun 覆盖空 Key 预设、实际请求构建、普通与驱动时长、独立角色计数、固定比例/尾帧、显式 0/false、ID 关联/偏移及实际提交集合；Go 覆盖真实声明式插件、信任边界校验、终态、X-Task-Id 恢复与禁止重复 POST、鉴权和响应头保留。根据改动风险执行相关检查及前端构建，并按 Comet 的最终 Verify 要求记录证据。没有真实 Key 不发起付费生成，不为静态验证启动开发服务。

接入说明提供 Key 填写位置、支持范围、标准参考价与上游输出对齐说明；`pending-test.mdx` 记录无真实 Key 的上游验收缺口。未经真实生成不得写成上游已通过。

## Risks / Trade-offs

- [只有用户提供的服务契约，没有真实 Key] → 用请求/响应专项检查验证协议；真实付费验收留给填入 Key 后的使用。
- [前端直接请求和后台声明式执行存在两条路径] → 逐一追踪调用方，验证二者生成相同 Fisher JSON，不只修用户提到的入口。
- [通用能力当前不区分独立驱动额度] → 使用总容量加 Fisher 专属角色校验，两条创建路径均须校验，不放宽其他提供商的校验。
- [媒体 URL 或元数据缺失] → 复用资源解析和上传，不在 UI 静默丢弃素材；无法证明本地时长时保留上游校验和具体错误。
- [创建超时可能已扣费] → 不自动重发，尽可能保留上游任务编号并继续查询。


```

Full source: docs/openspec/changes/fisher-h3-video/design.md

## docs/openspec/changes/fisher-h3-video/tasks.md

- Source: docs/openspec/changes/fisher-h3-video/tasks.md
- Lines: 1-21
- SHA256: 77314063ebfeaaa85367f89d7c6de92acab7e317c29d07e94ccc84e4029c9b39

```md
# Tasks

## 1. 提供商与渠道配置

- [ ] 1.1 在现有插件目录生成入口增加 Fisher-H3 声明式 provider 和 MiniMax-H3 模型预设，补插件文档及最小协议检查；验证创建/查询路径、默认 Base URL、模型名称及产物加载均正确。 <!-- comet-task:72abd7c7-a9e6-4141-b88d-343d256d5a6c -->
- [ ] 1.2 将 Fisher 专属能力接入前后台渠道配置，允许无 Key 保存，配置 480P/768P/1080P、比例、时长与参考价格说明；通过能力与配置检查验证 1080P 不转成 2K、缺 Key 不提交、官方 MiniMax 能力未被改写。 <!-- comet-task:dba57f72-d5ff-4e6e-9ef3-e33879178c6d -->

## 2. 媒体与参数交互

- [ ] 2.1 在现有媒体选择及任务 metadata 链路保留 Fisher 的首帧、尾帧和普通参考角色，独立计算数量；补角色专项检查及混合参考说明，验证 tail-only、首尾帧与九图混合以及各类超限行为。 <!-- comet-task:02117ce2-6692-46cc-9fdc-07e68f3d6291 -->
- [ ] 2.2 提供独立驱动音频选择与 audio_control 参数，沿现有 providerOptions 通道传递，补默认值/显式值检查和参数说明；验证驱动存在时 4–60 秒、非 native 必须有驱动、reference_only 冲突拒绝、0/false 保留。 <!-- comet-task:5bd4c7be-cd3c-4f85-b013-5a58349b0926 -->
- [ ] 2.3 提供绑定媒体的参考视频起始偏移，并验证已知素材与偏移后剩余时长；补偏移专项检查和 24 FPS/2–15 秒说明，验证 start_time_seconds 落在正确内容项及不足两秒时不提交。 <!-- comet-task:bc057669-f32b-4943-b2e4-7f2fcfe63c03 -->

## 3. 请求与异步结果

- [ ] 3.1 接通前端生成调用方，复用既有传输和媒体解析，发送 Fisher content/分辨率/比例/时长/音频控制；以真实请求构建专项检查验证字段、合法边界及非法参数无 POST，不静默裁剪输入。 <!-- comet-task:a6ae1330-0a08-41b7-b286-05dba38ec546 -->
- [ ] 3.2 接通后端任务与必要的中转鉴权，复用声明式运行、查询、下载保存及恢复流程，补最小任务检查和错误恢复说明；验证 HTTP 200 task.error、cancelled、task.content.url、不确定提交的 X-Task-Id 保存及无 ID 时不重建。 <!-- comet-task:7e3648c5-3671-4009-a27c-b521a37f7134 -->

## 4. 集成验收

- [ ] 4.1 执行覆盖最终输入面的专项检查及必要构建/集成审查，检查实际参数 UI 的可发现性和键盘行为，更新接入入口与 pending-test；验证所有规格场景有检查依据，真实 Key 生成明确标记待验收，交付 diff 聚焦且无密钥或本地数据。 <!-- comet-task:b50a3969-3691-47dd-94e6-1ba438c3f89d -->

```

## docs/openspec/changes/fisher-h3-video/.openspec.yaml

- Source: docs/openspec/changes/fisher-h3-video/.openspec.yaml
- Lines: 1-2
- SHA256: 720b3de6231f9647898feba395449b03234e0c4fa3bd4ce47fc3bb341b0f16d6

```md
schema: spec-driven
created: 2026-10-07

```

## docs/openspec/changes/fisher-h3-video/specs/fisher-h3-video/spec.md

- Source: docs/openspec/changes/fisher-h3-video/specs/fisher-h3-video/spec.md
- Lines: 1-133
- SHA256: 1fb7d43412cac3a87e3b18e6b43817030ec00ab6f930d2212c048706d327770a

[TRUNCATED]

```md
# Spec Delta

## Purpose

为影策提供 Fisher 的 MiniMax-H3 视频生成渠道，使用户能够在填写自己的 API Key 后，通过既有工作台提交文生、关键帧、多模态参考与独立驱动音频任务，并可靠查询和保存生成结果。该能力准确表达 Fisher 服务的扩展和限制，避免错误套用官方 MiniMax 的参数。

## ADDED Requirements

### Requirement: 提供可保存的 Fisher 渠道配置

系统 SHALL 提供可发现的 Fisher-H3 视频提供商，默认地址为 `https://api.work-fisher.com`，模型精确为 `MiniMax-H3`，API Key 留空并使用既有密钥输入。缺少 Key 时允许保存配置，但 MUST 阻止生成请求；密钥 MUST 经登录态后端中转，不进入 URL、日志或持久任务正文。

#### Scenario: 用户先配置再填 Key
- **WHEN** 用户选择 Fisher-H3，尚未填写 API Key
- **THEN** 可保存地址和模型，生成入口明确提示填写 Key
- **AND** 系统不使用占位密钥或其他渠道密钥提交

### Requirement: Fisher 分辨率和固定比例

系统 SHALL 仅接受 480P、768P、1080P 分辨率，并接受对应小写输入；固定比例为 1:1、2:3、3:2、3:4、4:3、9:16、16:9、21:9。系统 MUST 保留用户选择，不将 1080P 映射成 2K。1080P 原生对齐输出及计费按请求秒数的差异 SHALL 在文档中说明。

#### Scenario: 选择 1080P
- **WHEN** 用户选择 1080P 和 16:9
- **THEN** 请求携带 `resolution=1080P`、`ratio=16:9`，不承诺输出必为 1920×1080

#### Scenario: 不支持的分辨率
- **WHEN** Fisher 请求配置为 2K 或未知分辨率
- **THEN** 系统在付费提交前报错，不静默改用其他分辨率

### Requirement: 提示词与普通时长验证

系统 MUST 要求至少一个非空文本，拼接文本最多 10000 字符；普通任务时长 MUST 为 4–15 秒整数。纯文本请求 MUST 携带明确固定比例。无效参数 MUST 在创建付费任务前失败，不静默截断、取整或裁剪素材。

#### Scenario: 普通文生视频
- **WHEN** 用户提交有效提示词、480P、5 秒、16:9
- **THEN** 创建 JSON 使用 `content=[{type:"text",text:...}]` 及指定模型、分辨率、时长和比例
- **AND** 不发送顶层 `prompt`、seed、采样步数、flow shift、target 或 conditions

#### Scenario: 普通任务超限
- **WHEN** 无驱动音频时配置 16 秒、非整数时长或空提示词
- **THEN** 系统说明具体参数错误且不发送创建请求

### Requirement: 首尾帧及混合参考

系统 SHALL 支持单独首帧、单独尾帧及首尾帧，使用 `first_frame` 和 `last_frame` role，各至多一张；并 SHALL 允许关键帧与普通参考图片、视频、音频和驱动音频混合，各类独立计数。普通参考图片最多九张、参考视频最多三条、参考音频最多三条，独立驱动音频至多一条。

#### Scenario: 单独尾帧
- **WHEN** 用户只指定尾帧
- **THEN** 请求包含该图的 `last_frame` role，且不因缺少首帧被拒绝

#### Scenario: 混合输入
- **WHEN** 用户指定首尾帧、九张参考图、三个参考视频、三个参考音频和一条驱动音频
- **THEN** 在其他参数有效时分别保留这些素材与角色，不把关键帧或驱动音频占入普通参考类别额度

### Requirement: 比例的自适应边界

系统 SHALL 对存在关键帧的输入接受 `adaptive` / `auto` 并保留显式固定比例。纯参考场景省略比例时 SHALL 使用 16:9，显式 `adaptive` / `auto` MUST 报错。自适应 SHALL 说明是选择最近固定比例，不保证原图任意比例的精确输出。

#### Scenario: 首帧固定比例
- **WHEN** 带首帧的请求明确选择 9:16
- **THEN** 请求保留 9:16，不强制改为 adaptive

#### Scenario: 纯参考自适应
- **WHEN** 无关键帧的参考任务显式选择 adaptive
- **THEN** 系统在创建前拒绝并提示改选固定比例

### Requirement: 驱动音频与扩展时长

系统 SHALL 提供可发现的独立驱动音频选择并使用 `type=audio_url`、`role=drive_audio`；其最大时长为 60 秒。存在驱动音频时输出时长 SHALL 接受 4–60 秒整数；普通参考音频 SHALL 使用 `reference_audio` 并单独计数。

#### Scenario: 60 秒驱动任务
- **WHEN** 用户选择一条有效驱动音频并配置输出 60 秒
- **THEN** 请求保留 60 秒和 drive_audio role，不限制为 15 秒或改为 reference_audio

#### Scenario: 驱动超限
- **WHEN** 用户选取两条驱动音频或驱动音频超过 60 秒
- **THEN** 系统在创建前说明超限并拒绝提交

### Requirement: 音频控制模式和显式值


```

Full source: docs/openspec/changes/fisher-h3-video/specs/fisher-h3-video/spec.md
