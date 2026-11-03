# Proposal：完善 yuxibox 视频任务查询响应映射与内容获取兜底

## Why

用户提供了 yuxibox 官方「视频任务查询」与「视频内容获取」两份接口文档。与创建文档对照，查询完成态的权威字段为 `video_url` **和 `download_url`**（双字段），且上游提供 `GET /v1/videos/{taskId}/content` 二进制内容端点（MP4，Bearer 鉴权，7 天有效期，部分视频为 HEVC）。当前插件 `yuxibox-seedance-video` 的响应映射只覆盖 `video_url`（coalesce 链含 video_url/videoUrl/result_url/url/data.video_url/output.url），缺 `download_url`；也未声明 `result` 内容端点作为 URL 提取失败时的兜底下载路径。实测成功任务用的是 `video_url` 且可下载，功能已通，但按官方文档补齐字段与兜底端点可以消除单一字段依赖的脆弱性。

## What Changes

- `plugin-packages/generate-catalog.mjs` yuxibox 定义块：
  - 响应 `videos` coalesce 链在 `video_url` 之后插入 `response.download_url`（与 `downloadUrl`）。
  - 新增 `result` 声明：`GET /v1/videos/{{taskId}}/content`（Accept video/mp4），作为 URL 缺失/不可下载时宿主的兜底二进制下载端点。
- 重新生成 manifest 并重建 `yuxibox-seedance-video.yingce-plugin` 包。
- 契约测试补断言：download_url 命中提取链、result 端点路径与鉴权。
- 插件文档（README/interface.md）与渠道协议文档补充：状态词 `queued`/`in_progress`（处理中）/`completed`（完成）/`failed`（失败）、`/v/` 短链 7 天有效期、HEVC 编码提示。
- `docs/content/docs/progress/pending-test.mdx` 更新 yuxibox 验收项（勾选已验证项，登记 content 兜底与 HEVC 观察项）。

## Capabilities

### Modified Capabilities

- `yuxibox-seedance-video-provider`：「异步任务查询与结果归一」需求的成片提取字段增加 `download_url`，并新增 content 端点兜底下载行为。

## Impact

- 插件定义与包：`plugin-packages/generate-catalog.mjs`、`plugin-packages/yuxibox-seedance-video/`、`plugin-packages/yuxibox-seedance-video.yingce-plugin`。
- 测试：`backend/internal/protocol/yuxibox_seedance_video_test.go` 补 2 个断言面。
- 文档：`docs/content/docs/backend/http-api.mdx` yuxibox 小节、`docs/content/docs/progress/pending-test.mdx`。
- 无数据库、无前端、无宿主代码改动；delta spec 变更走 MODIFIED Requirements。
