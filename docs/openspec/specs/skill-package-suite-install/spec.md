# skill-package-suite-install Specification

## Purpose
定义 GitHub 技能安装对"套装型仓库"（1 总控 + N 兄弟子技能）的归一化行为：子目录安装时自动纳入被引用的兄弟技能目录，保证总控技能的相对引用在安装包内可达。

## Requirements

### Requirement: 子目录安装纳入被引用的兄弟技能目录

GitHub 技能安装填子目录 X 时，系统 MUST 在保留 `X/` 内容（压平到包根）的同时，将仓库内满足以下全部条件的其他目录的完整内容纳入最终技能包，路径保持仓库内相对路径不变：

1. 该目录直接位于仓库根下（一级目录）；
2. 该目录内包含 SKILL.md 文件；
3. 目录名以 `名称/` 形式出现在 X 的 SKILL.md 正文（或已纳入技能的 SKILL.md 正文）中。

纳入过程按引用关系传递：新纳入的技能目录若再引用其他满足条件的兄弟目录，继续纳入，直到无新增。

#### Scenario: 套装仓库总控安装引用可达

- **WHEN** 用户以仓库 `R` 的子目录 `director` 安装，`director/SKILL.md` 正文引用了根下一级目录 `engine-a/`，且 `engine-a/SKILL.md` 存在
- **THEN** 安装成功，最终包内同时含 `SKILL.md`（来自 `director`）、`engine-a/SKILL.md` 及 `engine-a/` 的其他文件

#### Scenario: 传递闭包

- **WHEN** `director` 引用 `engine-a`，`engine-a/SKILL.md` 又引用 `engine-b`，三者目录都含 SKILL.md
- **THEN** 最终包内含 `engine-a/` 与 `engine-b/` 两个目录的完整内容

#### Scenario: 无 SKILL.md 的兄弟目录不纳入

- **WHEN** 入口 SKILL.md 正文提到某根下一级目录 `examples/`，但 `examples/` 内没有 SKILL.md
- **THEN** `examples/` 不被纳入最终包

#### Scenario: 未被引用的兄弟技能目录不纳入

- **WHEN** 仓库根下另有目录 `unused-skill/` 含 SKILL.md，但入口及已纳入技能的正文从未提及它
- **THEN** `unused-skill/` 不被纳入最终包

### Requirement: 入口契约保持不变

套装安装产出的技能包入口 MUST 为根级 `SKILL.md`（来自子目录 X），包内不得再出现其他路径层级上会被识别为入口的歧义；已有校验、文件浏览、打包下载与 Agent 渐进披露读取对套装包行为与单技能包一致。

#### Scenario: 入口提升与唯一性

- **WHEN** 子目录 `director/SKILL.md` 被安装为套装包
- **THEN** 包根存在唯一 `SKILL.md`，内容与 `director/SKILL.md` 一致；`director/SKILL.md` 不在包内重复保留

### Requirement: 产品上限对最终包生效

套装安装纳入兄弟目录后，最终包的全部文件 MUST 受既有产品上限约束：总内容不超过 20MB、单文件不超过 8MB、文件数不超过 512。

#### Scenario: 超限明确报错

- **WHEN** 纳入闭包后的最终包总内容超过 20MB
- **THEN** 安装失败并返回明确的大小超限错误，不静默截断

### Requirement: 既有安装路径行为不变

不填子目录的 GitHub 安装、ZIP 上传、Markdown 上传的归一化行为 MUST 保持现状：不填子目录时仓库含多个 SKILL.md 仍被拒绝；ZIP 上传单根技能归一化结果与现状一致。

#### Scenario: 不填子目录仍拒绝多技能

- **WHEN** 用户不填子目录安装含 13 个 SKILL.md 的仓库
- **THEN** 安装失败，错误信息提示指定单个技能目录

#### Scenario: 单技能仓库安装不变

- **WHEN** 用户填子目录安装一个不引用任何兄弟目录的普通技能
- **THEN** 最终包内容与既有行为完全一致（压平为单技能包）
