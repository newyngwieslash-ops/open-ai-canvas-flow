# yuxibox Minimax H3 Quality 视频协议

## ADDED Requirements

### Requirement: minimax-h3-quality 视频请求协议

系统 SHALL 提供独立插件 `yuxibox-minimax-h3-quality`（能力 `video`，默认 Base URL `https://yuxibox.cn`，鉴权 `bearer`），并以 `application/json` 向 `POST /v1/videos` 提交生成请求。

请求体 SHALL 包含：`model`、`prompt`（1–20000 字符）、`seconds`（1–15 整数，默认 5）、`resolution`（`480p` 或 `768p`，默认 `768p`）、`ratio`（`16:9`、`9:16`、`1:1`，默认 `9:16`）、`reference_images`（1–9 个公网 HTTPS 地址）、`audios`（0–3 个公网 HTTPS 地址，可选）、`seed`（可选整数）。

分辨率档位 SHALL 接受 `720p` 作为 `768p` 的别名输入，并在发往上游前归一为小写 `768p`。

#### Scenario: 720p 归一为 768p

- **WHEN** 用户选择分辨率 720P
- **THEN** 请求体 `resolution` 为 `768p`
- **AND** 能力配置中暴露的档位为 `480P`、`768P`（不暴露 `720P` 为独立档）

#### Scenario: 参考图与音频数量上限

- **WHEN** 请求携带 10 张参考图或 4 条参考音频
- **THEN** 宿主在准入阶段拒绝该请求
- **AND** 不向上游发出任何生成请求

### Requirement: 不支持参考视频

该协议 SHALL 声明不支持参考视频：能力配置 `references.maxVideos = 0`，参数 `videos` 映射为 `unsupported`。携带参考视频的请求 SHALL 在准入阶段被拒绝，而不是静默丢弃。

#### Scenario: 参考视频被拒绝

- **WHEN** 用户为该模型连入参考视频节点并提交
- **THEN** 宿主返回明确的参数错误
- **AND** 上游不收到请求

### Requirement: 任务查询与结果持久化

系统 SHALL 通过 `GET /v1/videos/{{taskId}}` 轮询任务状态：`queued` 与 `in_progress` 归一为进行中，`completed` 取 `video_url`（缺失时回退 `download_url`），`failed` 呈现上游错误信息。响应中的媒体地址标记为 `resultEphemeral`，由宿主立即下载持久化，避免短链 7 天过期。

#### Scenario: 完成态取 video_url

- **WHEN** 轮询返回 `status=completed` 且含 `video_url`
- **THEN** 宿主下载该地址并保存为画布资源
- **AND** `video_url` 与 `download_url` 同时存在时优先 `video_url`

#### Scenario: 失败态保留上游原因

- **WHEN** 轮询返回 `status=failed`
- **THEN** 任务呈现上游 `error.message` 或等价失败描述
- **AND** 不重试创建、不产生第二个任务
