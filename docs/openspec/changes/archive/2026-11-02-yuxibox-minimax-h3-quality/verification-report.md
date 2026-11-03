# 验证报告

## 结论

**PASS（含已归因的范围边界）**

## 验证证据

| 验证项 | 命令 | 结果 | 证据 |
| --- | --- | --- | --- |
| 插件契约测试（新增 4 项） | `go test ./internal/protocol/ -run TestYuxiboxMinimaxH3Quality` | 4/4 通过 | 会话记录 |
| protocol 包全量回归 | `go test ./internal/protocol/ -count=1` | 通过（未破坏 metaso / seedance / 现有插件） | 会话记录 |
| 后端构建 | `go build ./...`（cwd=backend） | exit 0 | `.comet/checks/55b470e2-28ca-45c3-a48b-aef6f942ffb7.log` |
| 验证阶段 | `go test ./internal/protocol/ -count=1`（cwd=backend） | exit 0 | `.comet/checks/4f943692-4fc4-45f6-9e54-1a9f0d63d700.log` |
| 前端构建 | `bun run build` | exit 0（7.84s） | 会话记录 |
| 插件包一致性 | 包内 manifest 与源码 manifest 逐字节相等 | 通过 | `TestYuxiboxMinimaxH3QualityPackage` |

## 契约测试覆盖

1. **请求体逐字段**：`model`、`prompt`、`seconds`（数值化）、`resolution`（小写归一）、`ratio`（缺省 9:16）、`reference_images` / `audios`（按序映射、空数组省略）、`seed`（providerOptions 透传）。
2. **720P 别名**：选择 720P 时请求体 `resolution` 发送 `768p`（`$switch` 归一），已由失败测试反向验证（初版仅 `$lower` 只小写不改写，测试捕获后修复）。
3. **参考视频拒绝**：11 项非法输入子测试中，`reference video present` 明确拒绝；参数表 `videos.mapping = unsupported`。
4. **响应映射**：`queued` → pending、`in_progress` → processing、`completed` → succeeded（`video_url` 优先于 `download_url`）、`failed` 保留上游 message。
5. **下载回退**：`/v1/videos/{taskId}/content`、`Accept: video/mp4`、Bearer 注入。

## 范围边界（已知偏差）

- **时段差价未自动生效**：供应方要求高峰 08:00–24:00 与空闲 00:00–08:00 两档价，但当前价格档模型（`priceTiers` + `vquality` selector）没有时间窗口维度。插件按高峰价配置基础价目（480p 0.040、768p 0.060），空闲档差价需管理后台人工调整。引入时间窗口计费属于跨模块改动（涉及账务与价格模型），已记入 proposal Non-Goals。
- **`input_reference` multipart 未实现**：宿主统一走公网 URL 提交（`requiresPublicMediaUrls=true`），本地文件会先上传为可读地址。
- **`/content` 仅作回退**：正常路径用 `video_url` + `resultEphemeral` 立即持久化；`/content` 作为 URL 全缺失时的兜底。

## 未验证

- **真实上游生成未调用**：未使用真实 Key 提交付费任务，成片质量、实际计费、HEVC 编码播放均由用户验收。
- **管理后台界面验收未进行**：协议下拉可见性、能力档位渲染需真实登录态在 `/admin/channels` 确认。
