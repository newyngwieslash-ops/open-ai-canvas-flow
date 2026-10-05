# Tasks：asset-image-binding

## 1. 后端

- [x] 1.1 服务层 `ReplaceProjectAssetRepresentations`：分类白名单校验（拒绝 character）、资源强校验、复用版本化事务写新版本并更新 payload 的 primaryVersionId/updatedAt/data.storageKey
- [x] 1.2 路由 `PUT /projects/:id/assets/:assetId/representations`
- [x] 1.3 后端专项测试：场景绑定成功生成新版本、角色被拒、资源未就绪整体失败

## 2. 前端

- [x] 2.1 API 函数 `replaceProjectAssetRepresentations`
- [x] 2.2 预览弹窗"替换图片"入口放宽到 environment/prop，复用绑定图片选择器（非角色文案），绑定走新接口

## 3. 测试与构建

- [x] 3.1 前端专项测试（按钮分流 + 绑定 mutation 路由）并运行
- [x] 3.2 `go test ./internal/app ./internal/handler` 与 `go build ./...`、`bun run build`

## 4. 文档

- [x] 4.1 backend-database.mdx 补 `asset_representations`；features.mdx 资产章节说明绑定范围；pending-test.mdx 记录待验收项
