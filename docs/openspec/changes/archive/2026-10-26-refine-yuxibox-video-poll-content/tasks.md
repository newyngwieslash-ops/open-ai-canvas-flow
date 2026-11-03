# Tasks

## 1. 插件定义与包

- [x] 1.1 在 `plugin-packages/generate-catalog.mjs` yuxibox 定义块：`response.videos` coalesce 链在 `video_url` 后插入 `response.download_url`、`response.downloadUrl`；新增 `result: { method: "GET", path: "/v1/videos/{{taskId}}/content", headers: { Accept: "video/mp4" } }`。验证：`node plugin-packages/generate-catalog.mjs` 重新生成 manifest 且字段正确 <!-- comet-task:b22feafa-2c87-4f88-ae6e-91a35fc906a8 -->
- [x] 1.2 运行 `node plugin-packages/embed-documentation.mjs yuxibox-seedance-video` 回填文档，重建 `yuxibox-seedance-video.yingce-plugin` 包（Node zip 手写脚本同 yuxibox 首次接入）。验证：包结构有效、manifest 与源码一致 <!-- comet-task:09c37e6e-d985-475e-8d56-c100c9c160c5 -->
- [x] 1.3 更新 `plugin-packages/yuxibox-seedance-video/README.md` 与 `docs/interface.md`：查询状态词全集（queued/in_progress/completed/failed）、`download_url` 字段、content 端点（7 天有效期短链、HEVC 提示）。验证：文档含新增内容 <!-- comet-task:b570d404-a42b-4e4a-a33c-882d550ec33f -->

## 2. 测试与文档

- [x] 2.1 `backend/internal/protocol/yuxibox_seedance_video_test.go` 补断言：`download_url` 命中提取链（仅 download_url 的成功响应取到视频）、result 端点 `BuildResult` 路径 `/v1/videos/{id}/content`。验证：`go -C backend test ./internal/protocol/ -run YuxiboxSeedance` 通过 <!-- comet-task:9ff2128a-2eca-4e0c-85fe-fb1db8c8bef8 -->
- [x] 2.2 `docs/content/docs/backend/http-api.mdx` yuxibox 小节补状态词全集、download_url、content 兜底端点、7 天有效期与 HEVC 提示。验证：文档与插件行为一致 <!-- comet-task:47fe2306-e1ec-4307-960c-2f4d3f915b74 -->
- [x] 2.3 `docs/content/docs/progress/pending-test.mdx` 更新 yuxibox 验收项：勾选已验证核心链路，登记 content 兜底端点与 HEVC 播放观察项。验证：条目更新 <!-- comet-task:117d9346-2236-429c-9d3e-f62f56c31a37 -->

## 3. 集成验证

- [x] 3.1 `go -C backend test ./internal/protocol/` 全绿 + `openspec validate refine-yuxibox-video-poll-content`（docs 根）。验证：通过 <!-- comet-task:fe7e43c0-694c-4649-934f-f96b82a9ed2d -->
- [x] 3.2 提交全部改动（`tweak: yuxibox seedance2.5 - 完善查询响应映射与内容获取兜底`）。验证：工作区干净 <!-- comet-task:7e1872b6-2254-450e-ab5f-be1240d3c1b8 -->
