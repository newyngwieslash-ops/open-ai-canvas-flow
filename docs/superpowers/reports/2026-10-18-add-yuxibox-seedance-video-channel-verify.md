# 验证报告：add-yuxibox-seedance-video-channel

- 日期：2026-10-18
- 验证模式：full（规模评估：14 任务 / 1 delta capability / 32 变更文件 > 8 → full）
- 验证分支：feature/20260618/add-yuxibox-seedance-video-channel（base-ref 8f927d53）
- 验证人：Comet verify 阶段（agent 执行，全部结论可回溯到命令输出与文件行号）

## Summary

| 维度 | 状态 |
| --- | --- |
| Completeness | 14/14 任务完成 `[x]`；4/4 需求有实现证据 |
| Correctness | 10/10 场景有实现与测试/验证证据 |
| Coherence | 实现与 open 阶段 design.md 决策及 Design Doc 一致；无矛盾 |

## 逐项检查

### 1. 任务完成（Completeness）

`docs/openspec/changes/add-yuxibox-seedance-video-channel/tasks.md`：14/14 `[x]`（`grep -c "\- \[x\]"` = 14）。稳定任务 ID 已分配（`comet state tasks --assign-ids`），plan 任务映射 30 个 Step 全部带 `comet-task-ref` 且通过 guard。

### 2. 实现匹配 design.md 高层决策

| design.md 决策 | 实现证据 |
| --- | --- |
| D1 声明式插件（零专属宿主代码） | `plugin-packages/generate-catalog.mjs` yuxibox 定义块；后端仅 `registry.go:66-67`（显示名）+ `model_capability_defaults.go:218-228`（能力默认值）两处 case；`git diff 8f927d53...HEAD --name-only -- backend/` 确认无其他后端文件改动 |
| D2 统一 ratio（不发 size） | manifest create body 仅 `ratio: coalesce(aspectRatio, "16:9")`；契约测试断言 `size` 键不存在（`yuxibox_seedance_video_test.go:60`） |
| D3 按条计费 fixed_request | `use-config-store.ts:531-533` `billingMode: "fixed_request"`、描述含 4.5 元/条与倍率说明 |
| 复用声明式宿主（SSRF/恢复合同/下载） | 插件无 cancel/result 声明外的任何宿主代码；轮询/恢复走 `provider_video.go:67-70` 既有适配器路径 |

### 3. 实现匹配 Design Doc（docs/superpowers/specs/2026-10-18-yuxibox-seedance-video-design.md）

- 2.1 插件定义块：manifest 生成验证通过（`node -e` 抽查 provider 字段、7 validations、workflow defaults `{"model":"seedance2.5-yx-count","duration":30,"aspectRatio":"16:9"}`）。
- 2.2 后端注册：`OfficialDeclarativeVideoInterface("yuxibox-seedance-video") == ("yuxibox Seedance", true)`；能力默认值逐字段与 Design Doc 2.2 骨架一致（range 4–30 default 30、七档 ratios default 16:9、resolutions `["720p"]`、上限 30/10/10、promptMaxChars 16000、Operations +reference_to_video/+audio_to_video）。
- 2.3 前端三处：`createYuxiboxChannel()` + preset `"yuxibox"` + 按钮 + `model-capabilities.ts` yuxibox 分支；前后端能力字段逐项对齐（promptMaxChars 16000 / 30-10-10 / duration range 4–30 default 30 / 七档 ratios / `["720p"]`）。
- 2.4 无前端 provider：`git diff` 确认 `web/src/services/api/video.ts`、`video-contracts.ts` 无改动。
- 3 边界语义：seconds 字符串化（测试断言 `body["seconds"] == "30"`）；空素材 omitEmpty（测试断言键不存在）；无取消接口（manifest 无 cancel 声明）。
- 6 风险缓解：完成态字段未实测→多字段 coalesce（测试覆盖 3 种 URL 形态）；纯音频生视频→Operations 开放 audio_to_video 并登记 pending-test 验收项。

### 4. 能力规格场景全部通过（Correctness）

10/10 场景逐项证据：

| 场景 | 实现证据 | 测试/验证证据 |
| --- | --- | --- |
| 管理员一键创建渠道 | use-config-store.ts:531 + channel-settings-pane.tsx:67,189 | bun run build 通过（类型覆盖）；真实点击验收登记 pending-test |
| 模型名单校验 | manifest validation `eq(model, "seedance2.5-yx-count")` | 测试 `model renamed` 拒绝 ✅ |
| 时长档位校验 | manifest validation `4 <= duration <= 30` | 测试 `duration low(3)`/`duration high(31)` 拒绝 ✅ |
| 默认时长 | 能力层 Duration.Default=30（前后端）+ workflow defaults duration:30 | 后端 model_capability_defaults.go:228 + 前端 model-capabilities.ts:491 |
| 画幅映射 | create 模板 `ratio: coalesce(aspectRatio, "16:9")` 单字段 | 测试断言 ratio=adaptive/16:9 且 size 不存在 ✅ |
| 多参考图图生视频 | `reference_images` map 映射 | 测试断言 2 张图按序映射 URL ✅ |
| 参考数量上限 | manifest validations ≤30/≤10/≤10 | 测试 31 图/11 视频/11 音频全部拒绝 ✅ |
| 创建与轮询 | poll `GET /v1/videos/{taskId}` | 测试 ParseCreate queued→pending + BuildPoll 路径断言 ✅ |
| 恢复任务不重复计费 | 宿主既有 resumedProviderRequestID 只查询合同（provider_video.go:231 注释） | 插件层无重复创建代码；声明式宿主原生行为（与 metaso 同构，宿主测试覆盖） |
| 结果不确定语义 | asyncResponse taskId 链提取失败→宿主抛"声明式协议创建请求没有返回任务 ID" | 测试断言 request_id 不成为任务 ID ✅ |

### 5. proposal.md 目标满足

- 管理员可配渠道 ✅（一键预设 + API Key 填写）
- 文生视频与多模态参考生视频 ✅（协议映射 + 契约测试）
- 4–30 秒/比例档位宿主能力层约束 ✅（前后端能力默认值 + manifest validations）
- 复用声明式宿主 ✅（零专属宿主代码）

### 6. delta spec 与 Design Doc 无矛盾

Design Doc 第 6 节明确"Spec Patches: 无"；D2（统一 ratio）是 spec"画幅映射"场景允许范围内的实现选择（场景原文"发送 size: 1280x720（或对应 ratio 档位）"）。Build 阶段未发生增量 spec 修改（handoff_hash 与 open 产物一致路径：tasks.md 因勾选与 ID 分配有变化，proposal/design/specs 未改语义）。无 Spec Drift。

### 7. 关联设计文档可定位

`docs/superpowers/specs/2026-10-18-yuxibox-seedance-video-design.md` 存在且 frontmatter 含 `comet_change: add-yuxibox-seedance-video-channel` / `role: technical-design` / `canonical_spec: openspec`。

## 验证命令记录

| 命令 | 结果 |
| --- | --- |
| `comet check run ... build --local -- go -C backend test ./internal/protocol/ ./internal/generation/` | exit=0（runtime 证据，guard build 14/14 通过） |
| `go -C backend test ./internal/protocol/ -run YuxiboxSeedance -v` | 4 Test + 9 拒绝子用例全部 PASS |
| `cd web && bun run build` | ✓ built（两次：Task 5 后与 S2 修复后） |
| `cd docs && openspec validate add-yuxibox-seedance-video-channel` | valid |
| `node -e` manifest 抽查（provider 身份/validations/defaults） | 全部符合预期 |

## 已知边界（如实记录，非失败）

- `go -C backend test ./internal/app/` 全量有 40 个失败：在 base 8f927d53 干净树上复测同样失败（Windows Temp 目录 Access denied、临时 Redis 未启动、Cloud Agent schema 预算 36137>36000 等），均为基线预存问题，与本变更无关（本变更后端仅 3 个文件、+191 行，不触碰上述测试覆盖的模块）。与 yuxibox 能力默认值相关的 app 专项测试（TestYuxibox/TestMetaso/TestDefaultModelCapability/TestChannelModel 等）全部通过。
- 真实上游行为（完成态响应字段名、纯音频生视频、计费倍率）未实测，已登记 `docs/content/docs/progress/pending-test.mdx` 待用户浏览器验收；验收差异只需改 manifest 响应映射一处（设计文档风险表既定缓解）。
- 代码审查（build 阶段 review_mode: standard，change 级一次）：无 CRITICAL/IMPORTANT；2 条 SUGGESTION——S1（生成管线会抹掉手写增强文档，仓库既有风险非本变更引入）知悉并接受，S2（defaultBaseUrlForChannelInterface 缺条目）已修复并提交。
- `.yingce-plugin` 包因本机无 `zip` 命令用 Node 手写 zip（store 模式）生成：结构经 EOCD/central directory 解析验证有效，且 `TestYuxiboxSeedancePackage` 通过 Go `ParsePluginPackage`（含 CRC 校验）与源码 manifest 逐字节一致性断言。

## Final Assessment

**All checks passed. Ready for archive.**

无 CRITICAL/WARNING 问题；4 需求 10 场景全部有实现与测试证据，实现与 open 阶段设计决策及 Design Doc 一致，代码审查无严重发现。剩余边界均为已登记的待验收项（真实 Key 浏览器验收）。
