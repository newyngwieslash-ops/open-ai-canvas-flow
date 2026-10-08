# Spec Delta：metaso-minimax-video-provider

## Purpose

密塔 H3（metaso.cn 中转 MiniMax V2）视频生成渠道能力：定义渠道配置、任务创建与查询协议、参数档位约束以及参考素材角色映射，使画布与创作页能通过该渠道完成文生视频、图生视频（首帧）和多模态参考生视频。

## ADDED Requirements

### Requirement: 渠道协议身份

系统 SHALL 提供声明式协议插件 `metaso-minimax-video`，以 `https://metaso.cn/api/minimax` 为默认 Base URL、Bearer API Key 鉴权，创建端点为 `POST /v2/video_generation`，查询端点为 `GET /v2/query/video_generation/{taskId}`。插件 MUST 声明 `requiresPublicMediaUrls`，参考素材以公网可访问 URL 提交。

#### Scenario: 管理员可选中密塔协议创建渠道

- **WHEN** 管理员在渠道配置中打开协议选择器并选中 `metaso-minimax-video`
- **THEN** 系统显示默认 Base URL `https://metaso.cn/api/minimax`，并要求填写 API Key

#### Scenario: 创建请求携带 Bearer 凭证与完整端点

- **WHEN** 系统向密塔提交视频生成任务
- **THEN** 请求方法为 POST、路径为 `{baseUrl}/v2/video_generation`，Authorization 头为 `Bearer <apiKey>`，且 API Key 不得出现在 URL 中

### Requirement: 参数档位约束

系统 SHALL 将密塔 H3 的生成参数约束为：`model` 固定 `MiniMax-H3`；`resolution` 可选 `480P`/`768P`/`1080P`/`2K` 且默认 `768P`；`duration` 为 4–15 整数秒且默认 5；`ratio` 可选 `21:9`/`16:9`/`4:3`/`1:1`/`3:4`/`9:16` 且默认 `16:9`。超出档位的请求 MUST 在提交上游前被拒绝，不得产生计费请求。

#### Scenario: 生成面板展示可选分辨率档位

- **WHEN** 用户在绑定密塔渠道的生成面板中查看分辨率选项
- **THEN** 可选项为 480P、768P、1080P、2K，初始选中 768P

#### Scenario: 超档参数被本地拒绝

- **WHEN** 用户选择时长 20 秒或分辨率 2K 后发起生成
- **THEN** 请求在本地校验阶段失败并提示合法档位，不向上游发出请求

### Requirement: 参考素材角色映射

系统 SHALL 把统一参考素材映射为 MiniMax V2 `content[]` 条目：提示词为 `text` 条目；首帧图片为 `image_url` 条目且 `role` 为 `first_frame`；参考图片为 `reference_image`；参考视频为 `video_url` 条目且 `role` 为 `reference_video`；参考音频为 `audio_url` 条目且 `role` 为 `reference_audio`。系统 MUST 支持三种工作模式：纯文本（文生视频）、含首帧图片（图生视频）、含参考视频与参考音频（多模态参考生视频）。

#### Scenario: 文生视频仅发送文本条目

- **WHEN** 用户不添加任何参考素材直接生成
- **THEN** `content[]` 只包含 `text` 条目，`ratio` 使用用户所选固定比例

#### Scenario: 图生视频发送首帧角色

- **WHEN** 用户指定一张图片为首帧并发起生成
- **THEN** 对应图片条目的 `role` 为 `first_frame`，URL 为该素材解析后的公网地址

#### Scenario: 多模态参考生视频同时发送参考视频与参考音频

- **WHEN** 用户同时添加参考视频和参考音频并发起生成
- **THEN** `content[]` 依次包含 `text`、`video_url`（role `reference_video`）、`audio_url`（role `reference_audio`）条目

### Requirement: 异步任务查询与结果归一

系统 SHALL 以 `GET /v2/query/video_generation/{taskId}` 轮询任务状态，将上游 `task.status` 归一为本系统状态（成功/失败/进行中），成功时从 `task.content.url` 取回视频地址并持久化，失败时向上呈现上游错误消息。系统 MUST 在创建响应缺失 `task_id` 时按“结果不确定”处理，不得引导用户直接重发付费任务。

#### Scenario: 任务成功取回视频

- **WHEN** 查询响应 `task.status` 为 `succeeded` 且 `task.content.url` 非空
- **THEN** 系统将该地址下载持久化为任务结果视频

#### Scenario: 任务失败呈现上游错误

- **WHEN** 查询响应包含非零 `task.error.code` 或失败状态
- **THEN** 任务进入失败态，错误消息包含上游错误信息

#### Scenario: 创建响应缺少任务编号

- **WHEN** 创建请求后未获得可用的 `task_id`
- **THEN** 系统提示创建结果不确定、请核对已有任务与费用，不自动重发
