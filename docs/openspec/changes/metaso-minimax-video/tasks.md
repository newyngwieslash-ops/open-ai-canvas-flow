# Tasks

## 1. 插件包与生成管线

- [x] 1.1 在 `plugin-packages/generate-catalog.mjs` 新增 `metaso-minimax-video` 插件定义：provider（baseUrl `https://metaso.cn/api/minimax`、bearer 鉴权、`requiresPublicMediaUrls`、六档固定比例 + 480P/768P/1080P/2K 校验、4–15 秒校验、content[] 组装映射、查询/响应 `$coalesce` 映射）与 workflow（默认值 model `MiniMax-H3`、resolution `768P`、duration 5、ratio `16:9`）。验证：`node plugin-packages/generate-catalog.mjs` 生成 `manifest.json` 且包含新插件 <!-- comet-task:83e36e14170b47b0a62c9dc5e825aebf -->
- [x] 1.2 编写 `plugin-packages/metaso-minimax-video/README.md` 与 `docs/interface.md`（协议身份、配置字段 apiKey、统一字段映射、请求示例、响应与错误、兼容边界），运行 `node plugin-packages/embed-documentation.mjs metaso-minimax-video` 回填 manifest documentation。验证：manifest documentation 包含两份文档全文 <!-- comet-task:5f43f14327fe466fa7ce87a85e7be247 -->
- [x] 1.3 运行 `sh plugin-packages/build-packages.sh` 生成 `metaso-minimax-video.yingce-plugin`。验证：仓库根 `plugin-packages/` 出现该包且 `unzip -l` 含 manifest.json、README.md、docs/interface.md <!-- comet-task:d21364a738a340d38c13aad4f13ef121 -->

## 2. 后端注册与能力默认值

- [x] 2.1 在 `backend/internal/generation/registry.go` 的 `OfficialDeclarativeVideoInterface` 注册 `metaso-minimax-video` → 显示名"密塔 H3"。验证：`go build ./...` 通过 <!-- comet-task:3b29ec5b16694d0896f11a9d1493cb89 -->
- [x] 2.2 在 `backend/internal/app/model_capability_defaults.go` 为 `metaso-minimax-video` 添加能力默认值：Operations 含 text_to_video/image_to_video/reference_to_video，Resolutions `["480P","768P","1080P","2K"]` 默认 768P，Duration 4–15 默认 5，Ratios 六档默认 16:9，参考素材上限（图片 9、视频 3、音频 3）。验证：`go build ./...` 通过 <!-- comet-task:8205ba179722454baf2f26403e56e6f0 -->
- [x] 2.3 新增 `backend/internal/protocol/metaso_minimax_video_test.go`：仿 `fisher_h3_video_test.go` 用 `officialPackageAdapter` 断言插件包契约（模型固定 MiniMax-H3、档位校验、三种模式 content[] 组装、创建/查询路径与 Bearer、响应映射、resultEphemeral）。验证：`cd backend && go test ./internal/protocol/ -run MetasoMinimax` <!-- comet-task:1cb0dccc9be140c2bcb41b4c635e347e -->

## 3. 前端 provider 与配置

- [x] 3.1 新增 `web/src/services/api/video-provider-metaso.ts`：create（content[] 组装、task_id 校验与"结果不确定"错误语义）+ poll（状态归一、成功取回视频 URL、失败呈现上游错误）。验证：`bun run build` 通过 <!-- comet-task:cf23cc552a674d4bb890dd80a919b871 -->
- [x] 3.2 在 `web/src/services/api/video.ts` 增加 `metaso-minimax-video` 的 create/poll 分发行（provider 标识 `metaso-minimax`）。验证：代码审查 + `bun run build` 通过 <!-- comet-task:ce73eef836c3425d91c60a3dfbc90d17 -->
- [x] 3.3 在 `web/src/lib/model-capabilities.ts` 与 `web/src/stores/use-config-store.ts` 添加密塔能力映射与默认 Base URL `https://metaso.cn/api/minimax`。验证：`bun run build` 通过，渠道选择器出现"密塔 H3" <!-- comet-task:f6456828f8224f01a8e37e51bb981687 -->
- [x] 3.4 新增前端 payload 构建测试（三种模式 content[]、档位拒绝、首帧角色），仿现有 provider 测试模式。验证：`bun test` 相关用例通过 <!-- comet-task:b01ecdb7d16145dead7d0b42d7f7de53 -->

## 4. 文档与登记

- [x] 4.1 在 `docs/content/docs/backend/http-api.mdx` 或渠道协议专题文档补充密塔协议说明（端点、鉴权、档位）。验证：`cd docs && bun run types:check` 通过 <!-- comet-task:a6b0813f24df4682945e96275c6a3af3 -->
- [x] 4.2 在 `docs/content/docs/progress/pending-test.mdx` 登记"密塔 H3 渠道待真实 Key 浏览器验收"。验证：文件已更新并提交 <!-- comet-task:e0a4ae638bbf4b619f7fc56b523546fc -->

## 5. 集成验证

- [x] 5.1 后端全量：`cd backend && go test ./...`（关注 protocol 与 app 包）。验证：新增用例与既有用例全部通过 <!-- comet-task:d5bff2af85704366886ff9e752a60e71 -->
- [x] 5.2 前端构建：`cd web && bun run build`。验证：构建成功无类型错误 <!-- comet-task:33941c9132994153a011c2059410941c -->
