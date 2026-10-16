# Proposal：GitHub 技能套装安装支持

## Why

技能仓库存在"1 总控 + N 兄弟子技能"的套装形态（实例：`lixiaoxiao9888-create/manju-laoli-skill`，总控 `short-drama-director/` 引用兄弟目录 `sd-decision-engine/` 等 12 个子技能）。当前 GitHub 安装填子目录 X 时，`normalizeSkillArchiveRoot` 只保留 `X/` 前缀内容并把层级压平到根，兄弟技能目录被全部裁掉——总控 SKILL.md 内的相对引用（如 `sd-decision-engine/SKILL.md`）在运行时全部断链，技能装出来是残废。`kevinchin12/storyboard-director` 因把子技能嵌在自己目录内才侥幸可用。

## What Changes

- GitHub 安装（含 ZIP 压平入口共用同一归一化函数）填子目录 X 时，除 `X/` 内容外，将仓库内**其他含 SKILL.md 且被入口正文以 `dir/` 形式引用**的兄弟技能目录一并纳入最终包（传递闭包：新纳入技能的 SKILL.md 再引用其他兄弟目录则继续纳入）。
- 入口契约不变：X 的 SKILL.md 仍提升到根 `SKILL.md`，`EntryPath == "SKILL.md"`、`validateSkillPackageSnapshot`、bundle、Agent 卡片路径全链路零改动。
- 兄弟技能目录的内部文件保持**原相对路径**（如 `sd-decision-engine/SKILL.md`、`sd-script-engine/references/...`），保证总控相对引用可达。
- 不填子目录的 GitHub 安装行为不变：多 SKILL.md 仍拒绝（本次不改"列表选择"体验）。
- ZIP / Markdown 上传路径不受影响（同一函数但 `subdir == ""` 分支行为不变）。

## Capabilities

### New Capabilities

- `skill-package-suite-install`: 技能包套装安装——GitHub 子目录安装时自动纳入被引用的兄弟技能目录，保证套装型仓库的引用完整性。

### Modified Capabilities

（无既有 spec 需修改）

## Impact

- 后端 `backend/internal/skills/skill_packages_archive.go`：`normalizeSkillArchiveRoot` 子目录过滤分支 + 新增兄弟目录传递闭包扫描。
- 测试 `backend/internal/skills/skill_packages_test.go`：新增套装归一化用例（闭包纳入、无 SKILL.md 的兄弟目录不纳入、20MB 对最终包生效、不填子目录仍拒绝）。
- 不动前端、不动数据库、不动读取链路；已安装技能的存量 ZIP 包不重算。
- 已知边界：SKILL.md 用变量拼接引用路径时扫描不到（漏引），与现状一致、不劣化；纳入内容计入 20MB 产品上限，超限明确报错。
