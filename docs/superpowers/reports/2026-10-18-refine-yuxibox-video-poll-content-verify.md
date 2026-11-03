# 验证报告：refine-yuxibox-video-poll-content

- 日期：2026-10-18
- 验证模式：full（含 delta spec）
- 分支：sync/upstream-1.6.3（isolation: current）
- 工作流：tweak（build_mode: direct）

## Summary

| 维度 | 状态 |
| --- | --- |
| Completeness | 8/8 任务完成 `[x]`；1/1 MODIFIED 需求落地 |
| Correctness | 6/6 delta spec 场景有实现与测试证据 |
| Coherence | 与 design.md 决策一致；无宿主代码改动 |

## 逐项检查

1. **任务完成**：tasks.md 8/8 `[x]`，稳定 ID 已分配（`comet state tasks --assign-ids`），plan 同步通过。
2. **实现匹配 design.md**：
   - download_url 插入 coalesce 链第二位（`generate-catalog.mjs` yuxibox 块；manifest `response.videos` 实测含 `response.download_url`/`downloadUrl` 在 video_url 之后）。
   - `result: GET /v1/videos/{{taskId}}/content`（Accept video/mp4）已声明（manifest 实测）。
   - 状态归一未改（宿主 `normalizeStatus` 覆盖 queued/in_progress/completed/failed，零宿主改动确认）。
3. **delta spec 场景验证**（6/6）：
   - 创建与轮询 → `TestYuxiboxSeedancePollAndResponse` poll 路径断言 ✅
   - 处理中状态归一 → `{"status":"in_progress","progress":50}` → StatusProcessing ✅
   - 完成态 URL 提取 → `completed` + `video_url`/`download_url`/仅 download_url 三种形态命中 ✅
   - 内容端点兜底下载 → `ResultCapability.ResultAvailable()` + `BuildResult` 路径 `/v1/videos/task-123/content` + Accept video/mp4 + bearer ✅
   - 恢复不重复计费 → 插件层无创建侧变更（宿主既有恢复合同，yuxibox 主变更已验证）✅
   - 结果不确定语义 → `request_id` 不成为任务 ID 断言保持 ✅
4. **proposal 目标**：消除单一 video_url 字段依赖 ✅；文档状态词/HEVC 边界补齐 ✅。
5. **无矛盾**：delta spec 仅为 MODIFIED「异步任务查询与结果归一」，design.md 决策 1-3 与实现逐条对应；无 Spec Drift。
6. **关联文档**：delta spec 位于 `specs/yuxibox-seedance-video-provider/spec.md`，与主变更 capability 路径一致。

## 验证命令记录

| 命令 | 结果 |
| --- | --- |
| `comet check run ... build --local -- go -C backend test ./internal/protocol/` | exit=0（runtime 证据） |
| `comet check run ... verify --local -- go -C backend test ./internal/protocol/ -run YuxiboxSeedance` | exit=0（runtime 证据） |
| `go -C backend test ./internal/protocol/ -run YuxiboxSeedance -v` | 4 Test 全 PASS（含 in_progress 归一、download_url 提取、result 端点断言） |
| `cd docs && openspec validate refine-yuxibox-video-poll-content` | valid |
| manifest 实测 | videos 链 8 字段、result 端点就位 |
| 包完整性 | 重建 `.yingce-plugin`（Node zip store），manifest 与源码一致（TestYuxiboxSeedancePackage 逐字节断言 PASS） |

## 已知边界（非失败）

- content 兜底端点的真实触发需上游仅返回无 URL 成功态，未实测（登记 pending-test 观察项）；实现与 openai-videos 官方插件同构，风险低。
- HEVC 编码成片在画布播放器的兼容性待用户观察（登记 pending-test）。
- 全量 protocol 包 88 个重新生成产生的无关漂移已按 yuxibox 主变更同款方式还原，仅保留 yuxibox 相关改动（`git status` 核对）。

## Final Assessment

**All checks passed. Ready for archive.**
