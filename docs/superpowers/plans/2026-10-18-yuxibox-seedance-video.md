---
change: add-yuxibox-seedance-video-channel
design-doc: docs/superpowers/specs/2026-10-18-yuxibox-seedance-video-design.md
base-ref: 8f927d5301078a2a9e3ea6ad3ce1ae783b210302
---

# yuxibox seedance2.5 视频渠道实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增声明式协议插件 `yuxibox-seedance-video`（yuxibox.cn 中转 seedance2.5-yx-count），后端注册 + 能力默认值 + 前端渠道预设/能力映射，使画布与创作页可通过 yuxibox 渠道完成文生视频与多模态参考生视频（30 图 / 10 视频 / 10 音频，4–30 秒默认 30，七档 ratio，固定 720P，按条计费）。

**Architecture:** 协议知识全部收敛进插件 manifest（`plugin-packages/generate-catalog.mjs` 新增定义块生成，与 metaso/fisher 同构），后端零专属宿主代码——创建/轮询/下载/SSRF/恢复合同全部复用 `provider_protocol.go` 声明式宿主。后端仅两处 case：`generation/registry.go` 注册显示名、`app/model_capability_defaults.go` 提供能力默认值。前端三处：`createYuxiboxChannel()` 预设 + 设置页按钮 + `model-capabilities.ts` 能力映射；**不新增前端 provider**（声明式协议任务由后端代理执行，见设计文档 2.4 节）。

**Tech Stack:** Node ESM 脚本生成 manifest → `.yingce-plugin` zip 包；Go 1.25 声明式协议适配器（`backend/internal/protocol`，契约测试仿 `metaso_minimax_video_test.go`）；TypeScript + Bun（`web/`）。

## Global Constraints

以下约束来自设计文档（specs/2026-10-18-yuxibox-seedance-video-design.md）与 delta spec，所有任务隐含遵守：

- 协议 ID / provider ID 一律为 `yuxibox-seedance-video`；显示名 `yuxibox Seedance`；vendor `yuxibox`；能力 `video`；模型精确 `seedance2.5-yx-count`。
- Base URL：`https://yuxibox.cn`；Bearer 鉴权（`auth: bearer`，field `apiKey`）；`requiresPublicMediaUrls: true`。
- 端点：创建 `POST /v1/videos`（JSON），查询 `GET /v1/videos/{{taskId}}`；无取消接口（manifest 不声明 cancel）。
- 参数档位（manifest validations、`model_capability_defaults.go`、前端 `model-capabilities.ts` 三处一致）：prompt 非空且 ≤ 16000 字符；`duration` 4–30 整数默认 30；`aspectRatio` ∈ {"adaptive","16:9","9:16","1:1","4:3","3:4","21:9"} 缺省 "16:9"（统一 ratio 策略 D2：不发 `size`）；参考上限 images ≤ 30 / videos ≤ 10 / audios ≤ 10。
- create body：`model`、`prompt`、`seconds`（`{$toString: duration}`，字符串）、`ratio`、`reference_images[]`、`videos[]`、`audios[]`（后三组 `omit` 空数组不发字段键）；不发送 `input_reference`、`size`、`resolution`、`generateAudio`、`watermark`。
- params 过滤：复用共享 `videoParams` 但**剔除** `resolution`、`generateAudio`、`watermark` 三项（上游合同无这些字段）。
- 响应映射（`asyncResponse("video")` 基座 + overrides）：taskId ← response.id/task_id/taskId/data.id；status ← response.status/state/data.status 缺省 "pending"；message ← error.message/message/fail_reason；videos ← video_url/videoUrl/result_url/url/data.video_url/output.url coalesce 链；errorPaths `["error.code"]`；resultEphemeral true。
- 计费：`billingMode: "fixed_request"`，`unitPriceMicrocredits: 0`；描述注明基础价 4.5 元/条与令牌分组倍率，不预设 unitPrice 死值。
- 后端能力默认值（D3 设计骨架）：Duration `range` 4–30 step 1 default 30；Ratios 七档 default "16:9"；Resolutions 仅 `["720p"]`（不暴露分辨率选择）；GenerateAudio/Watermark 基座默认（Supported false）；Operations 在基座 `["text_to_video","image_to_video"]` 上追加 `reference_to_video`、`audio_to_video`；字节上限沿用宿主默认。
- 不发 multipart `input_reference`；同一视频不重复 POST（宿主既有恢复合同，插件层无需额外代码）；创建响应缺任务 ID 按"结果不确定"失败上抛（asyncResponse taskId 提取失败即报错）。
- 不改数据库 schema；不改 `provider_protocol.go`/轮询器/SSRF 出站；不加前端 provider 分发行（设计文档 2.4 节）。
- 仓库约定：手动编辑产出的文件行尾用 LF；`docs/*` 在 gitignore 中，提交 change 产物（计划/spec/openspec/pending-test 等 docs 下文件）需 `git add -f`；已纳入 gitignore 白名单的 `docs/content/docs/**` 已跟踪文件正常 add。
- Git 提交说明格式：`<type>(<scope>): <业务模块> - <变更摘要>`（如 `feat(video): yuxibox seedance2.5 - 新增声明式视频协议插件`）。
- 验证命令统一：后端 `go -C backend test ./...` 或专项；前端 `cd web && bun run build`；文档站 `cd docs && bun run types:check`。

## File Structure

```
plugin-packages/
  generate-catalog.mjs                        # 修改：新增 yuxibox 定义块（metaso 段 L166–211 之后）
  yuxibox-seedance-video/                     # 生成（脚本产物）
    manifest.json                             # 生成（node generate-catalog.mjs）
    README.md                                 # 手写：插件身份与边界说明
    docs/interface.md                         # 手写：接口字段文档（embed 后附 manifest 契约）
  yuxibox-seedance-video.yingce-plugin        # build-packages.sh 产物
backend/internal/generation/registry.go        # 修改：OfficialDeclarativeVideoInterface 加 case
backend/internal/app/model_capability_defaults.go  # 修改：加 yuxibox-seedance-video case
backend/internal/protocol/yuxibox_seedance_video_test.go  # 新增：插件契约测试
web/src/lib/model-capabilities.ts             # 修改：能力映射加 yuxibox 分支
web/src/stores/use-config-store.ts            # 修改：createYuxiboxChannel()
web/src/pages/settings/channel-settings-pane.tsx  # 修改：preset "yuxibox" + 按钮
docs/content/docs/backend/http-api.mdx        # 修改：渠道协议说明（已跟踪文件，正常 add）
docs/content/docs/progress/pending-test.mdx   # 修改：登记待验收（已跟踪文件，正常 add）
```

说明：`docs/superpowers/plans/`（本计划）与 `docs/openspec/changes/add-yuxibox-seedance-video-channel/` 均命中 `.gitignore:5 docs/*` 规则，提交时需 `git add -f`。`docs/content/docs/backend/http-api.mdx` 与 `docs/content/docs/progress/pending-test.mdx` 在 gitignore 白名单中且已被 git 跟踪，正常 `git add` 即可。

---

### Task 1: 插件定义与 manifest 生成（tasks 1.1）

**Files:**
- Modify: `plugin-packages/generate-catalog.mjs`（metaso 定义块位于 L166–211，其结束 `}`（L211）之后插入新块；新增位置在 L211 `}` 与 L213 `add({ id: "openai-chat-completions" ...` 之间）
- Create（生成产物，不手写）: `plugin-packages/yuxibox-seedance-video/manifest.json`

**Interfaces:**
- Consumes: `generate-catalog.mjs` 顶部既有助手——`ref`/`omit`/`coalesce`/`map`/`len`/`trim`/`gte`/`lte`/`gt`/`and`/`eq`/`sorted`、`videoParams`、`parameters`、`bearer`、`jsonCreate`、`asyncResponse`、`config`；`includes` 写法 `({ $in: [value, values] })`（metaso L176 同款）。
- Produces: provider `yuxibox-seedance-video`（manifest 契约即 Task 3 契约测试与 Task 4 能力默认值的行为真相）；`yuxibox-seedance-video.yingce-plugin` 包（Task 2/3 读取）。

- **Step 1: 在 metaso 定义块后新增 yuxibox 定义块** <!-- comet-task-ref:78f4fe0d-3d97-4dad-a07d-4475a30dde79 -->

在 L211（metaso 块结束 `}`）与 L213（`add({ id: "openai-chat-completions"`）之间插入以下完整块（独立块作用域，与 metaso 段同层级）：

```js
{
  // yuxibox 合同无 resolution/generateAudio/watermark，参数从源头不暴露；时长强制必填（默认 30 由能力层与 workflow defaults 保证）。
  const yuxiboxParams = videoParams
    .filter(([name]) => !["resolution", "generateAudio", "watermark"].includes(name))
    .map(([name, type, required, mapping, description]) => [name, type, required || ["duration"].includes(name), mapping, description]);
  const yuxiboxIncludes = (value, values) => ({ $in: [value, values] });
  const yuxiboxRatios = ["adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];
  add({
    id: "yuxibox-seedance-video", providerId: "yuxibox-seedance-video", name: "yuxibox Seedance", vendor: "yuxibox", capability: "video",
    baseUrl: "https://yuxibox.cn", auth: bearer, params: yuxiboxParams, requiresPublicMediaUrls: true,
    workflows: [{ id: "yuxibox-seedance-video", label: "yuxibox Seedance", providerId: "yuxibox-seedance-video", capability: "video", parameters: parameters(yuxiboxParams), defaults: { model: "seedance2.5-yx-count", duration: 30, aspectRatio: "16:9" } }],
    notes: "yuxibox（yuxibox.cn）中转 seedance2.5-yx-count：OpenAI Videos 风格异步合同，创建 POST /v1/videos JSON、查询 GET /v1/videos/{task_id}。模型名称精确为 seedance2.5-yx-count，时长 4–30 秒整数默认 30，输出固定 720P，七档比例（adaptive/16:9/9:16/1:1/4:3/3:4/21:9）统一走 ratio 单字段（不发 size）。参考素材 JSON 数组：reference_images ≤ 30、videos ≤ 10、audios ≤ 10，须为公网 HTTPS 或 data URL；不发 multipart input_reference。seconds 以字符串发送（上游示例 \"seconds\": \"30\"）。按条计费基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响，以 yuxibox 控制台为准。同一视频不重复提交：宿主恢复合同保证已有任务 ID 只查询不重建；创建响应缺失任务 ID 按“结果不确定”失败上抛。上游未提供取消接口。真实 Key 的付费生成尚待用户验收。",
    validations: [
      { assert: eq(ref("request.model"), "seedance2.5-yx-count"), message: "yuxibox 渠道模型必须为 seedance2.5-yx-count。" },
      { assert: and(gt(len(trim(ref("request.prompt"))), 0), lte(len(ref("request.prompt")), 16000)), message: "yuxibox 提示词不能为空且最多 16000 字符。" },
      { assert: and(gte(ref("request.duration"), 4), lte(ref("request.duration"), 30)), message: "yuxibox 时长为 4–30 秒整数，默认 30。" },
      { assert: yuxiboxIncludes(coalesce(ref("request.aspectRatio"), "16:9"), yuxiboxRatios), message: "yuxibox 比例仅支持 adaptive、16:9、9:16、1:1、4:3、3:4、21:9。" },
      { assert: lte(len(ref("request.images")), 30), message: "yuxibox 参考图最多 30 张。" },
      { assert: lte(len(ref("request.videos")), 10), message: "yuxibox 参考视频最多 10 条。" },
      { assert: lte(len(ref("request.audios")), 10), message: "yuxibox 参考音频最多 10 条。" }
    ],
    create: jsonCreate("/v1/videos", {
      model: ref("request.model"),
      prompt: ref("request.prompt"),
      seconds: { $toString: ref("request.duration") },
      ratio: coalesce(ref("request.aspectRatio"), "16:9"),
      reference_images: omit(map({ $sortByOrder: ref("request.images") }, "media", ref("media.value"))),
      videos: omit(map({ $sortByOrder: ref("request.videos") }, "media", ref("media.value"))),
      audios: omit(map({ $sortByOrder: ref("request.audios") }, "media", ref("media.value")))
    }),
    poll: { method: "GET", path: "/v1/videos/{{taskId}}" },
    response: asyncResponse("video", {
      taskId: coalesce(ref("response.id"), ref("response.task_id"), ref("response.taskId"), ref("response.data.id"), ref("taskId")),
      status: coalesce(ref("response.status"), ref("response.state"), ref("response.data.status"), "pending"),
      message: coalesce(ref("response.error.message"), ref("response.message"), ref("response.fail_reason")),
      videos: coalesce(ref("response.video_url"), ref("response.videoUrl"), ref("response.result_url"), ref("response.url"), ref("response.data.video_url"), ref("response.output.url")),
      errorPaths: ["error.code"],
      resultEphemeral: true
    })
  });
}
```

要点核对（与设计文档 2.1 节逐项一致）：
- `params` 过滤掉 `resolution`/`generateAudio`/`watermark`，保留 model/prompt/images/videos/audios/duration/aspectRatio/providerOptions；`duration` 提升为必填（metaso L169 同款写法）。说明：video manifest 参数的 `required` 是文档/UI 元数据，不在协议宿主运行时强制；真正的档位闸门是 validation `4 <= duration <= 30`——后端 `provider_protocol_request.go:78-80` 只在 `VideoSeconds` 有效时注入 `request.Duration`，缺省为 0 会被校验拒绝。默认 30 由能力层 Duration.Default=30（Task 3/5）驱动 UI 始终携带时长，spec 场景"默认时长"由此保证。
- validations 七条全部中文错误消息；ratio 用 `coalesce(aspectRatio, "16:9")` 再白名单。
- create 用 `{$sortByOrder: ...}` 直接 map（newapi-channel-2 L812-814 同款，无 role 处理——yuxibox 上游只要 URL 数组）。
- `seconds` 用 `{$toString: duration}` 字符串化（openai-videos L772 同款）。
- response overrides 显式重申 taskId/status/message/videos 链（`asyncResponse("video")` 基座即此四项，显式写出便于 interface.md 文档渲染与后续验收时单独调整；errorPaths 与 resultEphemeral 与基座一致）。
- 无 `cancel` 声明（上游无 DELETE）。

- **Step 2: 运行生成脚本，产出 manifest** <!-- comet-task-ref:78f4fe0d-3d97-4dad-a07d-4475a30dde79 -->

```bash
cd plugin-packages && node generate-catalog.mjs
```

预期：`plugin-packages/yuxibox-seedance-video/manifest.json` 生成，无报错；`specs` 数量比执行前多 1。

- **Step 3: 抽查 manifest 关键字段** <!-- comet-task-ref:78f4fe0d-3d97-4dad-a07d-4475a30dde79 -->

```bash
node -e "const m=require('./plugin-packages/yuxibox-seedance-video/manifest.json');const p=m.contributes.providers[0];console.log(p.id,p.baseUrl,p.auth.type,p.requiresPublicMediaUrls,p.parameters.map(x=>x.name).join(','))"
```

预期输出：`yuxibox-seedance-video https://yuxibox.cn bearer true model,prompt,images,videos,audios,duration,aspectRatio,providerOptions`（不含 resolution/generateAudio/watermark）。

再核对 workflow defaults：

```bash
node -e "const m=require('./plugin-packages/yuxibox-seedance-video/manifest.json');console.log(JSON.stringify(m.contributes.workflows[0].defaults))"
```

预期输出：`{"model":"seedance2.5-yx-count","duration":30,"aspectRatio":"16:9"}`。

- **Step 4: Commit** <!-- comet-task-ref:78f4fe0d-3d97-4dad-a07d-4475a30dde79 -->

```bash
git add plugin-packages/generate-catalog.mjs plugin-packages/yuxibox-seedance-video/manifest.json
git commit -m "feat(video): yuxibox seedance2.5 - 新增插件定义与 manifest 生成"
```

---

### Task 2: 插件文档与打包（tasks 1.2、1.3）

**Files:**
- Create: `plugin-packages/yuxibox-seedance-video/README.md`（手写）
- Create: `plugin-packages/yuxibox-seedance-video/docs/interface.md`（手写正文；`embed-documentation.mjs` 会在文末追加 manifest 契约块）
- Create（脚本产物）: `plugin-packages/yuxibox-seedance-video.yingce-plugin`

**Interfaces:**
- Consumes: Task 1 生成的 `manifest.json`；`embed-documentation.mjs` 的拼接规则（README + interface.md → manifest `documentation` 字段）。
- Produces: `.yingce-plugin` zip 包（manifest.json、README.md、docs/interface.md），Task 3 契约测试 `ParsePluginPackage` 读取它并要求与源码 manifest 逐字节一致。

- **Step 1: 写 README.md** <!-- comet-task-ref:ec1bae1f-6178-4d69-b030-f7c61dcff4f5 -->

创建 `plugin-packages/yuxibox-seedance-video/README.md`（LF 行尾）：

```markdown
# yuxibox Seedance

该目录是独立官方协议插件源码。后端从生成的 `yuxibox-seedance-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

yuxibox（yuxibox.cn）中转 seedance2.5-yx-count 视频模型，提供 OpenAI Video 风格异步协议（`POST /v1/videos` 创建 + `GET /v1/videos/{task_id}` 查询）：文生视频与多模态参考生视频（参考图 ≤ 30、参考视频 ≤ 10、参考音频 ≤ 10），时长 4–30 秒默认 30，输出固定 720P，七档比例统一走 `ratio` 单字段（不发 `size`）。参考素材走 JSON 数组（`reference_images`/`videos`/`audios`），须为公网 HTTPS 或 data URL；不发送 multipart `input_reference`。按条计费，基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响。

完整接口见 [docs/interface.md](docs/interface.md)。
```

- **Step 2: 写 docs/interface.md 正文** <!-- comet-task-ref:ec1bae1f-6178-4d69-b030-f7c61dcff4f5 -->

创建 `plugin-packages/yuxibox-seedance-video/docs/interface.md`（LF 行尾）。正文骨架如下（manifest 契约块由 embed 脚本自动追加，手写部分如下；含嵌套代码块，整体照抄）：

````markdown
# yuxibox Seedance 接口字段

## 协议身份

- 插件 ID：`yuxibox-seedance-video`。
- Provider ID：`yuxibox-seedance-video`。
- 能力：`video`。
- 默认 Base URL：`https://yuxibox.cn`。
- 鉴权驱动：`bearer`（配置字段 `apiKey`）。
- 创建：`POST /v1/videos`（application/json）。
- 查询：`GET /v1/videos/{{taskId}}`。
- 取消：上游未提供，manifest 不声明 cancel。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key（Bearer） |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 视频模型 ID，精确 `seedance2.5-yx-count`。 |
| `prompt` | string | 是 | `prompt` | 视频提示词，1–16000 字符。 |
| `images` | media[] | 否 | `reference_images` | 参考图，最多 30 张，URL 数组。 |
| `videos` | media[] | 否 | `videos` | 参考视频，最多 10 条，URL 数组。 |
| `audios` | media[] | 否 | `audios` | 参考音频，最多 10 条，URL 数组。 |
| `duration` | integer | 是 | `seconds` | 时长秒数 4–30，默认 30；以字符串发送。 |
| `aspectRatio` | string | 否 | `ratio` | 画幅比例：adaptive/16:9/9:16/1:1/4:3/3:4/21:9，缺省 16:9；不发 `size`。 |
| `providerOptions` | object | 否 | provider-specific fields | 插件命名空间内的厂商扩展字段。 |

不暴露 `resolution`（输出固定 720P）、`generateAudio`、`watermark`——上游合同无这些字段。

## 请求示例

```json
POST /v1/videos
Authorization: Bearer <apiKey>
Content-Type: application/json

{
  "model": "seedance2.5-yx-count",
  "prompt": "少女在雨巷中转身回望",
  "seconds": "30",
  "ratio": "16:9",
  "reference_images": ["https://example.com/ref-1.jpg"],
  "videos": [],
  "audios": []
}
```

空素材组（`reference_images`/`videos`/`audios` 为空数组）不发送字段键；`seconds` 为字符串（上游示例 `"seconds": "30"`，整数同样接受）。

## 响应与错误

- 任务 ID 提取：`response.id` → `response.task_id` → `response.taskId` → `response.data.id`，提取失败按"结果不确定"失败上抛，不自动重发。
- 状态归一：`response.status`/`response.state`/`response.data.status`，缺省 `pending`。
- 错误消息：`response.error.message` → `response.message` → `response.fail_reason`；`errorPaths: ["error.code"]`。
- 视频地址 coalesce：`response.video_url` → `response.videoUrl` → `response.result_url` → `response.url` → `response.data.video_url` → `response.output.url`；`resultEphemeral: true`，宿主立即下载持久化。
- HTTP 400/401/403/429/5xx 为真实失败上抛，不包装成成功。

## 兼容边界

- 同一视频不重复 POST：宿主恢复合同保证已有任务 ID 只查询不重建（上游错误处理章节要求）。
- 参考素材须为公网 HTTPS URL 或 data URL（`requiresPublicMediaUrls: true`），裸 Base64/内网地址不可用。
- 不发送 multipart `input_reference`（JSON `reference_images` 路径覆盖全部用例）。
- 画幅统一 `ratio` 单字段，`size` 与 `ratio` 不会同时发送。
- 计费按条：基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响，以 yuxibox 控制台为准。
- 真实 Key 的付费生成尚待用户验收。
````

- **Step 3: 回填 manifest documentation 并打包** <!-- comet-task-ref:a31f5d7e-dd40-4159-a1c0-c5c15aad18dd -->

```bash
cd plugin-packages && node embed-documentation.mjs yuxibox-seedance-video && sh build-packages.sh
```

注意：`build-packages.sh` 全量打包所有插件并编译支付后端二进制，耗时较长；只想重建本插件包时，可手动等价执行 zip（与 `package_plugin` 保持一致——从包目录内 `find manifest.json README.md docs assets web backend LICENSE -type f | LC_ALL=C sort | zip -X -q ../yuxibox-seedance-video.yingce-plugin.tmp -@` 后 `mv`）。全量脚本是最简可靠路径，优先使用。

预期：
1. `manifest.json` 的 `documentation` 字段包含 README 与 interface.md 全文（含 manifest 契约块）。
2. `plugin-packages/yuxibox-seedance-video.yingce-plugin` 生成。

- **Step 4: 验证包内容** <!-- comet-task-ref:a31f5d7e-dd40-4159-a1c0-c5c15aad18dd -->

```bash
unzip -l plugin-packages/yuxibox-seedance-video.yingce-plugin
```

预期：含 `manifest.json`、`README.md`、`docs/interface.md` 三项。

再验证 documentation 回填：

```bash
node -e "const m=require('./plugin-packages/yuxibox-seedance-video/manifest.json');console.log(m.documentation.includes('yuxibox Seedance 接口字段'),m.documentation.includes('YINGCE_MANIFEST_CONTRACT_START'),m.documentation.length)"
```

预期输出：`true true <正长度>`。

- **Step 5: Commit** <!-- comet-task-ref:a31f5d7e-dd40-4159-a1c0-c5c15aad18dd -->

```bash
git add plugin-packages/yuxibox-seedance-video/
git commit -m "docs(video): yuxibox seedance2.5 - 插件 README、接口文档与打包"
```

注意：`.yingce-plugin` 包是否入库遵循仓库现状——`plugin-packages/*.yingce-plugin` 在 git 中已跟踪（见 metaso/fisher 先例），正常 `git add`；若 `git status` 显示包文件无变化（构建产物被忽略），跳过即可。

---

### Task 3: 后端注册与能力默认值（tasks 2.1、2.2）

**Files:**
- Modify: `backend/internal/generation/registry.go:57-86`（`OfficialDeclarativeVideoInterface` switch）
- Modify: `backend/internal/app/model_capability_defaults.go:171-239`（`DefaultModelCapabilityConfigForModel` switch）

**Interfaces:**
- Consumes: Task 1/2 的插件包（`ensureOfficialProtocolAdapter` → `LoadOfficialFallbackRegistry().Resolve("yuxibox-seedance-video")`，无需新代码）。
- Produces: `OfficialDeclarativeVideoInterface("yuxibox-seedance-video") == ("yuxibox Seedance", true)`；`DefaultModelCapabilityConfig("yuxibox-seedance-video")` 返回设计文档 2.2 节的 VideoCapabilityConfig。Task 5 契约测试与 Task 4 前端能力映射依赖这两处。

- **Step 1: registry.go 注册显示名** <!-- comet-task-ref:a0cc38d4-3b07-4515-948d-c03eecc231c9 -->

在 `backend/internal/generation/registry.go` 的 `OfficialDeclarativeVideoInterface` switch 中，`case "metaso-minimax-video": return "密塔 H3", true`（L65-66）之后插入：

```go
	case "yuxibox-seedance-video":
		return "yuxibox Seedance", true
```

- **Step 2: model_capability_defaults.go 添加能力默认值 case** <!-- comet-task-ref:936004e0-1b61-41e5-8404-2d6919622c05 -->

在 `backend/internal/app/model_capability_defaults.go` 的 `DefaultModelCapabilityConfigForModel` switch 中，`case "metaso-minimax-video":` 块（L180-190）之后插入（设计文档 2.2 节骨架，补齐注释与字段名核对）：

```go
	case "yuxibox-seedance-video":
		// yuxibox 中转 seedance2.5：固定 720P（不暴露分辨率选择），统一 ratio 单字段，
		// 时长 4–30 默认 30；audio_to_video 依据上游 audios 独立数组开放，
		// 纯音频+文本若被上游拒绝再收紧（见设计文档风险表）。
		video.Operations = append(video.Operations, "reference_to_video", "audio_to_video")
		video.References.PromptMaxChars = 16000
		video.References.MaxImages, video.References.MaxVideos, video.References.MaxAudios = 30, 10, 10
		video.Duration = VideoDurationConfig{Selection: "range", Min: 4, Max: 30, Step: 1, Default: 30}
		video.Ratios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}
		video.DefaultRatio = "16:9"
		video.Resolutions, video.DefaultResolution = []string{"720p"}, "720p"
```

字段核对：`VideoReferenceConfig` 有 `PromptMaxChars/MaxImages/MaxVideos/MaxAudios`（metaso case L184-185 同款赋值方式）；`VideoDurationConfig{Selection, Min, Max, Step, Default}`（fisher case L177 同款）；GenerateAudio/Watermark 保持基座默认（L166-167 已是 Supported false），不写。字节上限（MaxImageBytes 等）沿用基座 30MB 图片上限，不覆盖。

- **Step 3: 构建验证** <!-- comet-task-ref:936004e0-1b61-41e5-8404-2d6919622c05 -->

```bash
go -C backend build ./...
```

预期：编译通过，无输出。

- **Step 4: Commit** <!-- comet-task-ref:a0cc38d4-3b07-4515-948d-c03eecc231c9 -->

```bash
git add backend/internal/generation/registry.go backend/internal/app/model_capability_defaults.go
git commit -m "feat(video): yuxibox seedance2.5 - 注册官方声明式接口与能力默认值"
```

---

### Task 4: 后端契约测试（tasks 2.3）

**Files:**
- Test: `backend/internal/protocol/yuxibox_seedance_video_test.go`（新增，仿 `metaso_minimax_video_test.go`）

**Interfaces:**
- Consumes: Task 2 的 `yuxibox-seedance-video.yingce-plugin`；同包既有测试助手 `officialPackageAdapter(t, packageName, providerID)`（`official_catalog_test.go:534`）、`manifestTestBody(t, spec)`（`official_catalog_test.go:557`）；类型 `GenerationRequest`/`MediaReference`/`RequestContext`/`PollContext`/`Status*`。
- Produces: `yuxiboxRequest()` 测试基座函数与四个 Test 函数，覆盖设计文档 4.1 节测试清单。Task 6 集成验证复跑。

- **Step 1: 写契约测试（先跑确认对 Task 3 的注册已生效、对 manifest 行为的断言可失败）** <!-- comet-task-ref:3bae58db-0aaa-48c6-addd-a65ca9ac68e6 -->

创建 `backend/internal/protocol/yuxibox_seedance_video_test.go`：

```go
package protocol

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func yuxiboxRequest() GenerationRequest {
	return GenerationRequest{Model: "seedance2.5-yx-count", Prompt: "少女在雨巷中转身回望。", Duration: 30, AspectRatio: "16:9"}
}

func TestYuxiboxSeedancePackage(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-seedance-video.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-seedance-video", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "yuxibox Seedance" || provider.BaseURL != "https://yuxibox.cn" || !provider.RequiresPublicMediaURLs || provider.Auth.Type != "bearer" || len(workflows) != 1 || workflows[0].ProviderID != "yuxibox-seedance-video" {
		t.Fatalf("yuxibox identity/workflow = %#v, %#v", provider, workflows)
	}
	if workflows[0].Defaults["model"] != "seedance2.5-yx-count" || workflows[0].Defaults["duration"] != float64(30) || workflows[0].Defaults["aspectRatio"] != "16:9" {
		t.Fatalf("yuxibox workflow defaults = %#v", workflows[0].Defaults)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("yuxibox workflow parameters differ from provider contract")
	}
	parameterNames := make(map[string]bool)
	for _, parameter := range provider.Parameters {
		parameterNames[parameter.Name] = true
	}
	for _, unsupported := range []string{"resolution", "generateAudio", "watermark"} {
		if parameterNames[unsupported] {
			t.Errorf("yuxibox advertises unsupported parameter %s", unsupported)
		}
	}
	if !parameterNames["duration"] {
		t.Error("yuxibox duration parameter missing")
	}
}

func TestYuxiboxSeedanceCreateAssembly(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	// 文生视频：seconds 字符串化、ratio 缺省 16:9、空素材组不出现字段键。
	textOnly := yuxiboxRequest()
	textOnly.AspectRatio = ""
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: textOnly})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v1/videos" || spec.Method != "POST" || spec.Auth.Type != "bearer" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "seedance2.5-yx-count" || body["prompt"] != "少女在雨巷中转身回望。" || body["seconds"] != "30" || body["ratio"] != "16:9" {
		t.Fatalf("text-only parameters = %#v", body)
	}
	for _, key := range []string{"reference_images", "videos", "audios", "size", "resolution", "input_reference"} {
		if _, exists := body[key]; exists {
			t.Fatalf("unexpected field %s", key)
		}
	}
	// 多模态：三组素材按序映射 URL。
	request := yuxiboxRequest()
	request.AspectRatio = "adaptive"
	request.Images = []MediaReference{{URL: "https://example.com/ref-1.jpg"}, {URL: "https://example.com/ref-2.jpg"}}
	request.Videos = []MediaReference{{URL: "https://example.com/ref.mp4"}}
	request.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav"}}
	spec, err = adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if body["ratio"] != "adaptive" || body["seconds"] != "30" {
		t.Fatalf("multimodal parameters = %#v", body)
	}
	images := body["reference_images"].([]any)
	if len(images) != 2 || images[0] != "https://example.com/ref-1.jpg" || images[1] != "https://example.com/ref-2.jpg" {
		t.Fatalf("reference_images = %#v", images)
	}
	if body["videos"].([]any)[0] != "https://example.com/ref.mp4" || body["audios"].([]any)[0] != "https://example.com/dialogue.wav" {
		t.Fatalf("videos/audios = %#v, %#v", body["videos"], body["audios"])
	}
}

func TestYuxiboxSeedanceValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model renamed", func(r *GenerationRequest) { r.Model = "seedance2.5" }},
		{"prompt empty", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"prompt too long", func(r *GenerationRequest) { r.Prompt = string(make([]byte, 16001)) }},
		{"duration low", func(r *GenerationRequest) { r.Duration = 3 }},
		{"duration high", func(r *GenerationRequest) { r.Duration = 31 }},
		{"ratio not in tier", func(r *GenerationRequest) { r.AspectRatio = "2:3" }},
		{"thirty one images", func(r *GenerationRequest) {
			for i := 0; i < 31; i++ {
				r.Images = append(r.Images, MediaReference{URL: "https://example.com/x.jpg"})
			}
		}},
		{"eleven videos", func(r *GenerationRequest) {
			for i := 0; i < 11; i++ {
				r.Videos = append(r.Videos, MediaReference{URL: "https://example.com/x.mp4"})
			}
		}},
		{"eleven audios", func(r *GenerationRequest) {
			for i := 0; i < 11; i++ {
				r.Audios = append(r.Audios, MediaReference{URL: "https://example.com/x.wav"})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := yuxiboxRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid yuxibox request accepted")
			}
		})
	}
}

func TestYuxiboxSeedancePollAndResponse(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"t1","status":"queued"}`))
	if err != nil || created.TaskID != "t1" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// 追踪 request_id 不得成为任务编号（metaso 测试同款断言）。
	// 缺任务 ID 的“结果不确定”由声明式宿主 provider_protocol.go 抛出
	//（extractProviderTaskID 兜底后仍为空 → errors.New(“声明式协议创建请求没有返回任务 ID”)），
	// 插件层 ParseCreate 只保证不把无关编号当任务 ID。
	tracked, err := adapter.ParseCreate(context.Background(), []byte(`{"request_id":"trace-123"}`))
	if err != nil || tracked.TaskID != "" {
		t.Fatalf("tracking ID must not become task ID: %#v, %v", tracked, err)
	}
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v1/videos/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
	// 状态归一与多字段 URL coalesce。
	for _, tc := range []struct {
		payload string
		status  Status
		message string
	}{
		{`{"status":"queued"}`, StatusPending, ""},
		{`{"status":"running"}`, StatusProcessing, ""},
		{`{"status":"failed","fail_reason":"rejected"}`, StatusFailed, "rejected"},
		{`{"error":{"code":"401","message":"invalid api key"}}`, StatusFailed, "invalid api key"},
		{`{"status":"cancelled"}`, StatusCancelled, ""},
		{`{"status":"succeeded","url":"https://example.com/result.mp4"}`, StatusSucceeded, ""},
		{`{"status":"succeeded","video_url":"https://example.com/alt.mp4"}`, StatusSucceeded, ""},
		{`{"status":"succeeded","output":{"url":"https://example.com/output.mp4"}}`, StatusSucceeded, ""},
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(tc.payload))
		if err != nil || result.Status != tc.status || result.TaskID != "task-123" || result.Message != tc.message {
			t.Fatalf("poll %s = %#v, %v", tc.payload, result, err)
		}
		if tc.status == StatusSucceeded && (result.Result == nil || len(result.Result.Videos) != 1) {
			t.Fatalf("successful result = %#v", result)
		}
	}
}
```

要点核对：
- import 含 `"bytes"`（`TestYuxiboxSeedancePackage` 用 `bytes.Equal`，与 metaso 测试 L4 一致）。
- `provider.Label`/`provider.BaseURL`/`provider.RequiresPublicMediaURLs`/`provider.Auth.Type`/`workflows[0].Defaults`/`reflect.DeepEqual(workflows[0].Parameters, provider.Parameters)` 均为 metaso 测试同款既有断言面。
- 上游 status 词表未实测：`running`/`cancelled` 归一依赖宿主既有状态机，与 metaso 一致；若真实上游使用其他词（如 `processing`），验收时只需在响应映射补状态映射，测试同步调整（设计文档风险表）。
- “结果不确定”语义是宿主 `provider_protocol.go` 行为（非插件代码）：manifest taskId 链提取为空 → `extractProviderTaskID` 再兜底 → 仍为空则 `errors.New("声明式协议创建请求没有返回任务 ID")` 上抛，不自动重发；契约测试只锁定“无关编号不得成为任务 ID”。

- **Step 2: 运行测试** <!-- comet-task-ref:3bae58db-0aaa-48c6-addd-a65ca9ac68e6 -->

```bash
go -C backend test ./internal/protocol/ -run YuxiboxSeedance -v
```

预期：4 个 Test 全部 PASS（含 9 个 validation 子测试）。若 `TestYuxiboxSeedancePackage` 因 manifest 与包内 manifest 不一致失败，回 Task 2 Step 3 重新 embed + 打包。

- **Step 3: Commit** <!-- comet-task-ref:3bae58db-0aaa-48c6-addd-a65ca9ac68e6 -->

```bash
git add backend/internal/protocol/yuxibox_seedance_video_test.go
git commit -m "test(video): yuxibox seedance2.5 - 插件契约测试"
```

---

### Task 5: 前端渠道预设与能力映射（tasks 3.1、3.2、3.3）

**Files:**
- Modify: `web/src/stores/use-config-store.ts:527-529`（`createMetasoChannel` 之后新增 `createYuxiboxChannel`）
- Modify: `web/src/pages/settings/channel-settings-pane.tsx:65-66`（`addChannel` preset 类型与分支）、`web/src/pages/settings/channel-settings-pane.tsx:187`（按钮，"添加密塔 H3"之后）
- Modify: `web/src/lib/model-capabilities.ts:488`（metaso 分支之后新增 yuxibox 分支）

**Interfaces:**
- Consumes: `createModelChannel`（`use-config-store.ts:500`）、`defaultModelCapabilityConfig(protocol, model)`（`model-capabilities.ts`，import 已存在于 `use-config-store.ts`）；`ModelProtocol` 为 `string` 别名，无需在 `model-protocols.ts` 注册新类型。
- Produces: 导出函数 `createYuxiboxChannel(): ModelChannel`；preset 标识 `"yuxibox"`；`defaultModelCapabilityConfig("yuxibox-seedance-video", ...)` 的 video 档位（与 Task 3 后端默认值逐字段对齐）。Task 6 构建验证与用户手动验收依赖。

- **Step 1: use-config-store.ts 新增 createYuxiboxChannel** <!-- comet-task-ref:296b3313-145d-46d4-973b-d43378590a54 -->

在 `createMetasoChannel()`（L527-529）之后插入（设计文档 2.3 节骨架）：

```ts
export function createYuxiboxChannel(): ModelChannel {
    return createModelChannel({ name: "yuxibox Seedance", baseUrl: "https://yuxibox.cn", apiKey: "", models: ["seedance2.5-yx-count"], modelCosts: [{ model: "seedance2.5-yx-count", displayName: "Seedance 2.5 (yuxibox)", capability: "video", protocol: "yuxibox-seedance-video", billingMode: "fixed_request", unitPriceMicrocredits: 0, capabilityConfig: defaultModelCapabilityConfig("yuxibox-seedance-video", "seedance2.5-yx-count"), description: "yuxibox seedance2.5：4–30 秒（默认 30）、固定 720P、七档比例；按条计费基础价 4.5 元/条，实际以 yuxibox 控制台与令牌分组倍率为准。" }] });
}
```

字段核对：`ModelCost` 类型含 `model/displayName/capability/protocol/billingMode/unitPriceMicrocredits/capabilityConfig/description`（`use-config-store.ts:100-106` 与 fisher/metaso 同款）；`billingMode: "fixed_request"` 是合法联合类型成员（`use-config-store.ts:104`）。

- **Step 2: channel-settings-pane.tsx 加 preset 与按钮** <!-- comet-task-ref:5883bcdc-a517-4e95-8057-91e416ac26d6 -->

改动 1——import（L10-20 import 块内，`createMetasoChannel` 之后加一行）：

```tsx
    createYuxiboxChannel,
```

改动 2——`addChannel`（L65-66）：

```tsx
    const addChannel = (preset?: "fisher" | "metaso" | "yuxibox") => {
        const channel = preset === "fisher" ? createFisherH3Channel() : preset === "metaso" ? createMetasoChannel() : preset === "yuxibox" ? createYuxiboxChannel() : createModelChannel({ name: `渠道 ${userChannels.length + 1}` });
```

改动 3——按钮（L187 `添加密塔 H3` 按钮之后加一行）：

```tsx
                    <Button className="h-10 sm:h-8" onClick={() => addChannel("yuxibox")}>添加 yuxibox Seedance</Button>
```

- **Step 3: model-capabilities.ts 新增 yuxibox 分支** <!-- comet-task-ref:3d36f574-6a4a-460d-a54e-78ec136ba2c5 -->

在 `if (protocol === "metaso-minimax-video") {...}` 块（L480-488）之后、`if (protocol === "minimax-video" || protocol === "metaso-h3")` 块之前插入（与 Task 3 后端默认值逐字段对齐）：

```ts
    if (protocol === "yuxibox-seedance-video") {
        Object.assign(video.references, { promptMaxChars: 16000, maxImages: 30, maxVideos: 10, maxAudios: 10 });
        video.duration = { selection: "range", min: 4, max: 30, step: 1, default: 30 };
        video.ratios = ["adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];
        video.defaultRatio = "16:9";
        video.resolutions = ["720p"];
        video.defaultResolution = "720p";
        video.operations.push("reference_to_video", "audio_to_video");
    }
```

前后端对齐核对表（后端 Task 3 ↔ 前端本步）：

| 字段 | 后端（Go） | 前端（TS） |
| --- | --- | --- |
| promptMaxChars | 16000 | 16000 |
| maxImages/maxVideos/maxAudios | 30 / 10 / 10 | 30 / 10 / 10 |
| duration | range 4–30 step 1 default 30 | range 4–30 step 1 default 30 |
| ratios | 七档 + DefaultRatio "16:9" | 七档 + defaultRatio "16:9" |
| resolutions | ["720p"] / "720p" | ["720p"] / "720p" |
| generateAudio/watermark | 基座默认（不支持） | 基座默认 `{ supported: false, default: false }`（L437-438，不覆盖） |
| operations | + reference_to_video, audio_to_video | + reference_to_video, audio_to_video |

说明：resolutions 值统一小写 `"720p"`（与后端 Task 3 一致；仓库惯例是同一协议前后端同大小写——metaso 前后端均大写、jimeng/veo 前后端均小写，jimeng 的 `["720p"]` 固定单档是与本渠道最同构的先例，见 `model_capability_defaults.go:193` 与 `model-capabilities.ts:443-444`）。前端 `Object.assign(video.references, ...)` 只覆盖写出的四个键，maxImageBytes/maxVideoBytes 等保持基座值，与后端"字节上限沿用宿主默认"对齐。

- **Step 4: 构建验证** <!-- comet-task-ref:3d36f574-6a4a-460d-a54e-78ec136ba2c5 -->

```bash
cd web && bun run build
```

预期：构建成功，无类型错误（覆盖 `createYuxiboxChannel` 返回结构、preset 分支联合类型、能力映射字段）。

- **Step 5: Commit** <!-- comet-task-ref:296b3313-145d-46d4-973b-d43378590a54 -->

```bash
git add web/src/stores/use-config-store.ts web/src/pages/settings/channel-settings-pane.tsx web/src/lib/model-capabilities.ts
git commit -m "feat(video): yuxibox seedance2.5 - 前端渠道预设、按钮与能力映射"
```

---

### Task 6: 文档更新与待验收登记（tasks 4.1、4.2）

**Files:**
- Modify: `docs/content/docs/backend/http-api.mdx`（渠道协议说明；已跟踪文件）
- Modify: `docs/content/docs/progress/pending-test.mdx`（待验收登记；已跟踪文件）

**Interfaces:**
- Consumes: Task 1-5 的协议事实（端点、鉴权、档位、计费模式）。
- Produces: 文档站可构建的协议说明；待验收条目（用户真实 Key 浏览器验收的依据）。

- **Step 1: http-api.mdx 补 yuxibox 协议说明** <!-- comet-task-ref:2101c9ec-c45f-4963-bf70-954ba2a59ef2 -->

在 `docs/content/docs/backend/http-api.mdx` 的渠道协议章节（搜索既有 `metaso`/`fisher` 协议说明小节，按相同层级与格式追加）：

```markdown
### yuxibox Seedance（`yuxibox-seedance-video`）

yuxibox（yuxibox.cn）中转 seedance2.5-yx-count 的声明式视频协议插件。

- 端点：创建 `POST {base}/v1/videos`（JSON），查询 `GET {base}/v1/videos/{task_id}`；无取消接口。
- 鉴权：Bearer API Key（配置字段 `apiKey`）。
- 档位：时长 4–30 秒（默认 30）；画幅七档 `adaptive/16:9/9:16/1:1/4:3/3:4/21:9`（统一 `ratio` 单字段，不发 `size`）；输出固定 720P，不暴露分辨率选择。
- 参考：`reference_images` ≤ 30、`videos` ≤ 10、`audios` ≤ 10；素材须为公网 HTTPS 或 data URL。
- 计费：按条计费（`fixed_request`），基础价 4.5 元/条；最终费用受令牌分组倍率及请求规格影响，以 yuxibox 控制台为准。
- 恢复合同：同一视频不重复 POST，已有任务 ID 只查询不重建；创建响应缺失任务 ID 按"结果不确定"失败。
```

（以文档中既有协议小节的实际标题层级与 front-matter 为准，保持同一格式；不确定层级时先看 metaso/fisher 小节的标题深度。）

- **Step 2: pending-test.mdx 登记待验收** <!-- comet-task-ref:820c4f43-edaf-4aed-82e0-f8f1a82caa4b -->

在 `docs/content/docs/progress/pending-test.mdx` 按既有条目格式追加：

```markdown
- **yuxibox seedance2.5 视频渠道**：设置页"添加 yuxibox Seedance"按钮 → 渠道卡片档位（时长 4–30 默认 30、七档比例、无分辨率选择、参考上限 30/10/10）→ 填入真实 Key 后画布发起文生视频与参考生视频，核对任务创建/轮询/结果下载与账单扣费（4.5 元/条基础价 × 分组倍率）。待用户执行浏览器验收。
```

- **Step 3: 文档站类型检查** <!-- comet-task-ref:2101c9ec-c45f-4963-bf70-954ba2a59ef2 -->

```bash
cd docs && bun run types:check
```

预期：通过，无 MDX/front-matter 类型错误。

- **Step 4: Commit** <!-- comet-task-ref:2101c9ec-c45f-4963-bf70-954ba2a59ef2 -->

注意：这两个 mdx 是 gitignore 白名单中已跟踪的文件，正常 `git add` 即可。

```bash
git add docs/content/docs/backend/http-api.mdx docs/content/docs/progress/pending-test.mdx
git commit -m "docs(video): yuxibox seedance2.5 - 渠道协议说明与待验收登记"
```

---

### Task 7: 集成验证与收尾（tasks 5.1、5.2、5.3）

**Files:**
- 无代码改动；仅运行验证并提交 change 产物。

**Interfaces:**
- Consumes: Task 1-6 全部产物。
- Produces: 全绿的集成验证结果；change 产物（openspec change 目录与本计划）入库。

- **Step 1: 后端集成测试** <!-- comet-task-ref:714f3d74-33b3-4cb3-8561-c10eaadac1e4 -->

```bash
go -C backend test ./internal/protocol/ ./internal/generation/ ./internal/app/
```

预期：全部 PASS（含既有用例与 `TestYuxiboxSeedance*` 四个新用例）。

- **Step 2: 前端构建** <!-- comet-task-ref:f13eedce-4925-4551-94b0-05bcbbf225ac -->

```bash
cd web && bun run build
```

预期：构建成功，无类型错误。

- **Step 3: OpenSpec 校验** <!-- comet-task-ref:067ba9e1-0386-4281-94fe-e4ed10b14226 -->

在 docs 根（`docs/openspec` 所在仓库根目录，即工作区根 `E:\AI_Vedio_Things\new-toonflow\open-ai-canvas-flow`）执行：

```bash
openspec validate add-yuxibox-seedance-video-channel
```

预期：通过（4 需求 12 场景齐备）。

- **Step 4: 提交 change 产物（gitignore 处理）** <!-- comet-task-ref:f13eedce-4925-4551-94b0-05bcbbf225ac -->

`docs/openspec/changes/add-yuxibox-seedance-video-channel/` 与 `docs/superpowers/plans/`、`docs/superpowers/specs/` 命中 `docs/*` 忽略规则，需 `git add -f`：

```bash
git add -f docs/openspec/changes/add-yuxibox-seedance-video-channel/ docs/superpowers/plans/2026-10-18-yuxibox-seedance-video.md docs/superpowers/specs/2026-10-18-yuxibox-seedance-video-design.md
git commit -m "docs(video): yuxibox seedance2.5 - 登记 openspec change、设计与实施计划"
```

- **Step 5: 汇总交付** <!-- comet-task-ref:067ba9e1-0386-4281-94fe-e4ed10b14226 -->

向用户报告：
1. 改动清单（插件包、后端两处 case + 测试、前端三处、两份文档）。
2. 验证结果（后端测试、前端构建、openspec validate 输出如实记录）。
3. 未覆盖项：真实 Key 浏览器验收（已登记 pending-test.mdx，待用户执行）；上游完成态响应字段名与纯音频生视频行为未实测，验收差异只需改 manifest 响应映射或能力默认值 Operations 一处。

---

## Self-Review 结论

- **Spec 覆盖**：delta spec 4 需求 12 场景——协议身份（Task 1/3/5）、参数档位（Task 1 validations + Task 3/5 能力默认值 + Task 4 校验测试）、参考素材映射（Task 1 create + Task 4 assembly 测试）、异步查询与恢复合同（Task 1 response + Task 4 poll/response 测试；恢复合同为宿主既有行为，无新代码）——全部有对应任务。
- **tasks.md 14 项**：1.1→Task 1、1.2→Task 2、1.3→Task 2、2.1/2.2→Task 3、2.3→Task 4、3.1/3.2/3.3→Task 5、4.1/4.2→Task 6、5.1/5.2/5.3→Task 7，无遗漏。
- **类型一致性**：`createYuxiboxChannel`（use-config-store.ts）↔ preset `"yuxibox"`（channel-settings-pane.tsx）↔ `defaultModelCapabilityConfig("yuxibox-seedance-video", ...)`（model-capabilities.ts）↔ `case "yuxibox-seedance-video"`（model_capability_defaults.go）↔ provider id（generate-catalog.mjs）↔ `officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")`（契约测试）命名一致。
- **设计文档 2.4 节**：确认不新增前端 provider（`video.ts`/`video-contracts.ts` 不在 File Structure 中）。
