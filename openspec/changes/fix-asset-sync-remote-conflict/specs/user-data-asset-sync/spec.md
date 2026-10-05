# user-data-asset-sync Specification

## ADDED Requirements

### Requirement: 素材基线认领（Acknowledge）

前端云同步模块 SHALL 暴露素材基线认领入口：调用方提供来自服务端权威响应的素材记录时，同步模块 SHALL 无条件把该记录采纳为同步基线并标记为已验证，后续增量校验不得将其判定为远端冲突。

#### Scenario: 角色域写入后投影回流

- **WHEN** 角色域接口（编辑角色、绑定声音/三视图、候选归并、生成回调）返回服务端权威素材数据，且调用方已将该数据投影写入本地素材库
- **THEN** 同步基线更新为该投影记录
- **AND** 下一次云端自动保存不再因该素材抛出"远端版本已变化"

### Requirement: entity 素材冲突判定基于版本号

对 `kind` 为 entity 的素材，增量校验的远端冲突判定 SHALL 优先比较基线与服务端记录的 `primaryVersionId`；任一方缺失 `primaryVersionId` 时 SHALL 回退到 `updatedAt` 比较。非 entity 素材的冲突判定行为保持不变（比较 `updatedAt`）。

#### Scenario: 服务端版本号未变化

- **GIVEN** 本地 entity 素材与服务端记录的 `primaryVersionId` 相同
- **WHEN** 增量校验执行
- **THEN** 判定为无冲突并继续推送

#### Scenario: 服务端版本号已变化

- **GIVEN** 服务端记录的 `primaryVersionId` 与基线不同
- **WHEN** 增量校验在非强制模式下执行
- **THEN** 该素材被判定为冲突并进入失败隔离，不覆盖服务端记录

### Requirement: 单素材校验失败不阻断整批同步

增量保存中任一素材的远端校验失败 SHALL 只隔离该素材：跳过其推送，其余待保存素材与画布 SHALL 继续推送。存在被隔离素材时，本轮保存 SHALL 以聚合错误收尾并向调用方报告被隔离素材；被隔离素材之外的内容视为已保存。

#### Scenario: 一个素材中毒其余照常保存

- **GIVEN** 待保存集合包含素材 A（远端校验失败）与素材 B、画布 P
- **WHEN** 云端保存执行
- **THEN** 素材 B 与画布 P 完成远端推送并确认
- **AND** 保存结果报告素材 A 校验失败及其名称

### Requirement: entity 素材不走通用覆盖推送

entity 素材 SHALL NOT 通过通用素材覆盖接口（按 ID 整体替换素材记录）推送：角色 payload 由后端角色域接口权威维护，前端投影仅用于展示与基线对齐。entity 素材被判定为本地待推送时，同步模块 SHALL 仅对齐基线并记录告警，不发起覆盖请求。

#### Scenario: 角色投影被判脏

- **GIVEN** 角色投影导致本地 entity 素材与基线不一致
- **WHEN** 增量保存执行
- **THEN** 不向服务端发起该素材的覆盖请求
- **AND** 同步基线与本地投影对齐，后续轮次不再将其判脏
