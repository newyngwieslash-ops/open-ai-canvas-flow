# 画布同步本地功能扩展

## ADDED Requirements

### Requirement: 确认基线持久化恢复

画布保存确认后，系统 SHALL 在用户作用域本地存储中为每个画布维护至多两条确认基线回执（当前基线与其前驱），并在打开画布、Agent 刷新、增量同步与保存路径统一使用以下恢复顺序：内存 `acknowledgedProjects` 基线 → 本地快照候选（revision 与内容哈希均匹配当前本地文档）→ 服务端指定版本完整操作回执（`getRemoteCanvasRevision`，ID、revision、哈希全部一致才可采用）。

#### Scenario: 刷新后从本地快照恢复基线

- **WHEN** 浏览器刷新后内存基线为空，本地存在与当前画布 revision 和 remoteContentHash 一致的确认快照
- **THEN** 该快照被用作三方合并基线，本地编辑不丢失
- **AND** 哈希不匹配的候选不得被采用为基线

#### Scenario: 无任何可信基线时拒绝自动合并

- **WHEN** 内存基线、本地快照、服务端回执均无法通过核验
- **THEN** 系统报"缺少已确认基线，无法自动合并"并保留本地编辑
- **AND** 不得用当日云端状态冒充基线

### Requirement: 素材冲突隔离与聚合报告（DEFERRED）

> v1.6.8.12 上游以"冲突素材自动备份并采用云端"统一了素材冲突语义（#798–#809）。entity（角色卡）投影链路——跳过通用覆盖推送、专用角色接口对齐、隔离聚合报告——依赖后端 `/characters` 域投影逻辑整体重实现，**移出本 change 范围**，作为独立后续 change 交付。本 change 保留 `acknowledgeRemoteAssets` 基线认领导出（其调用方已存在且 2 项认领回归通过）。

### Requirement: 收敛 create_node 重放

journal 重放收到 409 `canvas_field_conflict` 时，系统 SHALL 先读取服务端权威画布文档，用 `dropConvergedCreateNodeOperations` 剔除服务端已存在的 create_node 操作；收敛请求先持久化再重试，全部操作已收敛时 SHALL 返回现网 revision 而不重发任何操作。

#### Scenario: 幽灵 create 重放解除阻塞

- **WHEN** 响应丢失导致 journal 保留 create_node 重放，而服务端已提交同 ID 节点
- **THEN** 重试 409 后该 create_node 被剔除并以新 mutationId 重试剩余操作
- **AND** 全部操作收敛时调用方收到现网 revision，画布不再被旧 journal 阻塞

### Requirement: 保存前脏基线预修复

批量保存画布前，系统 SHALL 对哈希漂移的确认基线（本地 revision > 0 且基线内容哈希与自身不符）执行历史回执恢复，恢复结果重新写入 `acknowledgedProjects`；登录恢复的脏缓存不得被静默视为已保存状态。

#### Scenario: 登录恢复的脏缓存不误报已保存

- **WHEN** 增量会话中某画布基线来自登录时的脏缓存且哈希自漂移
- **THEN** 系统先从历史回执恢复可信基线再判定 dirty
- **AND** 恢复失败时该画布进入无基线隔离，不阻塞其他画布
