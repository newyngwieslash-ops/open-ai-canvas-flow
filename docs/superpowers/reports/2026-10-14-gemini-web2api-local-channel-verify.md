# 验证报告：gemini-web2api-local-channel

- 日期：2026-10-14
- 模式：full（scale 评估 full；无 delta spec，重点覆盖运行时配置一致性）
- 变更类型：纯运行时配置（无代码 diff、无 spec 变更，`skip_specs: true`）
- 工作流：tweak（Comet classic，zh-CN）

## 验证检查结果

| # | 检查项 | 结果 | 证据 |
|---|--------|------|------|
| 1 | tasks.md 全部勾选（13/13） | PASS | `rg -c "- [ ]"` = 0 未勾选 |
| 2 | 改动与任务描述一致 | PASS | 本 change 仅新增产物目录 `docs/openspec/changes/gemini-web2api-local-channel/`（gitignore 下按历史惯例归档时强制加入）与 dev DB 运行时配置；base_ref 至 HEAD 的 19 个文件全部属于其它 change / 用户改动（fix-canvas-sync、user-data-sync、cloud-agent 等），未混入本 change |
| 3 | 构建通过 | PASS | `go -C backend build ./cmd/server ./internal/...` exit=0（build 阶段 guard 证据 48aef612）；代码零改动，构建状态不受配置影响 |
| 4 | 相关测试 / 运行时验收 | PASS | 见下"运行时验收证据" |
| 5 | 无安全问题 | PASS | 上游 API key 只存在于 dev DB（加密列），change 产物与报告 grep 无泄露；未修改鉴权/SSRF 代码；私网放行依赖既有 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS`（restart-local.ps1:355 已含 127.0.0.1） |
| 6 | 代码审查 | SKIP | `review_mode: off`（tweak 配置态）；本 change 无代码 diff，无可审实现 |

## 运行时验收证据（全新读取，2026-10-14 23:5x）

- 渠道：`CHANNEL_000004` `local-gemini-web2api` baseUrl=`http://127.0.0.1:8083/v1` enabled=1 未删除
- 图片模型：`MODEL_000050` `gemini-nano-banana` capability=image protocol=`openai-image` providerModelKey=`gemini-image` enabled=1
- 视频模型：`MODEL_000053` `gemini-veo-web` capability=video protocol=`newapi` providerModelKey=`gemini-video` enabled=1
- 前台目录 `/api/model-catalog`：两模型 `available=true`，capabilityConfig 禁用项（蒙版/参考图/透明底/参考视频/参考音频，operations=[text_to_video]）正确下发
- **图片验收任务** `4689c3684c6981f9ea605ee35be2a9e6`（prompt=白底蓝色陶瓷杯）：status=succeeded，产出 804,145 字节 PNG（PNG magic 校验通过），resourceId=804f0417b2f25020a8f260842198ca27，经 `/api/resources/.../file` 下载验证 200
- **视频验收任务** `3a4925b8e3c4d4efc0d32207d01c044b`：建单 200（queued）→ 后端进入 poll（provider_request_id=video_0bcb7d07daa3e947）→ 上游 Google 账号缺 Veo 产物能力返回 failed，错误如实透传。符合 YINGCE_HANDOFF.md 完成定义："建单 200 即可，完成作为加分"

## 过程中的缺陷与修复（均已闭环）

1. 图片模型首配 `quality.supported=false` 导致 openai-image 线路无法自动补齐像素尺寸换算表（fail-closed 报"当前线路缺少像素尺寸换算表"）。修复：PATCH capabilityConfig，显式声明 parameters（size 仅像素值 + quality 档位 auto/low/medium/high，上游忽略）。
2. 上游 gemini-web2api-go 图片/视频产物抽取在 22:57–23:44 间歇性 502（"media generation succeeded but artifact retrieval failed"，上游侧 Google 响应问题，非影策配置问题）。23:44 后自行恢复，重发验收任务成功。

## 结论

PASS。全部 13 个任务完成，无 CRITICAL/IMPORTANT 未决问题；handoff 完成 C-D 段定义（渠道 id、模型名、成功任务 id 均已记录）。
