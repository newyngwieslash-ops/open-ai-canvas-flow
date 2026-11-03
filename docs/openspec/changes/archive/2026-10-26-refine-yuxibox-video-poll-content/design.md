# Design：yuxibox 查询响应映射与内容获取兜底

## Context

实测成功任务返回 `video_url` 且宿主下载成功（30MB mp4 已入七牛）。官方查询文档补齐了完成态字段全集：`video_url` + `download_url`（同一 `/v/<token>` 短链，7 天有效），并给出 `GET /v1/videos/{taskId}/content` 二进制端点（Bearer 鉴权，支持 Range，部分视频 HEVC）。状态词全集：`queued`/`in_progress`（处理中）、`completed`（完成）、`failed`（失败）——宿主 `normalizeStatus` 已覆盖（queued→pending、in_progress→processing、completed→succeeded、failed→failed），无需改动。

## Goals / Non-Goals

**Goals:**

- `videos` 提取链加入 `download_url`/`downloadUrl`（video_url 之后）。
- 声明 `result: GET /v1/videos/{{taskId}}/content`，作为成片 URL 缺失时宿主的兜底二进制下载（与 metaso `GET /videos/{id}/content` 同构，宿主在 `video_url` 为空时自动走 result 端点）。
- 测试与文档同步。

**Non-Goals:**

- 不改状态归一（宿主已覆盖全部状态词）。
- 不改创建映射、validations、能力默认值。
- 不处理 HEVC 转码（播放器兼容问题登记待验收即可）。

## Decisions

1. **download_url 插在 coalesce 链第二位**（video_url 之后）。官方文档两字段值相同（同一短链），video_url 优先保持与实测行为一致。
2. **声明 result 端点而非仅依赖 URL**。宿主声明式协议在成功态无 URL 时走 result 端点下载（`provider_protocol.go` 既有行为），content 端点带 Bearer 鉴权由宿主统一注入。
3. **文档同步**：http-api.mdx yuxibox 小节补状态词全集、7 天有效期与 HEVC 提示。

## Risks / Trade-offs

- content 端点对部分编码（HEVC）返回的字节与 MP4 声明一致，宿主按 `video/mp4` 处理，无额外分支。
- 短链 7 天有效期：`resultEphemeral: true` 已保证宿主立即下载持久化，不受有效期影响。
