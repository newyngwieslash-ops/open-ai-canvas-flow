# Tasks

## 1. 归一化实现

- [x] 1.1 在 `skill_packages_archive.go` 新增兄弟技能目录闭包扫描：从 subdir 归一化后的 raw 视图出发，token 化入口（及已纳入技能）正文，收集"根下一级 + 含 SKILL.md + 被引用"目录直至固定点 <!-- comet-task:1-1 -->
- [x] 1.2 改造 `normalizeSkillArchiveRoot` 的 subdir 分支：X 内容压平到根后并入闭包目录文件，保持兄弟目录原相对路径；不填 subdir 分支行为不变 <!-- comet-task:1-2 -->
- [x] 1.3 确认 `finalizeSkillArchive` 的 20MB / 单文件 8MB / 512 文件上限对并入后的最终集合生效（预期零改动，验证即可） <!-- comet-task:1-3 -->

## 2. 测试

- [x] 2.1 套装闭包用例：总控引用 1 个兄弟技能 → 两目录内容均入包、入口提升到根、兄弟路径保留 <!-- comet-task:2-1 -->
- [x] 2.2 传递闭包用例：A 引 B、B 引 C → 三者均入包 <!-- comet-task:2-2 -->
- [x] 2.3 排除用例：`examples/`（无 SKILL.md）不纳入；未被引用的含 SKILL.md 目录不纳入；词边界（`sd-script-engine-x/` 不误命中） <!-- comet-task:2-3 -->
- [x] 2.4 回归用例：不填 subdir 多 SKILL.md 仍拒绝；单技能 ZIP 归一化结果与现状一致（既有 `TestArchiveFromZip*` 全部保持通过） <!-- comet-task:2-4 -->
- [x] 2.5 真实仓库端到端：manju-laoli 归档填 `short-drama-director` 装出含 13 个技能目录的包；storyboard 归档同步结果与现状一致 <!-- comet-task:2-5 -->

## 3. 验证与交付

- [x] 3.1 `go test ./internal/skills/` 与 `go build ./...` 通过 <!-- comet-task:3-1 -->
- [x] 3.2 更新 `docs/content/docs/progress/pending-test.mdx`（用户浏览器验证 manju-laoli GitHub 安装） <!-- comet-task:3-2 -->
- [x] 3.3 提交并推送 origin 现有分支 <!-- comet-task:3-3 -->
