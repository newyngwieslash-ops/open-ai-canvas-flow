# Proposal：修复角色资产云同步误报"素材远端版本已变化"

## Why

角色卡操作（编辑角色、绑定声音、绑定三视图、候选归并、三视图生成回调）由后端领域接口重写 `payload_json` 并用服务器时钟重新生成内嵌 `updatedAt`，但前端拿到响应后只写了 asset store、没有同步更新云端同步基线 `acknowledgedAssets`。下一次 `saveRemoteUserDataNow()` 校验时把前端自己发起、已 ingestion 到 store 的服务端写入误判为第三方远端修改，抛出"素材远端版本已变化，已停止覆盖，请重新打开素材库"。该错误发生在整批推送之前，会中断本次会话所有云保存（含画布保存），且报错文案指引的"重新打开素材库"并不能恢复基线（`acceptRemoteAssets` 会跳过本地已脏的素材），只有整页刷新有效。

## What Changes

- user-data-sync 暴露 `acknowledgeRemoteAssets(assets)` 入口：调用方拿到服务端权威素材数据（本项目为角色域接口响应投影）时，同时更新 `acknowledgedAssets` 基线与 `verifiedAssets` 标记，消除自源写入误报。
- 增量校验（`saveRemoteUserDataBatch`）对 `kind === "entity"` 的素材改用 `primaryVersionId` 比对：角色资产每次服务端操作必然产生新版本 ID，比时间戳（RFC3339 纳秒字符串比较）更可靠。
- 单个素材远端校验失败不再中断整批同步：隔离该素材（标记为需人工介入），继续推送其余素材与画布；画布保存不再被单个素材阻断。
- entity 资产不再走通用 `upsertRemoteAsset`：角色 payload 由后端领域接口（角色版本链）权威维护，前端投影推送会用列表投影覆盖服务端完整 payload。entity 素材跳过通用 upsert，仅当其从未同步过时告警说明。

## Capabilities

### New Capabilities

- `user-data-asset-sync`: 前端用户素材云端同步的基线维护、增量校验与失败隔离行为。

### Modified Capabilities

（无既有 spec，项目 openspec 为首次使用。）

## Impact

- `web/src/services/user-data-sync.ts`：新增 acknowledge 入口、校验逻辑分型、失败隔离、entity upsert 跳过。
- `web/src/pages/projects/detail/assets.tsx`：`syncPersonalCharacterProjection` 调用 acknowledge 入口。
- 专项测试：`web/test/`（对应用户数据同步素材校验路径）。
- 不改后端接口与数据结构；不改通用媒体素材同步路径。
