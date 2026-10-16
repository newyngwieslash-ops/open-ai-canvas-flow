---
comet_change: github-skill-suite-install
role: technical-design
canonical_spec: openspec
archived-with: 2026-10-16-github-skill-suite-install
status: final
---

# Design Doc：GitHub 技能套装安装

## 背景与目标

`normalizeSkillArchiveRoot`（`backend/internal/skills/skill_packages_archive.go:172`）子目录安装是硬裁剪：只保留 `X/` 前缀文件并压平。套装型仓库（实例 `lixiaoxiao9888-create/manju-laoli-skill`：总控 `short-drama-director/` + 12 个根一级 `sd-*-engine/`）的总控 SKILL.md 以相对路径引用兄弟目录（`` `sd-decision-engine/SKILL.md` ``），裁剪后引用全部断链。

目标：子目录安装时自动纳入被引用的兄弟技能目录（传递闭包），入口契约与读取链路零改动。能力需求与验收场景以 delta spec 为准（`docs/openspec/changes/github-skill-suite-install/specs/skill-package-suite-install/spec.md`），本文不重复需求。

## 实证结论（设计依据）

以下结论全部基于真实仓库归档验证，非推测：

| # | 实证 | 结论 |
| --- | --- | --- |
| 1 | manju 总控引用形态 | 全部为反引号/括号包裹的 `sd-xxx/...` 路径；`/` 若作 token 分隔符则 12 个目录名全部退化为裸 token，`dir/` 前缀匹配失效 → **分隔符表必须不含 `/`** |
| 2 | 裸 token 匹配 | `/` 剥离后 `token == dir` 已覆盖 manju 全部 12 处引用；`dir/` 前缀形态作为补充覆盖 `dir/file` 整 token 场景 |
| 3 | 词边界 | `sd-script-engine-x` ≠ `sd-script-engine` 且不以 `sd-script-engine/` 开头，双形态均不误命中 |
| 4 | 自引用 | `sd-xuanhuan-engine/SKILL.md` 引用自身 → 闭包必须 visited 防环 |
| 5 | 根识别兼容性 | 现循环命中根 SKILL.md 即重置 + break，兄弟 SKILL.md 在任何 map 迭代序下均收敛到单根 `''` → 零改动 |
| 6 | storyboard 同步 | `examples/` 位于根一级、44 文件、无 SKILL.md；总控正文确有 `examples/` 提及 → 双条件准确排除，同步结果与现状一致 |

## 技术方案

### 数据流

```
archiveFromZip(data, subdir)
  └─ raw: map[路径]内容（GitHub 归档已剥外层 wrapper）
      └─ normalizeSkillArchiveRoot(raw, subdir)
          ├─ wrapper 剥离（不变）
          ├─ subdir != "" 分支（改造）：
          │    filtered = X/** 压平到根
          │    suite = suiteSiblingDirs(raw, string(filtered["SKILL.md"]))
          │    for dir in suite: filtered 并入 raw 中 dir/ 前缀文件（原路径）
          │    raw = filtered
          ├─ 根识别（不变，实证 #5 兼容）
          └─ 根内容提升 → files（兄弟目录已是根相对路径，原样通过）
               └─ finalizeSkillArchive：20MB 上限（对含兄弟目录的最终集合生效，零改动）
```

ZIP / Markdown 路径 `subdir == ""`，不进入改造分支，行为不变。

### 新增函数：suiteSiblingDirs

```go
// suiteSiblingDirs 从 raw 归档视图中收集被入口正文引用的兄弟技能目录名集合。
// 纳入条件（双条件）：目录位于仓库根一级、目录内含 SKILL.md、目录名以裸名
// 或 dir/ 前缀形态出现在某个已纳入技能的 SKILL.md 正文 token 中。
// 扫描按引用关系传递直至固定点；visited 防环（实证：仓库存在自引用）。
func suiteSiblingDirs(raw map[string][]byte, entryText string) []string
```

实现要点：

1. **skillDirs 索引**：一次遍历 raw，收集根一级含 `dir/SKILL.md` 的目录名。
2. **token 化**：新建 `suiteRefTokenSeparator`（`unicode.IsSpace` + `,，、。;；:：\|()（）[]【】{}<>"'""''!！?？+*&\``，**不含 `/`**）。不复用 `cloudAgentSkillTokenSeparator`——其表含 `/`，会破坏 `dir/` 形态（实证 #1）。
3. **匹配**：`token == dir || strings.HasPrefix(token, dir+"/")`。精确大小写，不做折叠（GitHub 目录名大小写敏感，实证仓库全小写；YAGNI）。
4. **闭包**：`pending` 队列初始为入口正文；每纳入一个 dir，把 `raw[dir+"/SKILL.md"]` 正文入队；visited 防环；队列空即固定点。
5. **返回**：排序后的目录名切片，保证并入顺序确定（内容哈希稳定）。

### 改造点：normalizeSkillArchiveRoot subdir 分支

在现有 `filtered` 构建（X 压平）之后、`raw = filtered` 赋值之前插入闭包并入。X 内容压平逻辑、报错路径（子目录不存在或为空）均不变。兄弟目录文件以原相对路径并入 `filtered`。

### 防御边界

- X 自身路径为 `dir/sub/...` 深层时，skillDirs 仍按剥壳后 raw 的根一级计算（GitHub 归档 wrapper 已剥离，语义一致）。
- 兄弟目录并入后若包根出现与 X 压平文件同名路径（理论不可能：X 与兄弟目录在仓库内互斥），后并入者不覆盖已有键（并入时检查 `if _, ok := filtered[p]; !ok`）。
- 入口正文为空（X 无 SKILL.md）：闭包返回空集，后续根识别报"缺少 SKILL.md"，行为与现状一致。

## 测试策略

单元（`skill_packages_test.go`）：

1. 套装闭包：总控引 1 兄弟 → 入口提升到根 + 兄弟原路径入包。
2. 传递闭包：A 引 B、B 引 C → 三者入包。
3. 排除：`examples/`（无 SKILL.md）不纳入；未被引用的含 SKILL.md 目录不纳入。
4. 词边界：`sd-script-engine-x/` 不误命中 `sd-script-engine`。
5. 防环：自引用目录不死循环。
6. 回归：既有 `TestArchiveFromZip*` 全部通过；不填 subdir 多 SKILL.md 仍拒绝。

端到端（一次性验证，不入库为常驻测试）：manju 真实归档填 `short-drama-director` 装出 13 个技能目录全量包；storyboard 真实归档同步结果与现状一致。

验证命令：`go test ./internal/skills/`、`go build ./...`。

## 风险与边界

- **漏引**（变量拼接路径）：扫描不到 → 包内缺失，运行时报"技能文件不存在"。与现状一致不劣化。
- **假阳性纳入**：正文提及且恰含 SKILL.md → 语义为作者真实引用，可接受。
- **体积**：闭包计入 20MB 产品上限，超限明确报错（manju 全仓 3.5MB）。

## 不做的事

- 不改 `EntryPath` 契约、`validateSkillPackageSnapshot`、bundle、Agent 卡片路径。
- 不做 Markdown 链接语法解析、不区分引用语境。
- 不做"多技能列表选择"安装体验（不填子目录仍拒绝）。
- 不改前端、数据库、存量已安装技能数据。
