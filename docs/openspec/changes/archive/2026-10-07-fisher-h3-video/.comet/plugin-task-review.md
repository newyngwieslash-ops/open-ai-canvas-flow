# Fisher 插件任务独立审查

范围：任务 `72abd7c7-a9e6-4141-b88d-343d256d5a6c` 的生成器 Fisher spec、生成 manifest/说明/安装包及 `backend/internal/protocol/fisher_h3_video_test.go`。没有审查或修改前端实现，也没有修改受审插件代码。

结论：首审参数声明问题已修复，唯一一次针对性复查通过；该插件任务无剩余阻断发现。

## P2：参数目录声明了实际上不会发送的声音与水印字段

位置：`plugin-packages/generate-catalog.mjs:123` 和 `:124`，Fisher 的 provider 与 workflow 都直接使用通用 `videoParams`。

生成目录和包因此包含 `generateAudio → generate_audio`、`watermark → watermark`，但 Fisher 的真实请求模板没有这两个字段，专项测试还明确要求不发送 `generate_audio` 和 `aigc_watermark`。协议浏览器及生成接口文档会展示这些声明，使用户认为设置它们会影响 Fisher 输出；实际设置被忽略。

建议：复用现有参数数组，仅为 Fisher 过滤未支持的 `generateAudio` 与 `watermark`，并将必需的 `duration`、`resolution` 标为必填；同步生成 manifest、文档及包，补一条真实包参数声明检查。不要修改其他 provider 的通用声明。

## 已核查通过

- 独立 ID、Fisher-H3 标签、精确 MiniMax-H3、默认 Base URL、Bearer、公网素材要求及 canvas/creation/user/admin/agent scopes。
- 既有启动流程扫描 `.yingce-plugin` 并加载声明式 provider，新增包无需硬编码 host 适配器；真实包与生成 manifest 的字节一致性已有检查。
- 480P/768P/1080P 大小写映射；固定比例保留；只在关键帧时放行 adaptive/auto；文本空比例拒绝。
- 各类角色独立计数、仅尾帧、九参考图加关键帧、三参考音频加驱动和 60 秒驱动输出。
- 音频默认模式/去噪/参考开关、显式 0/false、非 native 驱动约束和 reference_only 冲突拒绝。
- 偏移放内容项顶层；已知媒体时长与 ID 绑定校验由已确认的宿主工作包承担，未要求 DSL 重复完整探测逻辑。
- 创建只接受 task_id，poll 保留上下文任务 ID，request_id 不代替任务；五种任务状态与成功视频 URL 解析。

独立重跑 `cd backend; go test ./internal/protocol -run '^TestFisherH3' -count=1`：通过（`ok ... 0.372s`）。无真实 Key，未进行上游生成。

## 唯一一次针对性复查：PASS

仅复查首审 P2 参数合同修复，未扩大审查范围。生成器 Fisher 局部数组过滤 `generateAudio`/`watermark`、将 duration/resolution 标为必填，未改变通用 videoParams；provider 和 workflow 共享该数组。生成源 manifest、接口文档以及真实包载入后的 adapter.Metadata 参数均不再声明这两个字段，必填信息一致。真实包与 manifest 字节一致性及 workflow/provider 参数深度相等检查通过，既有基于 adapter 元数据的目录贡献也使用修复后的声明。

`plugin-evidence.md` 记录真实安装包测试的四处 RED → GREEN。独立再次运行 `go test ./internal/protocol -run '^TestFisherH3' -count=1`：通过（`ok ... 0.444s`），包含新的参数合同检查和既有请求/响应检查。首审 P2 已关闭；没有修改受审实现。
