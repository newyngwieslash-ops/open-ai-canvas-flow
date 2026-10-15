# Task1 fresh fix 报告

DONE。基于 `04321da0c12f840b121ba1bec9b0ead5b2f2eaf9`，分支 `codex/welcome-ui-hallmark`，standard 第 1/1 轮。实现 commit hash：`49981eea1708ef6fa6552ca07bab8bccc0493445`；本报告另作文档提交。

## 实现与范围

- `scripts/audit-product-brand.py`：JSON 字符串解码后通过 `json.dumps(..., ensure_ascii=False)` 保留引号边界，URL 不再吞掉相邻作者字段。普通文件和 ZIP 条目共用修复；发现仍保留物理行号和原始文本，双重转义仍不当作语义品牌。
- 同一脚本：为 ignored docs 的 `os.walk` 增加 `onerror`，将枚举错误记为带目录路径的 product，reason 为 `audit error; coverage incomplete`，使 CLI 退出 1。
- 同一脚本：为服务名、镜像路径、环境变量增加前后边界，为插件扩展名增加结束边界。合法 `sample.yingce-plugin` 仍是扩展名例外；不豁免相似的展示值。
- `.superpowers/sdd/2026-10-15-product-brand-identity/task-1-fix-report.md`：本报告。未改 brand-inventory、产品 UI、素材、计划、任务或 checkpoint；未运行 Comet。未添加依赖。

## 复现与根因

先完整读取 brief、review report、实现报告，以及 TDD、systematic-debugging 技能与测试编写参考。只检查指定扫描器，没有全库探索。

通过 PowerShell here-string 输入 `python -`，在临时 Git 仓库调用真实 `audit_root`：紧凑 JSON 的 product 数为 0；三个相似技术展示值均为 exception。根因分别是解码去掉引号、无完整 token 边界。原始输出：`.local/cache/task-1-fix-reproduce.log`。

文档故障注入首次未命中：临时根路径未 resolve，与被测函数规范化后的路径不同。先定位此测试设置问题，统一 resolve 后，Git 输入仅 `.gitignore`、scandir 拒绝 docs、发现数组 `[]`，CLI 门禁会通过。根因是 `os.walk` 默认静默忽略枚举错误。确认输出：`.local/cache/task-1-fix-reproduce-docs.log`。没有以首次未命中的结果作为覆盖证据。

## 实际 RED / GREEN

每项先只添加 self-test fixture / 断言，再运行同一命令观察失败，之后才修改对应生产代码：

```powershell
python scripts/audit-product-brand.py --self-test 2>&1 | Tee-Object -FilePath '.local/cache/task-1-fix-red-1.log'
```

每次运行均立即读取 `$LASTEXITCODE`，将 `exit=<code>` 附到同一日志并按原退出码退出；其余五次仅将文件名换为 green-1、red-2、green-2、red-3、green-3。

| 项目 | RED 实际断言输出（退出 1） | 最小修复后 GREEN |
| --- | --- | --- |
| 紧凑 JSON | `AssertionError: compact JSON URL must not hide escaped author: compact.json` | 退出 0；普通文件及 ZIP 的作者发现均保留原文和行 1 |
| ignored docs 枚举 | `AssertionError: ignored docs enumeration error must fail CLI with a coverage finding` | 退出 0；临时仓库真实 CLI 在注入 PermissionError 后输出目录覆盖错误并退出 1 |
| 技术 token 边界 | `AssertionError: technical exception must not hide prefixed/suffixed display values: line 7` | 退出 0；6 个合法 token、10 个前后缀反例及既有 self-test 全部通过 |

三次 GREEN 的原始输出均为：

```text
self-test: PASS (source, names, ZIP, exact exceptions, docs, exclusions, non-text, repeatability)
exit=0
```

原始完整短输出均保存在仓库 `.local/cache/`，可使用 `Get-Content -LiteralPath <日志路径>` 复核：

| 日志名 | SHA-256 |
| --- | --- |
| task-1-fix-red-1.log | `427615ee0cfe4a49c63f29dee5b802e59e812048b38ee6b39cee9ebf91beed22` |
| task-1-fix-red-2.log | `3a3e2332a9ece0f8b1f66e02f30899378acaf642acba0dd48343d55ed34b02a7` |
| task-1-fix-red-3.log | `cbb187945d454a97c5dfdd5f237ce9bcbef00e61e5943ad01d8f0ca172f87db3` |
| task-1-fix-green-1.log / green-2.log / green-3.log | `604fa9ace5173ec8f73a3bc43ffc09be0f848a60de25a0c85960e97a1cc5e014`（三份相同） |

额外执行 `git diff --check`、`git diff --cached --check`，均退出 0、无错误；提交前确认暂存区仅有扫描器。本报告属于文档，RED/GREEN 不适用，无镜像测试。普通 `git add -- scripts/audit-product-brand.py` 曾被沙箱拒绝创建 index.lock；按用户授权使用 require_escalated 后暂存、提交成功。

## 风险信号与未覆盖项

三个 Important 已逐项修复，聚焦 self-test 全部通过；本轮没有意外的生产测试失败。未运行真实基线、全库测试、构建或浏览器验收；旧基线计数只代表修复前扫描器，不能当作当前计数。未修改或重新验证图片像素、视频及已有 UI 的运行效果。

枚举失败通过在临时仓库 CLI 进程内注入标准库 `os.scandir` 的 PermissionError 验证，未修改宿主 ACL。仍沿用原扫描器的 UTF-8 / JSON 字符串范围和可信 ZIP 整条读取限制；不声称覆盖任意语言转义或恶意归档。未调用独立复审者，不声称获得新一轮审查通过。原有未提交用户修改与素材保留；缓存日志不提交。
