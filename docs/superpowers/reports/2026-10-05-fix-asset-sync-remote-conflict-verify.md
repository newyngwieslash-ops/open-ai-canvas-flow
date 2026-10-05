# 验证报告：fix-asset-sync-remote-conflict

日期：2026-10-05 ｜ 工作流：tweak ｜ verify_mode：full ｜ 分支：fix/asset-sync-conflict

## 概览

| 维度 | 状态 |
| --- | --- |
| 完整性 | 8/8 任务完成；delta spec 1 个能力 4 条需求全部有实现证据 |
| 正确性 | 4/4 需求覆盖，5 项专项测试 + 48 项回归通过；无 WARNING 及以上问题 |
| 一致性 | 实现与 design.md 四项决策一致；代码风格与既有同步模块一致 |

## 检查明细

| # | 检查项 | 结果 | 证据 |
| --- | --- | --- | --- |
| 1 | tasks.md 全部勾选 | PASS | 8/8 `[x]`（openspec/changes/fix-asset-sync-remote-conflict/tasks.md） |
| 2 | 变更文件与任务一致 | PASS | `git diff --stat c8c1a35a...HEAD`：11 个文件，均为任务所述范围（user-data-sync.ts、assets.tsx、专项测试、模型目录预存错误修复、文档、OpenSpec 产物） |
| 3 | 构建通过 | PASS | `bun run --cwd web build`（tsc --noEmit && vite build）exit=0，✓ built in 5.56s |
| 4 | 相关测试通过 | PASS | `bun test` 5 个文件 53 项通过 0 失败（user-data-asset-sync 5 项专项 + user-data-sync-message 2 + asset-batch-delete 5 + canvas-remote-revision 22 + channel-model-catalog 19） |
| 5 | 无明显安全问题 | PASS | 未新增密钥、未新增 unsafe 操作；entity 跳过覆盖推送降低了写入面 |
| 6 | 代码评审 | PASS（tweak direct 模式未配置独立评审） | review_mode 默认 off；diff 范围小且全部行为由专项测试锁定，跳过原因记录于此 |

## 需求 → 实现映射

| spec 需求 | 实现证据 | 场景覆盖 |
| --- | --- | --- |
| 素材基线认领（Acknowledge） | `web/src/services/user-data-sync.ts:312` `acknowledgeRemoteAssets`；调用点 `assets.tsx:767` | 测试「角色域写入后 acknowledge 投影，后续保存不再误报」 |
| entity 冲突判定基于版本号 | `user-data-sync.ts:1001` `remoteAssetConflictsWithBaseline`（entity 比较 `primaryVersionId`，缺失回退 `updatedAt`） | 测试「entity 版本号相同而服务端 updatedAt 变化时不再误判冲突」 |
| 单素材校验失败不阻断整批 | `user-data-sync.ts:820-825`（隔离收集）、`:844`（跳过推送）、`:859-862`（聚合错误含素材名） | 测试「entity 冲突只隔离该素材，其余素材与画布照常推送」「未 acknowledge 的旧基线仍会隔离并抛聚合错误」 |
| entity 素材不走通用覆盖推送 | `user-data-sync.ts:845-853`（跳过 upsert、对齐基线、console.warn） | 测试「entity 素材判脏后跳过通用覆盖推送，仅对齐基线」 |

## design.md 决策遵循

- 决策 1（新增 acknowledge 入口而非复用 acceptRemoteAssets）：遵循，带无条件语义与注释说明。
- 决策 2（entity 比较版本号、缺失回退时间戳、仅 entity 启用）：遵循。
- 决策 3（失败隔离 + 推送完成后聚合上抛、保留"素材远端版本已变化"原文）：遵循。
- 决策 4（entity 跳过通用 upsert、告警、acknowledge 投影）：遵循。

## 备注

- `web/src/lib/channel-model-catalog.ts` 的重复属性修复为预存阻塞问题的顺带修复，已由 channel-model-catalog.test.ts 19 项回归锁定。
- 浏览器手工验收项已记录于 `docs/content/docs/progress/pending-test.mdx`（角色编辑后自动同步、替换图片绑定、真实双端冲突），不阻塞归档。

## 结论

全部检查通过，无 CRITICAL / WARNING 问题。可进入归档确认。
