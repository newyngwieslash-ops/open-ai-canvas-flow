# Spec Delta

## MODIFIED Requirements

### Requirement: 异步任务查询与结果归一

系统 SHALL 以 `GET /v1/videos/{task_id}` 轮询任务状态，将上游 `status` 归一为本系统状态（`queued`/`pending`/`in_progress` 等为等待或处理中，`succeeded`/`completed` 为成功，`failed`/`cancelled` 为失败），成功时按 `video_url`、`download_url` 及兼容字段顺序提取视频地址并持久化；成片地址缺失时系统 SHALL 以 `GET /v1/videos/{task_id}/content`（Bearer 鉴权）下载二进制内容作为兜底。系统 MUST NOT 为同一视频重复提交创建请求：恢复任务已有任务 ID 时只查询不重建。创建响应缺失任务 ID 时 MUST 按"结果不确定"处理，不得引导用户直接重发付费任务。

#### Scenario: 创建与轮询

- **WHEN** 创建请求返回 `{"id": "task_example", "status": "queued"}`
- **THEN** 系统保存任务 ID 并以 `GET /v1/videos/task_example` 轮询，直至状态归一为成功或失败

#### Scenario: 处理中状态归一

- **WHEN** 轮询返回 `{"status": "in_progress", "progress": 50}`
- **THEN** 系统按"处理中"继续轮询，不标记失败

#### Scenario: 完成态 URL 提取

- **WHEN** 轮询返回 `{"status": "completed", "video_url": "https://yuxibox.cn/v/a", "download_url": "https://yuxibox.cn/v/b"}`
- **THEN** 系统优先取 `video_url`；仅有 `download_url` 时同样命中提取

#### Scenario: 内容端点兜底下载

- **WHEN** 任务成功但响应缺失全部 URL 字段
- **THEN** 系统以 `GET /v1/videos/{task_id}/content`（Bearer）下载 MP4 二进制并持久化

#### Scenario: 恢复任务不重复计费

- **WHEN** 任务在创建后因轮询失败进入恢复流程且已有任务 ID
- **THEN** 系统只继续查询该任务，不再次调用创建端点

#### Scenario: 结果不确定语义

- **WHEN** 创建请求 HTTP 成功但响应缺失任务 ID
- **THEN** 系统按"结果不确定"报告错误，不自动重发创建请求
