# 验证报告：sync-upstream-161（上游 v1.6.1 融合）

- 日期：2026-10-07
- 范围：b355df61..ed95133e（合并提交 ab5549a2，parents 5f17fc92 + 0b1d5073）
- verify_mode：full

## 检查项（openspec-verify-change 三维）

| 维度 | 结果 | 证据 |
| --- | --- | --- |
| Completeness 任务 | 14/14 勾选 | tasks.md 全 [x]；计划步骤全勾（ed95133e） |
| Completeness 需求 | 4/4 delta spec 需求落实 | 合并提交拓扑（Task 9 四祖先 OK）；工具态零污染；fetch-only upstream；推送前置条件齐备 |
| Correctness | 10 文件裁决三方对照无丢失（审查确认）；交织区拼装逐路径推演无互斥 | 审查报告 2 Important 已修复（15c4e791）；bun test user-data-asset-sync 5/5、welcome-title LF 检出 2/2 |
| Coherence | Design Doc 裁决表与实际一致；welcome 裁决按审查发现修正（seoTitle 优先） | 15c4e791 diff（忽略 CR 后 +12/−8） |

## 构建与测试证据（fresh）

- go test ./...：全量仅 2 失败，均已归因非合并引入：
  1. TestGmailMailboxRegistrationConcurrent：上游 19841cb9 新增，Windows TempDir 文件锁清理（users.db 被占用），环境性问题；merge-base 与 main 无此测试。
  2. TestAppearanceUpdateAnnouncementMediaRangeAndDisabledAccess：**在纯上游 0b1d5073 上同样失败**（独立 worktree 复现），main 通过 → 上游 v1.6.1 预存失败，非合并引入。失败点：fs.PathError 500，涉及公告媒体 Range 读取链。
- cd web && bun run build：成功（tsc+vite，built in 6s）。
- 迁移实证：真实数据目录启动后 schema_migrations MAX(version)=47（upload_reservations/skill_curation/skill_curation_roots）；SkillCuration 迁移专项测试 3/3 过。
- 完整性：merge-base --is-ancestor main/0b1d5073/c98c9b39/853c7d2c 全 OK；acknowledgeRemoteAssets 与 Midjourney 配置抽查在位。

## 审查（review_mode: standard）

- 构建阶段最终审查（requesting-code-review 子代理）：无 Critical；2 Important 已修复：
  1. welcome 标题回退上游 SEO 修复 → seoTitle 优先、品牌后缀兜底。
  2. 冲突聚合 throw 早于画布推送、文案不符 → 后移至函数收尾。
- 修复提交曾引入 CRLF 行尾污染（编辑器改写），已归一 LF 后 amend（15c4e791，blob 级 0 CR）。

## 遗留（非阻塞）

- 上游预存失败测试 2 项（Gmail 并发/公告 Range），建议后续 change 跟进或待上游修复。
- Minor：本地增量校验块无 sessionEpoch 防护；交织区组合路径（无基线 pre-pass × entity）无专项测试。

## 结论

无 CRITICAL；WARNING 均为上游预存问题且与本合并无关。**通过，可归档。**
