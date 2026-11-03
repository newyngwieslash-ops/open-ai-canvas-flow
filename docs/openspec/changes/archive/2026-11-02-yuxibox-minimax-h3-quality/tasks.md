# Tasks

- [x] 1. 新建 `plugin-packages/yuxibox-minimax-h3-quality/`：`manifest.json`（provider 声明、create/poll 模板、response 映射、capabilityConfig）、`README.md`、`docs/interface.md`。
- [x] 2. 注册到 `plugin-packages/generate-catalog.mjs` 并生成 `yuxibox-minimax-h3-quality.yingce-plugin` 包。
- [x] 3. 契约测试 `backend/internal/protocol/yuxibox_minimax_h3_quality_test.go`：请求体逐字段（`720p→768p`、`seed` 透传、空 `audios` 省略、`videos` 拒绝）、响应四态映射、`video_url` 优先。
- [x] 4. 插件文档 `docs/content/docs/plugins/yuxibox-minimax-h3-quality.mdx` + `pending-test.mdx` 登记待验收项（含时段差价与真实上游生成）。
- [x] 5. 验证：`go test ./internal/protocol/`、`go build ./...`、`cd web && bun run build`，并核对生成的插件包能被后端加载。
