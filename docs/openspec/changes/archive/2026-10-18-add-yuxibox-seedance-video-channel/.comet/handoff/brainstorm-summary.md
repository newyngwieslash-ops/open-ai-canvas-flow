# Brainstorm Summary

- Change: add-yuxibox-seedance-video-channel
- Date: 2026-10-18

## Confirmed Technical Approach

用户已确认三项深度设计决策（2026-10-18 AskUserQuestion）：

1. **接入方案：A 声明式插件**（与密塔 H3 / Fisher-H3 同构）。新增插件 `yuxibox-seedance-video`：`plugin-packages/generate-catalog.mjs` 定义 provider/workflow/validations/create/poll/response → `plugin-packages/yuxibox-seedance-video/`（manifest.json + README.md + docs/interface.md）→ `embed-documentation.mjs` 回填 → `build-packages.sh` 构建 `.yingce-plugin` 包。后端仅两处注册：`generation/registry.go`（官方声明式视频接口清单 + 显示名"yuxibox Seedance"）、`app/model_capability_defaults.go`（能力默认值）。无专属宿主代码；任务调度、轮询、SSRF、下载、恢复合同全部走声明式协议宿主（provider_protocol.go 既有通道）。

2. **画幅策略：统一 ratio**。能力层 Ratios 暴露上游 ratio 七档全集 `["adaptive","16:9","9:16","1:1","4:3","3:4","21:9"]`（默认 16:9）；插件 create 模板把 `aspectRatio` 单字段映射为上游 `ratio`，不发送 `size`。单映射分支天然满足上游"size 与 ratio 不可同发"约束。分辨率固定 720P，Resolutions 仅 `["720p"]`，不暴露分辨率选择。

3. **计费建模：按条计费**。渠道预设 `billingMode: "fixed_request"`，模型描述注明"基础价 4.5 元/条，各时长均按一条视频计费；实际以 yuxibox 控制台与令牌分组倍率为准"；unitPriceMicrocredits 留 0（分组倍率影响最终费用，不预设死价格）。

其余已定要素（沿 open 阶段 design.md 框架）：

- 创建：`POST https://yuxibox.cn/v1/videos`，JSON 体：`model`（精确 `seedance2.5-yx-count`）、`prompt`（1–16000 字符）、`seconds`（4–30 整数，默认 30，按上游示例发字符串）、`ratio`、`reference_images[]`（≤30）、`videos[]`（≤10）、`audios[]`（≤10）。
- 查询：`GET /v1/videos/{taskId}`；响应 taskId 取 `id`/`task_id`/`taskId`，status 归一走宿主统一语义，视频 URL 按 `url`/`video_url`/`videoUrl`/`result_url`/`output.url`/`data.*` 多字段 coalesce 提取；`resultEphemeral: true` 触发宿主立即下载持久化。
- 参考素材 `requiresPublicMediaUrls: true`，HTTPS/data URL 由宿主既有解析链路提供；不发送 multipart `input_reference`。
- 恢复合同：宿主已有任务 ID 只查询不重建（对齐上游"不要为同一视频重复 POST"），声明式宿主原生支持，无需插件层代码。
- 前端：`use-config-store.ts` 新增 `createYuxiboxChannel()`；`channel-settings-pane.tsx` 新增"添加 yuxibox Seedance"按钮（preset 分支扩展 `"yuxibox"`）；`model-capabilities.ts` 按协议名补能力默认值。走声明式协议（后端执行），前端无需新增 video provider 分发行。
- 时长建模：能力层 Duration range 4–30 step 1 默认 30（避免 27 值枚举数组）。

## Key Trade-offs and Risks

- **真实上游未实测**：文档示例响应只有 queued 态占位（id/object/model/status/seconds/size），完成态字段名未给出；响应 URL 提取按 /v1/videos 生态惯例多字段 coalesce 兜底，真实 Key 验收若字段不同只需改 manifest 响应映射（一处）。已列入 pending-test。
- **"请求规格影响费用"表述模糊**：按条计费建模可能低估某些规格的实际扣费；描述中已注明以控制台为准，验收时核对账单。
- **720P 固定**：能力层锁死单档，未来上游开放更高分辨率需更新插件与能力默认值（预期演进成本可控）。
- **adaptive 档**：上游 ratio 支持 adaptive，能力层包含该档（与 UI "自适应"选项对齐）；上游文档未说明 adaptive 与参考图的交互，默认值仍为 16:9，不把 adaptive 设为默认。

## Testing Strategy

- 后端插件契约测试 `backend/internal/protocol/yuxibox_seedance_video_test.go`（仿 `metaso_minimax_video_test.go`）：
  - 包完整性：`.yingce-plugin` 内 manifest 与源码目录 manifest 逐字节一致；身份（id/baseUrl/bearer/requiresPublicMediaUrls）、workflow defaults（model/seconds 30/ratio 16:9）断言。
  - create 组装：JSON 体含 model/prompt/seconds（字符串化）/ratio；images→reference_images、videos→videos、audios→audios 数组映射。
  - 档位拒绝：model 非 seedance2.5-yx-count、prompt 空或超 16000、seconds 3/31、ratio 非七档、31 图、11 视频、11 音频均须在校验层报错。
  - poll：GET /v1/videos/{taskId} 路径与 Bearer。
  - 响应：taskId/status/videos 多字段 coalesce、resultEphemeral。
- 前端：`bun run build` 类型检查覆盖 `createYuxiboxChannel` 与能力映射；能力映射断言（时长 4–30 默认 30、八档比例、resolutions ["720P"]、上限 30/10/10）。
- 集成：`go -C backend test ./internal/protocol/ ./internal/generation/ ./internal/app/` + `cd web && bun run build` + `openspec validate`。
- 真实 Key 浏览器验收登记 pending-test.mdx，由用户执行。

## Spec Patches

None（open 阶段 delta spec 的 4 条需求 + 12 场景已覆盖深度设计确认的全部行为；画幅统一走 ratio 的决策与 spec "画幅映射"场景一致——场景允许"size 或对应 ratio 档位"，实现选择 ratio 分支不改变规格行为）。
