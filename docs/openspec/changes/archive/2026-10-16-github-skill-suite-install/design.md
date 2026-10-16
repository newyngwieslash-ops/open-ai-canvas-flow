# Design：GitHub 技能套装安装支持

## Context

`normalizeSkillArchiveRoot`（`backend/internal/skills/skill_packages_archive.go`）是 Markdown / ZIP / GitHub 三条安装路径共用的归一化函数：外层包装剥离 → subdir 过滤 → SKILL.md 根识别 → 根内容提升到包根。子目录过滤当前是硬裁剪：只保留 `prefix` 命中的文件，套装仓库的兄弟技能目录就此丢失。产品上限 20MB 已收敛到 `finalizeSkillArchive`（对最终文件集合生效），本设计无需再动大小校验。

## Goals / Non-Goals

**Goals:**

- 子目录安装时，纳入"根下一级、含 SKILL.md、被入口（或已纳入技能）正文以 `dir/` 形式引用"的兄弟技能目录，按引用传递闭包。
- 入口仍提升到根 `SKILL.md`，读取链路（snapshot/bundle/Agent 卡片）零改动。

**Non-Goals:**

- 不做"多技能列表选择"安装体验（不填子目录仍拒绝）。
- 不解析 Markdown 链接语法、不区分引用语境（纯文本子串匹配）。
- 不做递归任意深度扫描：闭包只发生在仓库根一级目录之间。
- 不改 ZIP / Markdown 上传行为、不做前端 UI。

## Decisions

1. **双条件纳入（文本引用 + 目录含 SKILL.md）**：入口正文对 `dir/` 的子串命中只是候选，还必须该目录含 SKILL.md 才纳入。单条件文本扫描会误拉 `examples/`（storyboard 的 SKILL.md 明确提到它，24MB），双条件把它挡掉。误纳概率趋近于零，无需更复杂的语法分析。
2. **闭包扫描在内存 map 上进行**：`archiveFromZip` 已把全部条目读进 `raw` map，扫描与纳入都是 map 操作，无二次 IO。实现为：从 `dir → files` 的视图出发，队列式扩展被引用目录集合，直到固定点。
3. **引用判定取"词边界"子串**：正文按空白/标点切 token 后检查 `dir/` 前缀命中（含 `dir/` 自身、`dir/xxx`），避免 `sd-script-engine-x/` 误命中 `sd-script-engine/`。朴素包含匹配在这个场景会出假阳性。
4. **入口提升逻辑不变**：X 的 SKILL.md 压平到包根后，包根存在 `SKILL.md`，现有根识别逻辑自然选择空根，兄弟目录以原路径并入 `files`。不引入新的入口语义。
5. **存量数据不动**：归一化只发生在安装/同步时，已安装技能的 ZIP 与 `skill_files` 不重算；storyboard 的自动同步走新逻辑后因 `examples/` 无 SKILL.md，结果与现状一致。

**备选已否决**：a) "保留完整仓库结构、入口指向 `X/SKILL.md`"——要改 `EntryPath` 契约与 5 处校验，牵动过大；b) "扫描 SKILL.md 中全部 Markdown 链接"——过度设计，套装引用多为行内代码或裸路径，token 匹配已覆盖；c) "打包整个仓库"——体积失控且引入无关内容。

## Risks / Trade-offs

- **漏引**：变量拼接的引用路径扫描不到 → 包内缺失，运行时读取报"技能文件不存在"。与现状一致，不劣化；用户可手装缺失子技能为独立技能兜底。
- **体积**：闭包纳入计入 20MB 产品上限，超限明确报错（manju 全仓 3.5MB，无压力）。
- **假阳性纳入**：正文提及且恰含 SKILL.md 的目录会被拉入。语义上是"作者确实引用了它"，可接受。
