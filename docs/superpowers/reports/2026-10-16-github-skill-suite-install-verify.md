# 验证报告：github-skill-suite-install

- 日期：2026-10-16
- 模式：full（scale 评估：11 任务 > 3 阈值）
- 分支：codex/welcome-ui-hallmark（base-ref 9ce50f27...HEAD）
- 变更范围：`backend/internal/skills/` 2 文件（+301/-3），另加 OpenSpec/Comet 产物与 pending-test.mdx 追加

## 验证证据（全部为本次新跑）

| 项 | 命令 | 结果 |
| --- | --- | --- |
| 测试 | `cd backend && go test ./internal/skills/ -count=1` | PASS（含 8 个 ArchiveFromZip 子用例、8 个闭包扫描用例、20MB 上限用例） |
| 构建 | `cd backend && go build ./...` | exit 0 |
| verify 证据 | `comet check run ... verify --local --cwd backend -- go test ./internal/skills/ -count=1` | exit=0（log: `.comet/checks/8726a161-*.log`） |
| build 证据 | `comet check run ... build --local --cwd backend -- go build ./...` | exit=0（policy: `.comet/check-policy.json` v2） |
| go vet | build 阶段审查 subagent 实测 | 干净 |

## 完整性（Completeness）

- tasks.md：11/11 已勾选（显式 `comet-task` ID 注解，计划为 canonical 引用模式）。
- 实现范围与 tasks.md 描述一致：仅 `skill_packages_archive.go`（`suiteRefTokenSeparator`/`suiteSiblingDirs` + subdir 分支闭包并入）与 `skill_packages_test.go`（三组新测试），`skill_packages.go` / `skill_packages_github.go` 零改动（与计划一致）。

## 正确性（Correctness）：delta spec 8 场景逐项

| spec 场景 | 归属测试 | 结果 |
| --- | --- | --- |
| 套装仓库总控安装引用可达 | `TestArchiveFromZipSuiteInstall/suite_closure_keeps_sibling_paths_and_lifts_entry` | PASS |
| 传递闭包 | `.../transitive_closure_bundles_referenced_engines` | PASS |
| 无 SKILL.md 的兄弟目录不纳入 | `.../assets_without_SKILL.md_and_unreferenced_skills_stay_out` | PASS |
| 未被引用的兄弟技能目录不纳入 | 同上（unused-skill 键断言） | PASS |
| 入口提升与唯一性 | `.../entry_directory_itself_is_not_re-bundled`（无 `director/` 前缀键） | PASS |
| 超限明确报错 | `TestArchiveFromZipSuitePackageLimit`（21MB 并入后拒绝，错误信息精确匹配） | PASS |
| 不填子目录仍拒绝多技能 | `TestArchiveFromZipRejectsTraversalAndMultipleSkills/multiple`（既有回归） | PASS |
| 单技能仓库安装不变 | `TestArchiveFromZipNormalizesWrapperAndNestedFiles` + `TestArchiveFromZipIgnoresOversizedUnrelatedAssets`（既有回归） | PASS |

真实仓库端到端（build 阶段一次性验证，产物 `.local/cache/suite-e2e/`）：
- manju-laoli 填 `short-drama-director`：154 文件、12 个 `sd-*-engine` 全量、总字节 3435040 ≤ 20MB。
- storyboard：base-ref worktree 对照 `diff` 为空（28 文件 / 269361 字节逐键一致），`examples/` 零命中。

## 一致性（Coherence）

- Design Doc 六条实证约束逐条落地：分隔符表不含 `/`（`skill_packages_archive.go:425`）、双形态匹配（`:449` 一带）、visited 防环、根识别零改动、ZIP/Markdown 路径不变、不覆盖已有键。
- build 阶段 code review（standard）：无 Critical/Important；Minor #1（裸 token 用例）已补测试并推送（c2c7f7ca）；Minor #2（最坏复杂度 O(token×目录)）记录为已知边界，无需现在修改。
- 无 delta spec 与 Design Doc 矛盾（build 阶段未修改 spec，Spec Patches 为 None）。

## 偏差记录（非阻塞）

1. `docs` 站 `types:check` 无法运行：本仓 `docs/` 仅入库 MDX 内容，Fumadocs 应用不在本仓，脚本不存在。pending-test.mdx 为纯 Markdown 追加，风险可忽略。
2. `pending-test.mdx` 未提交：该文件同时含他人进行中的"欢迎页"小节，按计划与 AGENTS.md 不夹带他人改动；本变更的待测条目随文件留待用户验收后一并提交。
3. comet build/verify 证据按 guard 要求落在仓库根 invocation cwd，通过 `.comet/check-policy.json`（v2，commands+cwd=backend）声明；root 直跑 `go build` 因本机 GOPATH 环境残留失败，与本次改动无关。

## 结论

无 CRITICAL、无 WARNING 级未决项。8/8 spec 场景有测试归属且全绿，真实仓库端到端对照一致，设计约束逐条落地。**可以归档。**
