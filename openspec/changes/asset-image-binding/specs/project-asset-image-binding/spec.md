# project-asset-image-binding Specification

## ADDED Requirements

### Requirement: 非角色项目资产图片绑定

项目资产接口 SHALL 提供图片绑定能力：对分类为 environment、prop、material、other 的项目资产，用户可通过图片绑定接口把一张或多张已上传图片资源绑定为资产新版本；每次绑定创建新资产版本并推进版本号。角色分类资产 SHALL 被该通用接口拒绝（角色继续使用角色形象接口）。

#### Scenario: 为场景资产绑定图片

- **GIVEN** 项目中存在分类为 environment 的资产，用户已上传一张就绪图片资源
- **WHEN** 用户调用图片绑定接口提交该资源
- **THEN** 服务端创建新资产版本并将图片记录为该版本的 primary 表现
- **AND** 资产列表与预览可见新图片

#### Scenario: 角色资产走通用接口被拒绝

- **GIVEN** 项目中存在分类为 character 的资产
- **WHEN** 用户对它调用通用图片绑定接口
- **THEN** 服务端拒绝请求并提示使用角色形象接口

### Requirement: 资源强校验

图片绑定接口 SHALL 校验每个资源属于当前用户、类型为 image、状态就绪；未通过校验的请求 SHALL 整体失败，不产生部分绑定。

#### Scenario: 资源不可用时整体失败

- **GIVEN** 请求中包含一个未就绪或非图片资源
- **WHEN** 提交绑定
- **THEN** 请求失败且不创建新版本

### Requirement: 前端替换图片入口覆盖可绑定分类

项目资产预览弹窗 SHALL 对 character、environment、prop 分类展示"替换图片"入口；非角色分类的绑定流程 SHALL 复用素材库选择器并在提交时调用通用图片绑定接口，不要求三视图语义。

#### Scenario: 预览弹窗为场景资产提供替换图片

- **GIVEN** 用户打开一个 environment 分类的资产预览
- **WHEN** 查看弹窗操作区
- **THEN** 存在"替换图片"按钮
- **AND** 选择一张素材库图片并确认后，绑定成功并提示生成新版本
