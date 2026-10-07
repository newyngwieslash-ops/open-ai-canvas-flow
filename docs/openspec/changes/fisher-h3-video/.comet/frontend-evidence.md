# Fisher-H3 前端实施证据

## 任务结果

- `dba57f72-d5ff-4e6e-9ef3-e33879178c6d`：DONE。个人渠道页新增 Fisher-H3 预设，准确填写地址、模型及协议，API Key 为空；新增能力总容量与三档服务分辨率，不修改官方 MiniMax 能力。价格作为说明保留，不改账户价格。
- `02117ce2-6692-46cc-9fdc-07e68f3d6291`：DONE。画布两处提示词入口、配置节点和创作台真实组件复用原生控件选择首尾帧；素材库显示 ID 与实际请求媒体 ID 对齐。提交集合保留明确帧与驱动；最终实际媒体集合交 Fisher helper 独立角色计数校验。
- `5bd4c7be-cd3c-4f85-b013-5a58349b0926`：DONE。驱动选择、音频模式、去噪及参考布尔参数使用已有 metadata 命名空间。无驱动时 UI 为 4–15，有驱动为 4–60；保留原始时长并在创建前拒绝非法值。创作历史保存、恢复 Fisher 元数据；失效 ID 被阻止提交，清空素材与已提交草稿重置选项。画布任务元数据保留命名空间，重复确认指纹包含实际 Fisher 参数。
- `bc057669-f32b-4943-b2e4-7f2fcfe63c03`：DONE。按媒体 ID 存储偏移，画布与创作台原生数字控件可修改；实际提交前复用纯 helper 检查非负有限数、素材存在及已知原/剩余视频时长。

## RED → GREEN

1. `bun test test/fisher-h3-config.test.ts`：RED，缺少 `createFisherH3Channel` 导出；实现预设与能力后 GREEN，2 个测试。
2. `bun test test/fisher-h3-canvas-inputs.test.ts`：RED，显式提示词引用导致选中驱动音频数组为空；修正实际提交集合后 GREEN。
3. `bun test test/fisher-h3-ui-state.test.ts`：RED，共享 UI 状态模块不存在；增加条件能力与真实提交预检后 GREEN。

最终专项和邻近回归：

```powershell
cd web
bun test test/fisher-h3-config.test.ts test/fisher-h3-canvas-inputs.test.ts test/fisher-h3-ui-state.test.ts test/fisher-h3-controls.test.tsx test/model-capabilities.test.ts test/canvas-node-generation-mentions.test.ts test/creation-generation-config.test.ts test/canvas-generation-submission.test.ts
```

结果：36 pass、0 fail、103 expectations。覆盖空 Key、独立容量、实际驱动/尾帧集合、条件时长、失效 ID、默认选项合并、0/false、严格时长、参数保留、控件 HTML 与原有调用路径回归。

运行 `bunx tsc --noEmit -p tsconfig.json --incremental false`：前端本工作包文件无错误；当次唯一错误属于并行主会话适配文件 `src/services/api/video-provider-fisher.ts(27,71)`，已通知主会话修复后复核。没有运行构建、启动 dev server 或请求真实付费生成。

## 静态 UI / 限制

共享控件采用带文本标签的原生 select/number/checkbox 和 details，支持键盘；画布边界保留 no-zoom / wheel-scroll，使用当前项目颜色变量。SSR 检查真实组件渲染了帧、驱动、偏移标签，显示 1920×1088 及正确驱动时长，保留显式 0/false。未执行浏览器验收，SSR 与源码阅读不等同真实布局验收；主会话最终 Verify 应保留此缺口。

## 有界复查修正

新增渠道范围检查先 RED：声明 min=6/max=10 的能力在选择驱动后被错误扩为 4–60。修正为交集；枚举也按有无驱动筛选，不能扩张管理员声明。真实画布调用组件通过选定 config 接收有效渠道默认值并复用 `generationMetadata` 合并节点覆盖，SSR 检查 native/.2/显式 false 正确展示。复查专项 6 pass、0 fail；再次运行完整 `bunx tsc --noEmit -p tsconfig.json --incremental false` 已通过（主会话适配器错误已修复）。

首审 IMPORTANT 有界修复：新建会话、选择其他会话、删除当前会话后切换到备用会话的三个清空草稿分支，都同步清空 Fisher 元数据；保留 `restoreMessageDraft` 从历史显式恢复的行为。新增“重置视频起始偏移”原生按钮，即使原视频已移除仍可清除失效偏移，同时保留驱动和音频控制。真实按钮 handler 检查先 RED（不存在恢复入口），再 GREEN（清空 offset 且其余选项保持）。`bun test test/fisher-h3-controls.test.tsx test/fisher-h3-ui-state.test.ts`：7 pass、0 fail、26 expectations；完整类型检查再次通过。三处草稿清空的一行状态补丁经源代码逐项核查，没有将其声称为浏览器交互验收。
