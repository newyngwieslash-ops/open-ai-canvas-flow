# fisher-h3-video Specification

## Purpose
为影策提供 Fisher 的 MiniMax-H3 视频生成渠道，使用户能够在填写自己的 API Key 后，通过既有工作台提交文生、关键帧、多模态参考与独立驱动音频任务，并可靠查询和保存生成结果。该能力准确表达 Fisher 服务的扩展和限制，避免错误套用官方 MiniMax 的参数。

## Requirements

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

系统 SHALL 支持 native、lock_source、remix_source、reference_only，去噪强度为 0–1，驱动作为参考为布尔值。有驱动时默认 lock_source/0/true，无驱动时默认 native/0.35/false。非 native 模式 MUST 有驱动；reference_only 的驱动作为参考 MUST 为 true。显式 0 和 false MUST 保留。

#### Scenario: 明确关闭驱动参考
- **WHEN** 有驱动且用户选择 remix_source、去噪 0、驱动作为参考 false
- **THEN** 请求完整携带这些值，不被默认值替换

#### Scenario: reference_only 冲突
- **WHEN** reference_only 模式下显式设置驱动作为参考 false，或非 native 模式未选驱动
- **THEN** 系统在创建前给出具体错误

### Requirement: 参考视频偏移

系统 SHALL 对每条参考视频支持非负 `start_time_seconds`，明确其按 24 FPS 转换的语义。参考视频及偏移后的剩余素材 MUST 满足 2–15 秒限制；素材时长已知时 MUST 在本地验证，不把偏移作为 URL 参数传递。

#### Scenario: 有效偏移
- **WHEN** 用户为已知十秒参考视频设置起始两秒
- **THEN** 内容项顶层携带 `start_time_seconds=2` 并保留 reference_video role

#### Scenario: 剩余时长不足
- **WHEN** 用户为十秒视频设置起始九秒
- **THEN** 系统在付费提交前拒绝，提示剩余素材少于两秒

### Requirement: 异步任务和结果

系统 SHALL 通过 `POST /v2/video_generation` 创建任务，读取 `task_id`，使用同一渠道凭据查询 `GET /v2/query/video_generation/{task_id}`。queued/running SHALL 保持待完成；succeeded SHALL 读取 `task.content.url` 并沿既有流程保存结果；failed/cancelled SHALL 终止并展示错误。

#### Scenario: 任务成功
- **WHEN** 创建返回任务 ID，随后查询返回 succeeded 和视频 URL
- **THEN** 系统保存任务 ID，查询同一任务并下载持久化输出，不使用旧 file_id 检索

#### Scenario: HTTP 200 中的终态失败
- **WHEN** 查询的 HTTP 状态为 200，但 `task.status=failed` 且有 `task.error`
- **THEN** 系统展示上游错误消息，不把 HTTP 200 当作生成成功

### Requirement: 不确定提交不自动重建

系统 MUST 在创建超时、5xx 或无效响应时停止自动重发付费创建请求。返回 `X-Task-Id` 时 SHALL 保存并支持继续查询该任务；请求追踪 ID MUST 不冒充任务 ID。轮询 SHALL 沿用现有取消语义及约五到十秒间隔。

#### Scenario: 提交不确定但存在任务 ID
- **WHEN** 创建返回错误且响应带 `X-Task-Id`
- **THEN** 系统保存该任务 ID，并查询已有任务，不创建第二个任务

#### Scenario: 没有任务 ID 的超时
- **WHEN** 创建超时且无可恢复任务 ID
- **THEN** 系统说明结果不确定，不自动重新提交

### Requirement: 参考价格与真实验收边界

系统 SHALL 在配置说明中提供 480P/768P/1080P 每秒 0.15/0.30/0.45 元的标准参考价，注明实际价格取决于分组和折扣，沿用现有报价机制。系统交付 MUST 区分模拟协议验证与使用真实 Key 的付费验收。

#### Scenario: 尚未提供真实 Key
- **WHEN** 协议检查通过但用户尚未填写 API Key
- **THEN** 交付明确记录真实生成待验收，不宣称上游生成已验证成功
