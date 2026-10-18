# Tasks

## 1. 插件包与生成管线

- [x] 1.1 在 `plugin-packages/generate-catalog.mjs` 新增 `yuxibox-seedance-video` 插件定义：provider（id `yuxibox-seedance-video`、baseUrl `https://yuxibox.cn`、bearer 鉴权、`requiresPublicMediaUrls`、model 精确 `seedance2.5-yx-count`、prompt 1–16000 校验、seconds 4–30 默认 30、ratio 七档校验、参考上限 30 图/10 视频/10 音频校验、create `POST /v1/videos` JSON：`model`/`prompt`/`seconds`/`ratio`/`reference_images`/`videos`/`audios`、poll `GET /v1/videos/{{taskId}}`、响应多字段 coalesce 提取视频 URL）与 workflow（defaults：model `seedance2.5-yx-count`、seconds 30、ratio `16:9`）。验证：`node plugin-packages/generate-catalog.mjs` 生成 manifest 且包含新插件 <!-- comet-task:78f4fe0d-3d97-4dad-a07d-4475a30dde79 -->
- [x] 1.2 编写 `plugin-packages/yuxibox-seedance-video/README.md` 与 `docs/interface.md`（协议身份、配置字段 apiKey、统一字段映射、请求示例、响应与错误、计费与兼容边界），运行 `node plugin-packages/embed-documentation.mjs yuxibox-seedance-video` 回填 manifest documentation。验证：manifest documentation 包含两份文档全文 <!-- comet-task:ec1bae1f-6178-4d69-b030-f7c61dcff4f5 -->
- [x] 1.3 运行 `sh plugin-packages/build-packages.sh` 生成 `yuxibox-seedance-video.yingce-plugin`。验证：`plugin-packages/` 出现该包且 `unzip -l` 含 manifest.json、README.md、docs/interface.md <!-- comet-task:a31f5d7e-dd40-4159-a1c0-c5c15aad18dd -->

## 2. 后端注册与能力默认值

- [x] 2.1 在 `backend/internal/generation/registry.go` 的 `OfficialDeclarativeVideoInterface` 注册 `yuxibox-seedance-video` → 显示名"yuxibox Seedance"。验证：`go -C backend build ./...` 通过 <!-- comet-task:a0cc38d4-3b07-4515-948d-c03eecc231c9 -->
- [x] 2.2 在 `backend/internal/app/model_capability_defaults.go` 为 `yuxibox-seedance-video` 添加能力默认值：Duration range 4–30 step 1 默认 30；Ratios `["adaptive","16:9","9:16","1:1","4:3","3:4","21:9"]` 默认 `16:9`；Resolutions 仅 `["720p"]`；参考上限 MaxImages 30 / MaxVideos 10 / MaxAudios 10（字节上限沿用宿主默认）；GenerateAudio/Watermark 不支持。验证：`go -C backend build ./...` 通过 <!-- comet-task:936004e0-1b61-41e5-8404-2d6919622c05 -->
- [x] 2.3 新增 `backend/internal/protocol/yuxibox_seedance_video_test.go`：仿 `metaso_minimax_video_test.go` 用 `officialPackageAdapter` 断言插件包契约（身份与 workflow defaults、模型精确 seedance2.5-yx-count、时长/比例/数量校验拒绝、create JSON 体含 reference_images/videos/audios 与 seconds 字符串化、poll 路径与 Bearer、响应 URL coalesce、resultEphemeral）。验证：`go -C backend test ./internal/protocol/ -run YuxiboxSeedance` <!-- comet-task:3bae58db-0aaa-48c6-addd-a65ca9ac68e6 -->

## 3. 前端渠道预设与能力映射

- [x] 3.1 在 `web/src/stores/use-config-store.ts` 新增 `createYuxiboxChannel()`：Base URL `https://yuxibox.cn`、模型 `seedance2.5-yx-count`、protocol `yuxibox-seedance-video`、`billingMode: "fixed_request"`、unitPriceMicrocredits 0、描述"yuxibox seedance2.5：4–30 秒默认 30、固定 720P、按条计费 4.5 元/条（实际以 yuxibox 控制台与令牌分组倍率为准）"。验证：`bun run build` 通过 <!-- comet-task:296b3313-145d-46d4-973b-d43378590a54 -->
- [x] 3.2 在 `web/src/pages/settings/channel-settings-pane.tsx` 的 addChannel preset 增加 `"yuxibox"` 分支与"添加 yuxibox Seedance"按钮。验证：`bun run build` 通过，设置页出现按钮 <!-- comet-task:5883bcdc-a517-4e95-8057-91e416ac26d6 -->
- [x] 3.3 在 `web/src/lib/model-capabilities.ts` 为 `yuxibox-seedance-video` 协议补能力默认值（时长 4–30 默认 30、八档比例默认 16:9、resolutions ["720P"]、参考上限 30/10/10）。验证：`bun run build` 通过，渠道模型能力卡片档位正确 <!-- comet-task:3d36f574-6a4a-460d-a54e-78ec136ba2c5 -->

## 4. 文档与登记

- [x] 4.1 在渠道协议专题文档（`docs/content/docs/backend/` 对应页面）补充 yuxibox 协议说明（端点、鉴权、档位、计费模式）。验证：`cd docs && bun run types:check` 通过 <!-- comet-task:2101c9ec-c45f-4963-bf70-954ba2a59ef2 -->
- [x] 4.2 在 `docs/content/docs/progress/pending-test.mdx` 登记"yuxibox seedance2.5 渠道待真实 Key 浏览器验收"。验证：文件已更新 <!-- comet-task:820c4f43-edaf-4aed-82e0-f8f1a82caa4b -->

## 5. 集成验证

- [x] 5.1 后端：`go -C backend test ./internal/protocol/ ./internal/generation/ ./internal/app/`（新增用例与既有用例通过）。验证：全绿 <!-- comet-task:714f3d74-33b3-4cb3-8561-c10eaadac1e4 -->
- [x] 5.2 前端构建：`cd web && bun run build`。验证：构建成功无类型错误 <!-- comet-task:f13eedce-4925-4551-94b0-05bcbbf225ac -->
- [x] 5.3 `openspec validate add-yuxibox-seedance-video-channel`（docs 根下执行）。验证：通过 <!-- comet-task:067ba9e1-0386-4281-94fe-e4ed10b14226 -->
