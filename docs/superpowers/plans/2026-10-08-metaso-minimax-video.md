---
change: metaso-minimax-video
design-doc: docs/superpowers/specs/2026-10-08-metaso-minimax-video-design.md
base-ref: 2bf738bcfe86f71fdfc017547181da4a49bbb7aa
---

# 密塔 H3（Metaso MiniMax V2 中转）视频渠道实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `metaso-minimax-video` 声明式协议插件（metaso.cn MiniMax V2 中转），完成后端注册 + 前端 provider/能力/渠道预设，使画布与创作页可通过密塔渠道完成文生视频、图生视频（首帧）和多模态参考生视频。

**Architecture:** 协议知识全部收敛进插件 manifest（由 `plugin-packages/generate-catalog.mjs` 生成，Fisher 同构但去掉 `audio_control`/`start_time_seconds`，分辨率 480P/768P/1080P/2K、时长 4–15 enum、六档固定比例）。后端仅两处 case：`generation/registry.go` 注册显示名、`app/model_capability_defaults.go` 提供能力默认值；创建/轮询/下载全部复用 `provider_protocol.go` 声明式宿主。前端仿 `video-provider-minimax.ts` 新增轻量 provider + 纯函数 payload 构建，`video.ts` 加 create/poll 分发行。

**Tech Stack:** Node ESM 脚本生成 manifest → `.yingce-plugin` zip 包；Go 1.25 声明式协议适配器（`backend/internal/protocol`）；TypeScript + Bun test（`web/`）。

## Global Constraints

- 协议 ID 一律为 `metaso-minimax-video`；前端任务 provider 标识为 `metaso-minimax`。
- Base URL：`https://metaso.cn/api/minimax`；创建 `POST /v2/video_generation`，查询 `GET /v2/query/video_generation/{{taskId}}`，Bearer 鉴权（field `apiKey`）。
- 参数档位（后端 manifest 校验、前端纯函数校验、`model_capability_defaults.go`、前端 `model-capabilities.ts` 四处一致）：`model` 固定 `MiniMax-H3`；`resolution` ∈ {480P, 768P, 1080P, 2K}（`$upper` 归一，2K 原样透传，无 adaptive）默认 768P；`duration` 4–15 整数秒默认 5；`ratio` ∈ {21:9, 16:9, 4:3, 1:1, 3:4, 9:16} 默认 16:9；提示词非空且 ≤ 10000 字符。
- content[] 组装：`text(prompt)` + `image_url`（role 为 `coalesce(media.role,"first_frame")`）+ `video_url`（role 固定 `reference_video`，**无** `start_time_seconds`）+ `audio_url`（role 为 `coalesce(media.role,"reference_audio")`）。无 `audio_control`、无 `aigc_watermark`、无 `generate_audio`。
- 素材上限：首帧 ≤ 1、参考图 ≤ 9、参考视频 ≤ 3、参考音频 ≤ 3；图片/音频 role 白名单校验；无媒体且未选比例时拒绝（文生视频必须指定固定比例）。
- 响应映射：`taskId = coalesce(response.task_id, taskId)`；`status = coalesce(response.task.status, "pending")`；`message = coalesce(response.task.error.message, response.error.message, response.base_resp.status_msg, response.task.message)`；`videos = response.task.content.url`；`errorPaths = ["task.error.code", "error.type"]`；`resultEphemeral = true`。
- 创建响应缺 `task_id`：前端报“创建结果不确定，请勿直接重发付费任务”，不自动重发；只读取 `task_id`，不把 `request_id`/含 Key 的编号当任务 ID。
- 插件声明 `requiresPublicMediaUrls: true`；素材无公网 URL 时由宿主 requireURL 策略拒绝，不静默转存。
- 不改数据库 schema；不改 `provider_protocol.go`/轮询器/SSRF 出站；不加 Fisher 式宿主校验层；不暴露 adaptive；不接密塔文本/图片。
- 中文注释只解释非直观原因；不使用真实 Key、不启动 dev server；验证命令按本计划各步骤执行。
- Git 提交说明格式：`<type>(<scope>): <业务模块> - <变更摘要>`（如 `feat(video): 密塔 H3 - 新增声明式协议插件`）。

## File Structure

```
plugin-packages/
  generate-catalog.mjs                      # 修改：新增 metasoParams + add({...}) 定义段
  metaso-minimax-video/                     # 生成（脚本产物，不手写）
    manifest.json                           # 生成
    README.md                               # 生成骨架，补写密塔中转说明
    docs/interface.md                       # 生成，docsFor(spec) 自动产出协议文档
  metaso-minimax-video.yingce-plugin        # build-packages.sh 产物
backend/internal/generation/registry.go     # 修改：OfficialDeclarativeVideoInterface 加 case
backend/internal/app/model_capability_defaults.go  # 修改：加 metaso-minimax-video case
backend/internal/protocol/metaso_minimax_video_test.go  # 新增：插件契约测试
web/src/lib/metaso-minimax-video.ts         # 新增：纯函数 payload 构建 + 档位校验
web/src/lib/metaso-minimax-video.test.ts    # 新增：纯函数测试
web/src/services/api/video-provider-metaso.ts  # 新增：create/poll provider
web/src/services/api/video-contracts.ts     # 修改：provider 联合类型加 "metaso-minimax"
web/src/services/api/video.ts               # 修改：create/poll 分发行
web/src/lib/model-capabilities.ts           # 修改：能力映射
web/src/stores/use-config-store.ts          # 修改：defaultBaseUrlForChannelInterface + createMetasoChannel
web/src/pages/settings/channel-settings-pane.tsx  # 修改：preset "metaso" 按钮
web/test/metaso-minimax-api.test.ts         # 新增：provider 集成测试（bun:test）
docs/content/docs/backend/http-api.mdx      # 修改：密塔协议说明
docs/content/docs/progress/pending-test.mdx # 修改：登记待浏览器验收
```

---

<!-- comet-task-ref:83e36e14170b47b0a62c9dc5e825aebf -->
<!-- comet-task-ref:5f43f14327fe466fa7ce87a85e7be247 -->
<!-- comet-task-ref:d21364a738a340d38c13aad4f13ef121 -->
### Task 1: 插件定义与 manifest 生成

**Files:**
- Modify: `plugin-packages/generate-catalog.mjs`（Fisher 定义段在 L106–164；`metasoParams` 定义仿 L107–109 `fisherParams`；`add` 段插到 Fisher 段之后，即 L164 `}` 与 L166 `add({ id: "openai-chat-completions" ...` 之间）
- Create（生成产物，不手写）: `plugin-packages/metaso-minimax-video/manifest.json`、`plugin-packages/metaso-minimax-video/README.md`、`plugin-packages/metaso-minimax-video/docs/interface.md`

**Interfaces:**
- Consumes: `generate-catalog.mjs` 顶部的助手（`ref`/`coalesce`/`map`/`sorted`/`upper`/`filter`/`len`/`eq`/`includes`/`gt`/`lte`/`and`/`validEvery` 式 `eq(len(filter(...)), len(ref(path)))` 写法、`videoParams`、`parameters`、`config`、`bearer`、`jsonCreate`、`scopes`）。
- Produces: 插件包 `metaso-minimax-video.yingce-plugin`（源目录含 manifest），provider ID `metaso-minimax-video`，供 Task 2 契约测试和 Task 3 前端引用。manifest 契约即后续所有任务的行为真相。

- Step 1: 在 `generate-catalog.mjs` Fisher 段后新增 metaso 定义段

在 L106 `{ const fisherParams = ... }` 块的结束 `}`（L164）之后插入（与 Fisher 段同层级，作为独立块作用域）：

```js
{
  const metasoParams = videoParams
    .filter(([name]) => !["generateAudio", "watermark"].includes(name))
    .map(([name, type, required, mapping, description]) => [name, type, required || ["duration", "resolution"].includes(name), mapping, description]);
  const metasoImageRole = coalesce(ref("media.role"), "first_frame");
  const metasoAudioRole = coalesce(ref("media.role"), "reference_audio");
  const metasoCountRole = (path, role, fallback) => len(filter(ref(path), "media", eq(coalesce(ref("media.role"), fallback), role)));
  const metasoRatios = ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"];
  const metasoHasReferences = gt(addNumbers(len(ref("request.images")), len(ref("request.videos")), len(ref("request.audios"))), 0);
  const metasoValidEvery = (path, condition) => eq(len(filter(ref(path), "media", condition)), len(ref(path)));
  const metasoIncludes = (value, values) => ({ $in: [value, values] });
  add({
    id: "metaso-minimax-video", providerId: "metaso-minimax-video", name: "MiniMax-H3 (Metaso)", vendor: "Metaso", capability: "video",
    baseUrl: "https://metaso.cn/api/minimax", auth: bearer, params: metasoParams, requiresPublicMediaUrls: true,
    workflows: [{ id: "metaso-minimax-video", label: "MiniMax-H3 (Metaso)", providerId: "metaso-minimax-video", capability: "video", parameters: parameters(metasoParams), defaults: { model: "MiniMax-H3", resolution: "768P", duration: 5, aspectRatio: "16:9" } }],
    notes: "密塔（metaso.cn）是 MiniMax V2 协议中转，创建/查询端点与官方 MiniMax 一致，差异仅在此 Base URL 与参数档位。模型名称精确为 MiniMax-H3，时长 4–15 秒整数，分辨率 480P/768P/1080P/2K（按大写原样透传，2K 为密塔开放档位），六档固定比例；纯文本（无任何参考素材）必须指定固定比例，参考素材存在时缺省 16:9。参考图最多 9 张且首帧至多 1 张（不支持尾帧），参考视频 3 条、参考音频 3 条。密塔不接收 audio_control、start_time_seconds、generate_audio 与 aigc_watermark，插件从源头不发送这些字段。创建只读取 task_id；超时、5xx、无效响应不自动重建，宿主保留 X-Task-Id 后查询。真实 Key 的付费生成尚待用户验收。",
    validations: [
      { assert: eq(ref("request.model"), "MiniMax-H3"), message: "密塔 H3 模型必须为 MiniMax-H3。" },
      { assert: and(gt(len(trim(ref("request.prompt"))), 0), lte(len(ref("request.prompt")), 10000)), message: "密塔 H3 提示词不能为空且最多 10000 字符。" },
      { assert: metasoIncludes(lower(ref("request.resolution")), ["480p", "768p", "1080p", "2k"]), message: "密塔 H3 分辨率仅支持 480P、768P、1080P、2K。" },
      { assert: and(gte(ref("request.duration"), 4), lte(ref("request.duration"), 15)), message: "密塔 H3 时长为 4–15 秒整数。" },
      { assert: metasoIncludes(coalesce(ref("request.aspectRatio"), conditional(metasoHasReferences, "16:9", "")), metasoRatios), message: "密塔 H3 需要固定比例；无参考素材时必须显式指定。" },
      { assert: and(lte(metasoCountRole("request.images", "reference_image", "first_frame"), 9), lte(metasoCountRole("request.images", "first_frame", "first_frame"), 1)), message: "密塔 H3 最多 9 张参考图及 1 张首帧。" },
      { assert: metasoValidEvery("request.images", metasoIncludes(metasoImageRole, ["reference_image", "first_frame"])), message: "密塔 H3 图片角色仅支持 reference_image 和 first_frame。" },
      { assert: and(lte(len(ref("request.videos")), 3), metasoValidEvery("request.videos", eq(coalesce(ref("media.role"), "reference_video"), "reference_video"))), message: "密塔 H3 最多 3 条 reference_video。" },
      { assert: and(lte(len(ref("request.audios")), 3), metasoValidEvery("request.audios", metasoIncludes(metasoAudioRole, ["reference_audio"]))), message: "密塔 H3 最多 3 条 reference_audio。" }
    ],
    create: jsonCreate("/v2/video_generation", {
      model: ref("request.model"),
      content: { $concatArrays: [
        [{ type: "text", text: ref("request.prompt") }],
        map(sorted(ref("request.images")), "media", { type: "image_url", image_url: { url: ref("media.value") }, role: metasoImageRole }),
        map(sorted(ref("request.videos")), "media", { type: "video_url", video_url: { url: ref("media.value") }, role: "reference_video" }),
        map(sorted(ref("request.audios")), "media", { type: "audio_url", audio_url: { url: ref("media.value") }, role: metasoAudioRole })
      ] },
      resolution: { $upper: ref("request.resolution") }, duration: ref("request.duration"), ratio: coalesce(ref("request.aspectRatio"), "16:9")
    }),
    poll: { method: "GET", path: "/v2/query/video_generation/{{taskId}}" },
    response: {
      taskId: coalesce(ref("response.task_id"), ref("taskId")),
      status: coalesce(ref("response.task.status"), "pending"),
      message: coalesce(ref("response.task.error.message"), ref("response.error.message"), ref("response.base_resp.status_msg"), ref("response.task.message")),
      videos: ref("response.task.content.url"), errorPaths: ["task.error.code", "error.type"], resultEphemeral: true
    }
  });
}
```

要点（与 Fisher 段 L107–163 的逐项差异）：
- `validations` 无 audio_control 三条与 `start_time_seconds` 校验；时长上限恒为 15（无条件 `$if`）；分辨率多 `2k`；图片角色白名单无 `last_frame`；无媒体时 `aspectRatio` 兜底为 `""` 以强制显式选择。
- `create.body` 无 `audio_control` 字段；videos map 无 `start_time_seconds`。
- `response.message` 比 Fisher 多一级 `response.base_resp.status_msg` 兜底。

- Step 2: 生成插件目录

Run: `node plugin-packages/generate-catalog.mjs metaso-minimax-video`
Expected: 输出 `generated 1 protocol packages`；`plugin-packages/metaso-minimax-video/` 下出现 `manifest.json`、`README.md`、`docs/interface.md`。`README.md` 是脚本固定骨架，后续 Task 2 覆写正文；`docs/interface.md` 由 `docsFor(spec)` 生成（协议身份、配置字段、统一字段映射、请求模板逐字段、响应映射、兼容边界=notes），无需手改。

- Step 3: 验证 manifest 关键字段

Run: `node -e "const m=require('./plugin-packages/metaso-minimax-video/manifest.json');const p=m.contributes.providers[0];console.log(p.id,p.label,p.baseUrl,p.requiresPublicMediaUrls,p.auth.type,p.poll.path);console.log(JSON.stringify(m.contributes.workflows[0].defaults));console.log('validations',p.validations.length)"`
Expected: `metaso-minimax-video MiniMax-H3 (Metaso) https://metaso.cn/api/minimax true bearer /v2/query/video_generation/{{taskId}}`、`{"model":"MiniMax-H3","resolution":"768P","duration":5,"aspectRatio":"16:9"}`、`validations 9`。

- Step 4: Commit

```bash
git add plugin-packages/generate-catalog.mjs plugin-packages/metaso-minimax-video
git commit -m "feat(video): 密塔 H3 - 新增 metaso-minimax-video 插件目录定义与生成清单"
```

---

<!-- comet-task-ref:83e36e14170b47b0a62c9dc5e825aebf -->
<!-- comet-task-ref:5f43f14327fe466fa7ce87a85e7be247 -->
<!-- comet-task-ref:d21364a738a340d38c13aad4f13ef121 -->
### Task 2: 插件文档与安装包构建

**Files:**
- Modify: `plugin-packages/metaso-minimax-video/README.md`（覆写脚本骨架正文）
- Create: `plugin-packages/metaso-minimax-video.yingce-plugin`（构建产物）

**Interfaces:**
- Consumes: Task 1 生成的 `manifest.json`（`embed-documentation.mjs` 把 README + docs/interface.md 拼接回填进 manifest `documentation`，并嵌入 Manifest 完整接口定义契约块）。
- Produces: 自包含插件包 `metaso-minimax-video.yingce-plugin`，满足 `official_catalog_test.go` 的自包含断言（documentation 全文一致、README/interface 存在、配置字段名出现在 interface 文档中），供 Task 3 与 Task 5 的 Go 测试加载。

- Step 1: 覆写 `plugin-packages/metaso-minimax-video/README.md`

用以下内容整体替换脚本生成的骨架：

```markdown
# MiniMax-H3 (Metaso)

该目录是独立官方协议插件源码。后端从生成的 `metaso-minimax-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

密塔（metaso.cn）提供 MiniMax V2 协议中转：创建与查询端点路径和官方 MiniMax 插件完全一致，差异仅在 Base URL（`https://metaso.cn/api/minimax`）与参数档位（分辨率 480P/768P/1080P/2K、时长 4–15 秒、六档固定比例）。密塔不接收 `audio_control`、`start_time_seconds`、`generate_audio` 与 `aigc_watermark`，本插件从源头不发送这些字段；不建议把密塔渠道配置成官方 `minimax-video` 协议，独立插件保证协议身份与专属默认值不被混淆。

完整接口见 [docs/interface.md](docs/interface.md)。
```

- Step 2: 回填 manifest documentation

Run: `node plugin-packages/embed-documentation.mjs metaso-minimax-video`
Expected: `manifest.json` 的 `documentation` 变为 README 与 interface 拼接全文，并包含 `<!-- YINGCE_MANIFEST_CONTRACT_START -->` 契约块。

- Step 3: 构建安装包

Run: `sh plugin-packages/build-packages.sh`
Expected: 脚本先跑 `embed-documentation.mjs`（无参数 = 全量回填），再对所有 `*/manifest.json` 目录执行 `package_plugin`；`plugin-packages/metaso-minimax-video.yingce-plugin` 出现。

- Step 4: 验证包内容

Run: `unzip -l plugin-packages/metaso-minimax-video.yingce-plugin`（Windows Git Bash 无 unzip 时用 `tar -tf`）
Expected: 列表包含 `manifest.json`、`README.md`、`docs/interface.md`（zip 需已安装；Windows Git Bash 自带）。

- Step 5: Commit

```bash
git add plugin-packages/metaso-minimax-video plugin-packages/metaso-minimax-video.yingce-plugin
git commit -m "build(video): 密塔 H3 - 回填插件文档并构建 yingce-plugin 安装包"
```

---

<!-- comet-task-ref:3b29ec5b16694d0896f11a9d1493cb89 -->
<!-- comet-task-ref:8205ba179722454baf2f26403e56e6f0 -->
<!-- comet-task-ref:1cb0dccc9be140c2bcb41b4c635e347e -->
### Task 3: 后端注册与能力默认值（TDD）

**Files:**
- Modify: `backend/internal/generation/registry.go:57-84`（`OfficialDeclarativeVideoInterface` switch）
- Modify: `backend/internal/app/model_capability_defaults.go:109-166`（`DefaultModelCapabilityConfigForModel` switch）
- Test: `backend/internal/protocol/metaso_minimax_video_test.go`（新增）

**Interfaces:**
- Consumes: Task 1/2 的插件包；`officialPackageAdapter(t, packageName, providerID)` 与 `manifestTestBody(t, spec)` 助手（定义在 `official_catalog_test.go:534/557`，同包直接使用）；`GenerationRequest`/`MediaReference`/`RequestContext`/`PollContext`/`Status*`（`protocol/types.go`）。
- Produces: `OfficialDeclarativeVideoInterface("metaso-minimax-video")` 返回 `("密塔 H3", true)`；`DefaultModelCapabilityConfigForModel` 对密塔返回 4–15 enum 时长、六档比例、480P/768P/1080P/2K 档位、素材上限。`provider_video.go` 声明式优先调度自动生效，无需改动。

- Step 1: 写失败测试 `backend/internal/protocol/metaso_minimax_video_test.go`

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

func metasoRequest() GenerationRequest {
	return GenerationRequest{Model: "MiniMax-H3", Prompt: "A person turns naturally.", Duration: 5, Resolution: "768P", AspectRatio: "16:9"}
}

func TestMetasoMinimaxPackage(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "metaso-minimax-video.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "metaso-minimax-video", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "MiniMax-H3 (Metaso)" || provider.BaseURL != "https://metaso.cn/api/minimax" || !provider.RequiresPublicMediaURLs || provider.Auth.Type != "bearer" || len(workflows) != 1 || workflows[0].ProviderID != "metaso-minimax-video" || workflows[0].Defaults["model"] != "MiniMax-H3" || workflows[0].Defaults["resolution"] != "768P" {
		t.Fatalf("Metaso identity/workflow = %#v, %#v", provider, workflows)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("Metaso workflow parameters differ from provider contract")
	}
}

func TestMetasoMinimaxContentAssembly(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	parameters := make(map[string]Parameter)
	for _, parameter := range adapter.Metadata().Parameters {
		parameters[parameter.Name] = parameter
	}
	for _, unsupported := range []string{"generateAudio", "watermark"} {
		if _, exists := parameters[unsupported]; exists {
			t.Errorf("Metaso advertises unsupported parameter %s", unsupported)
		}
	}
	for _, required := range []string{"duration", "resolution"} {
		if !parameters[required].Required {
			t.Errorf("Metaso parameter %s must be required", required)
		}
	}
	// 文生视频：仅文本条目。
	textOnly := metasoRequest()
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: textOnly})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v2/video_generation" || spec.Method != "POST" || spec.Auth.Type != "bearer" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "MiniMax-H3" || body["resolution"] != "768P" || body["ratio"] != "16:9" || body["duration"] != float64(5) {
		t.Fatalf("text-only parameters = %#v", body)
	}
	content := body["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("text-only content = %#v", content)
	}
	for _, key := range []string{"prompt", "audio_control", "generate_audio", "aigc_watermark", "seed"} {
		if _, exists := body[key]; exists {
			t.Fatalf("unexpected field %s", key)
		}
	}
	// 图生视频：首帧 role；多模态：参考视频 + 参考音频 role；无 start_time_seconds。
	request := metasoRequest()
	request.Resolution = "2K"
	request.Images = []MediaReference{{URL: "https://example.com/first.jpg", Role: "first_frame"}}
	request.Videos = []MediaReference{{URL: "https://example.com/ref.mp4", Role: "reference_video"}}
	request.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav", Role: "reference_audio"}}
	spec, err = adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if body["resolution"] != "2K" {
		t.Fatalf("2K must pass through verbatim: %#v", body)
	}
	content = body["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("multimodal content = %#v", content)
	}
	if content[1].(map[string]any)["role"] != "first_frame" {
		t.Fatalf("first frame role = %#v", content[1])
	}
	video := content[2].(map[string]any)
	if video["role"] != "reference_video" {
		t.Fatalf("video role = %#v", video)
	}
	if _, exists := video["start_time_seconds"]; exists {
		t.Fatalf("Metaso must not send start_time_seconds: %#v", video)
	}
	if content[3].(map[string]any)["role"] != "reference_audio" {
		t.Fatalf("audio role = %#v", content[3])
	}
	// 参考视频未显式指定比例时缺省 16:9 已由 body["ratio"] 断言覆盖。
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v2/query/video_generation/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
}

func TestMetasoMinimaxDefaultsAndValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	// 参考素材存在时比例缺省 16:9、480P 小写归一为大写。
	request := metasoRequest()
	request.Resolution = "480p"
	request.AspectRatio = ""
	request.Images = []MediaReference{{URL: "https://example.com/first.jpg", Role: "first_frame"}}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if body["resolution"] != "480P" || body["ratio"] != "16:9" {
		t.Fatalf("reference defaults = %#v", body)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model", func(r *GenerationRequest) { r.Model = "minimax-h3" }},
		{"prompt", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"prompt too long", func(r *GenerationRequest) { r.Prompt = string(make([]byte, 10001)) }},
		{"resolution", func(r *GenerationRequest) { r.Resolution = "2160p" }},
		{"adaptive", func(r *GenerationRequest) { r.Resolution = "adaptive" }},
		{"duration low", func(r *GenerationRequest) { r.Duration = 3 }},
		{"duration high", func(r *GenerationRequest) { r.Duration = 16 }},
		{"text ratio", func(r *GenerationRequest) { r.AspectRatio = "" }},
		{"bad ratio", func(r *GenerationRequest) { r.AspectRatio = "2:3" }},
		{"two first frames", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "first_frame"}, {Role: "first_frame"}} }},
		{"last frame", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "last_frame"}} }},
		{"ten reference images", func(r *GenerationRequest) {
			for i := 0; i < 10; i++ {
				r.Images = append(r.Images, MediaReference{Role: "reference_image"})
			}
		}},
		{"four videos", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Videos = append(r.Videos, MediaReference{Role: "reference_video"})
			}
		}},
		{"four audios", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Audios = append(r.Audios, MediaReference{Role: "reference_audio"})
			}
		}},
		{"invalid image role", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "unknown"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := metasoRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid Metaso request accepted")
			}
		})
	}
}

func TestMetasoMinimaxResponse(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"task_id":"task-123"}`))
	if err != nil || created.TaskID != "task-123" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// 追踪 request_id 不得成为任务编号。
	tracked, err := adapter.ParseCreate(context.Background(), []byte(`{"request_id":"trace-123"}`))
	if err != nil || tracked.TaskID != "" {
		t.Fatalf("tracking ID must not become task ID: %#v, %v", tracked, err)
	}
	for _, tc := range []struct {
		payload string
		status  Status
		message string
	}{
		{`{"task":{"status":"queued"}}`, StatusPending, ""},
		{`{"task":{"status":"running"}}`, StatusProcessing, ""},
		{`{"task":{"status":"failed","error":{"message":"rejected"}}}`, StatusFailed, "rejected"},
		{`{"error":{"message":"quota exceeded"}}`, StatusFailed, "quota exceeded"},
		{`{"base_resp":{"status_msg":"invalid api key"}}`, StatusFailed, "invalid api key"},
		{`{"task":{"status":"cancelled"}}`, StatusCancelled, ""},
		{`{"task":{"status":"succeeded","content":{"url":"https://example.com/result.mp4"}}}`, StatusSucceeded, ""},
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(tc.payload))
		if err != nil || result.Status != tc.status || result.TaskID != "task-123" || result.Message != tc.message {
			t.Fatalf("poll = %#v, %v", result, err)
		}
		if tc.status == StatusSucceeded && (result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != "https://example.com/result.mp4") {
			t.Fatalf("successful result = %#v", result)
		}
	}
}
```

注意：
- `"bad ratio"` 用例选 `2:3`（Fisher 八档里存在、密塔六档里不存在，避免和 `adaptive` 用例语义重叠）。
- `base_resp.status_msg` 单独一条轮询用例，覆盖密塔特有的兜底链。
- `prompt too long` 用 `make([]byte, 10001)` 构造 10001 字符。

- Step 2: 运行测试确认失败

Run: `cd backend && go test ./internal/protocol/ -run MetasoMinimax`
Expected: FAIL——读不到 `metaso-minimax-video.yingce-plugin`（Task 2 已产出包则 FAIL 在 `provider metaso-minimax-video is missing` 之前的 identity 断言，总之不会 PASS）。若意外 PASS，说明插件 ID 冲突或断言写错，先修测试。

- Step 3: 后端注册 `backend/internal/generation/registry.go`

在 `OfficialDeclarativeVideoInterface` 的 `case "fisher-h3-video":`（registry.go:63-64）之后加：

```go
	case "metaso-minimax-video":
		return "密塔 H3", true
```

说明：密塔是纯字符串接口 ID（Fisher 先例），不加 `model.ChannelInterface*` 常量也能被声明式宿主、模型目录协议字段和前端 `interfaceType` 透传识别。

- Step 4: 后端能力默认值 `backend/internal/app/model_capability_defaults.go`

在 `case "fisher-h3-video":` 块（model_capability_defaults.go:110-117）之后加：

```go
	case "metaso-minimax-video":
		// 密塔与官方 minimax-video 同构：时长用 enum 逐值列出而非 range；Operations 只加
		// reference_to_video（audio_to_video 依赖 Fisher 式驱动音频语义，密塔没有）。
		video.Operations = append(video.Operations, "reference_to_video")
		video.References.PromptMaxChars = 10000
		video.References.MaxImages, video.References.MaxVideos, video.References.MaxAudios = 9, 3, 3
		video.References.MaxVideoBytes, video.References.MaxAudioBytes = 50*1024*1024, 15*1024*1024
		video.References.MaxVideoDuration, video.References.MaxAudioDuration = 15, 15
		video.Duration = VideoDurationConfig{Selection: "enum", Values: []int{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, Default: 5}
		video.Ratios = []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}
		video.Resolutions, video.DefaultResolution = []string{"480P", "768P", "1080P", "2K"}, "768P"
```

说明：`MaxImages=9` 时基线 `MaxImageBytes=30MB` 已足够；字节上限为 0 表示该类媒体不校验（`model_capability_validate.go:308` 语义），参考视频/音频与 MiniMax 官方对齐到 50MB/15MB 并配 15 秒时长上限，避免不限时长的大文件进入轮询。

- Step 5: 运行测试确认通过

Run: `cd backend && go test ./internal/protocol/ -run MetasoMinimax -v`
Expected: 全部 PASS（`TestMetasoMinimaxPackage`、`TestMetasoMinimaxContentAssembly`、`TestMetasoMinimaxDefaultsAndValidation`、`TestMetasoMinimaxResponse` 及子测试）。

- Step 6: 编译验证

Run: `cd backend && go build ./...`
Expected: 无输出，退出码 0。

- Step 7: Commit

```bash
git add backend/internal/generation/registry.go backend/internal/app/model_capability_defaults.go backend/internal/protocol/metaso_minimax_video_test.go
git commit -m "feat(video): 密塔 H3 - 注册声明式接口并添加能力默认值与插件契约测试"
```

---

<!-- comet-task-ref:cf23cc552a674d4bb890dd80a919b871 -->
<!-- comet-task-ref:ce73eef836c3425d91c60a3dfbc90d17 -->
<!-- comet-task-ref:f6456828f8224f01a8e37e51bb981687 -->
<!-- comet-task-ref:b01ecdb7d16145dead7d0b42d7f7de53 -->
### Task 4: 前端纯函数 payload 构建（TDD）

**Files:**
- Create: `web/src/lib/metaso-minimax-video.ts`
- Test: `web/src/lib/metaso-minimax-video.test.ts`

**Interfaces:**
- Consumes: 无外部依赖（纯函数，不 import 运行时模块）。
- Produces: 供 Task 5 provider 使用的精确签名：

```ts
export const METASO_PROTOCOL = "metaso-minimax-video";
export type MetasoMedia = { id: string; url?: string };
export type MetasoImageInput = MetasoMedia & { role?: string };
export type MetasoInput = {
    model: string;
    prompt: string;
    resolution: string;
    duration: string | number;
    ratio?: string;
    images?: MetasoImageInput[];
    videos?: MetasoMedia[];
    audios?: MetasoMedia[];
};
export function buildMetasoPayload(input: MetasoInput): Record<string, unknown>;
```

- Step 1: 写失败测试 `web/src/lib/metaso-minimax-video.test.ts`

```ts
import assert from "node:assert/strict";
import test from "node:test";

// @ts-expect-error -- Node 原生 TypeScript 测试运行器需要保留扩展名。
import { buildMetasoPayload, METASO_PROTOCOL } from "./metaso-minimax-video.ts";

const base = { model: "MiniMax-H3", prompt: "平稳转身", resolution: "768P", duration: 5, ratio: "16:9" };

test("METASO_PROTOCOL 常量与文生视频仅含文本条目", () => {
    assert.equal(METASO_PROTOCOL, "metaso-minimax-video");
    const payload = buildMetasoPayload(base);
    assert.deepEqual(payload, {
        model: "MiniMax-H3",
        content: [{ type: "text", text: "平稳转身" }],
        resolution: "768P",
        duration: 5,
        ratio: "16:9",
    });
});

test("图生视频发送首帧角色，多模态按序组装且无 start_time_seconds", () => {
    const payload = buildMetasoPayload({
        ...base,
        resolution: "2K",
        images: [{ id: "a", url: "https://media.example.com/first.jpg", role: "first_frame" }],
        videos: [{ id: "b", url: "https://media.example.com/ref.mp4" }],
        audios: [{ id: "c", url: "https://media.example.com/dialogue.wav" }],
    }) as Record<string, unknown>;
    assert.equal(payload.resolution, "2K");
    assert.deepEqual(payload.content, [
        { type: "text", text: "平稳转身" },
        { type: "image_url", image_url: { url: "https://media.example.com/first.jpg" }, role: "first_frame" },
        { type: "video_url", video_url: { url: "https://media.example.com/ref.mp4" }, role: "reference_video" },
        { type: "audio_url", audio_url: { url: "https://media.example.com/dialogue.wav" }, role: "reference_audio" },
    ]);
    const video = payload.content[2];
    assert.equal("start_time_seconds" in video, false);
});

test("无 role 的图片缺省 first_frame，参考图允许 reference_image", () => {
    const payload = buildMetasoPayload({
        ...base,
        images: [{ id: "a", url: "https://media.example.com/x.jpg" }, { id: "b", url: "https://media.example.com/y.jpg", role: "reference_image" }],
    }) as Record<string, unknown>;
    assert.equal(payload.content[1].role, "first_frame");
    assert.equal(payload.content[2].role, "reference_image");
});

test("档位归一与拒绝：480p 转大写、2K 原样、2160p/adaptive/3 秒/16 秒/超长提示词拒绝", () => {
    assert.equal(buildMetasoPayload({ ...base, resolution: "480p" }).resolution, "480P");
    assert.equal(buildMetasoPayload({ ...base, resolution: "2k" }).resolution, "2K");
    for (const mutate of [
        () => ({ ...base, resolution: "2160p" }),
        () => ({ ...base, resolution: "adaptive" }),
        () => ({ ...base, duration: 3 }),
        () => ({ ...base, duration: 16 }),
        () => ({ ...base, duration: 5.5 }),
        () => ({ ...base, prompt: "  " }),
        () => ({ ...base, prompt: "长".repeat(10001) }),
        () => ({ ...base, model: "MiniMax-H3-Max" }),
        () => ({ ...base, ratio: "2:3" }),
        () => ({ ...base, ratio: undefined }),
        () => ({ ...base, images: [{ id: "a", role: "last_frame" }] }),
        () => ({ ...base, images: [{ id: "a", role: "first_frame" }, { id: "b", role: "first_frame" }] }),
        () => ({ ...base, images: Array.from({ length: 10 }, (_, i) => ({ id: String(i), role: "reference_image" })) }),
        () => ({ ...base, videos: Array.from({ length: 4 }, (_, i) => ({ id: String(i) })) }),
        () => ({ ...base, audios: Array.from({ length: 4 }, (_, i) => ({ id: String(i) })) }),
        () => ({ ...base, images: [{ id: "a" }] }),
    ]) {
        assert.throws(() => buildMetasoPayload(mutate() as never), String, JSON.stringify(mutate()));
    }
});

test("有参考素材时比例缺省 16:9", () => {
    const payload = buildMetasoPayload({ ...base, ratio: undefined, images: [{ id: "a", role: "reference_image", url: "https://media.example.com/x.jpg" }] }) as Record<string, unknown>;
    assert.equal(payload.ratio, "16:9");
});
```

说明：测试沿用 `web/src/lib/asset-storage-revision.test.ts` 的 node:test + `.ts` 扩展名 import 模式（bun test 兼容 node:test 用例）。

- Step 2: 运行测试确认失败

Run: `cd web && bun test src/lib/metaso-minimax-video.test.ts`
Expected: FAIL，`Cannot find module` 或等价报错。

- Step 3: 实现 `web/src/lib/metaso-minimax-video.ts`

```ts
export const METASO_PROTOCOL = "metaso-minimax-video";

// 密塔协议与 Fisher-H3 同构但更严格：无 audio_control/驱动音频/start_time_seconds 扩展，
// 分辨率多 2K 档，比例固定六档，纯文本必须显式选择比例。前端先行校验给出友好错误，后端 manifest 校验兜底。
export type MetasoMedia = { id: string; url?: string };
export type MetasoImageInput = MetasoMedia & { role?: string };
export type MetasoInput = {
    model: string;
    prompt: string;
    resolution: string;
    duration: string | number;
    ratio?: string;
    images?: MetasoImageInput[];
    videos?: MetasoMedia[];
    audios?: MetasoMedia[];
};

export function buildMetasoPayload(input: MetasoInput): Record<string, unknown> {
    if (input.model !== "MiniMax-H3") throw new Error("密塔 H3 模型名称必须为 MiniMax-H3");
    const prompt = input.prompt.trim();
    if (!prompt || [...prompt].length > 10000) throw new Error("密塔 H3 提示词不能为空，且最多 10000 字符");
    const resolution = input.resolution.trim().toUpperCase().replace(/^(480|768|1080)$/, "$1P");
    if (!["480P", "768P", "1080P", "2K"].includes(resolution)) throw new Error("密塔 H3 分辨率仅支持 480P / 768P / 1080P / 2K");
    const images = input.images ?? [], videos = input.videos ?? [], audios = input.audios ?? [];
    const duration = Number(input.duration);
    if (!Number.isInteger(duration) || duration < 4 || duration > 15) throw new Error("密塔 H3 时长必须为 4–15 整数秒");
    const role = (image: MetasoImageInput) => image.role ?? "first_frame";
    if (images.some((image) => !["first_frame", "reference_image"].includes(role(image)))) throw new Error("密塔 H3 图片角色仅支持 first_frame 与 reference_image");
    if (images.filter((image) => role(image) === "first_frame").length > 1) throw new Error("密塔 H3 首帧图片最多 1 张");
    if (images.filter((image) => role(image) === "reference_image").length > 9) throw new Error("密塔 H3 参考图片最多 9 张");
    if (videos.length > 3) throw new Error("密塔 H3 参考视频最多 3 条");
    if (audios.length > 3) throw new Error("密塔 H3 参考音频最多 3 条");
    const hasMedia = images.length + videos.length + audios.length > 0;
    const ratio = input.ratio?.trim() || (hasMedia ? "16:9" : "");
    if (!["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"].includes(ratio)) throw new Error("密塔 H3 需要固定比例；纯文本生成必须显式指定");
    const mediaUrl = (media: MetasoMedia) => {
        if (!media.url?.trim()) throw new Error("密塔 H3 参考素材缺少可用地址");
        return media.url;
    };
    const content: Array<Record<string, unknown>> = [{ type: "text", text: prompt }];
    images.forEach((image) => content.push({ type: "image_url", image_url: { url: mediaUrl(image) }, role: role(image) }));
    videos.forEach((video) => content.push({ type: "video_url", video_url: { url: mediaUrl(video) }, role: "reference_video" }));
    audios.forEach((audio) => content.push({ type: "audio_url", audio_url: { url: mediaUrl(audio) }, role: "reference_audio" }));
    return { model: input.model, content, resolution, duration, ratio };
}
```

- Step 4: 运行测试确认通过

Run: `cd web && bun test src/lib/metaso-minimax-video.test.ts`
Expected: 全部 PASS。

- Step 5: Commit

```bash
git add web/src/lib/metaso-minimax-video.ts web/src/lib/metaso-minimax-video.test.ts
git commit -m "feat(video): 密塔 H3 - 前端纯函数 payload 构建与档位校验"
```

---

<!-- comet-task-ref:cf23cc552a674d4bb890dd80a919b871 -->
<!-- comet-task-ref:ce73eef836c3425d91c60a3dfbc90d17 -->
<!-- comet-task-ref:f6456828f8224f01a8e37e51bb981687 -->
<!-- comet-task-ref:b01ecdb7d16145dead7d0b42d7f7de53 -->
### Task 5: 前端 provider、分发与类型（TDD）

**Files:**
- Create: `web/src/services/api/video-provider-metaso.ts`
- Modify: `web/src/services/api/video-contracts.ts:51`（provider 联合类型）
- Modify: `web/src/services/api/video.ts:13-20,45-49,60-64`（import 与 create/poll 分发行）
- Test: `web/test/metaso-minimax-api.test.ts`（新增）

**Interfaces:**
- Consumes: Task 4 的 `buildMetasoPayload`/`METASO_PROTOCOL`；`video-contracts.ts` 的 `ResolvedAiConfig`/`RequestOptions`/`VideoGenerationTask`/`VideoGenerationTaskState`；`VideoProviderDeps`（`video-provider-deps.ts`）；`resolveVideoReferenceImageUrl`/`resolveVideoReferenceMediaUrl`（`video-reference-media.ts`）；`resolveVideoImageReferences`（`video-reference-roles.ts`）；`modelOptionName`（`use-config-store.ts` 再导出）；`videoResponseTools.readAxiosError/videoResultFromUrl`。
- Produces: `createMetasoVideoTask(deps, config, model, prompt, references, videoReferences, audioReferences, options?): Promise<VideoGenerationTask>`（task 的 `provider: "metaso-minimax"`）与 `pollMetasoVideoTask(deps, config, task, options?): Promise<VideoGenerationTaskState>`，供 `video.ts` 分发行调用。

- Step 1: 写失败测试 `web/test/metaso-minimax-api.test.ts`

```ts
import { expect, test } from "bun:test";
import { createMetasoVideoTask, pollMetasoVideoTask } from "../src/services/api/video-provider-metaso";
import { defaultConfig } from "../src/stores/use-config-store";
import { videoResponseTools } from "../src/services/api/video-response";
import type { VideoProviderDeps } from "../src/services/api/video-provider-deps";

const config = { ...defaultConfig, interfaceType: "metaso-minimax-video", model: "MiniMax-H3", baseUrl: "https://metaso.cn/api/minimax", apiKey: "test-only", videoSeconds: "5", vquality: "768P", size: "16:9" };
const image = { id: "first", name: "first.png", type: "image/png", dataUrl: "", url: "https://media.example.com/first.png" };

test("创建发送 content[] 且 base 以 /v2 结尾时不重复补前缀", async () => {
    let body: Record<string, unknown> = {};
    const deps = { transport: { post: async (url: string, value: Record<string, unknown>) => { expect(url).toBe("https://metaso.cn/api/minimax/v2/video_generation"); body = value; return { task_id: "task-1" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    const task = await createMetasoVideoTask(deps, config as never, config.model, "平稳转身", [image], [], []);
    expect(task).toEqual({ id: "task-1", provider: "metaso-minimax", model: config.model });
    expect(body.model).toBe("MiniMax-H3");
    expect(body.resolution).toBe("768P");
    expect(body.ratio).toBe("16:9");
    expect(body.content).toContainEqual({ type: "image_url", image_url: { url: image.url }, role: "first_frame" });
});

test("非法参数不发 POST；request_id 不充当 task_id，缺失时报结果不确定", async () => {
    let posts = 0;
    const deps = { transport: { post: async () => { posts++; return { request_id: "trace-only" }; } }, response: videoResponseTools } as unknown as VideoProviderDeps;
    await expect(createMetasoVideoTask(deps, { ...config, videoSeconds: "16" } as never, config.model, "运动", [], [], [])).rejects.toThrow();
    expect(posts).toBe(0);
    await expect(createMetasoVideoTask(deps, config as never, config.model, "运动", [], [], [])).rejects.toThrow("创建结果不确定");
    expect(posts).toBe(1);
});

test("轮询状态归一：成功取回 URL、失败带上游错误、queued/running 为 pending、未知状态抛错", async () => {
    const task = { id: "task-1", provider: "metaso-minimax", model: config.model } as never;
    const deps = { transport: { get: async () => ({ task: { status: "succeeded", content: { url: "https://media.example.com/result.mp4" } } }) }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "completed", result: { url: "https://media.example.com/result.mp4" } });
    deps.transport.get = async () => ({ task: { status: "failed", error: { code: "1001", message: "额度不足" } } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "failed", error: "1001：额度不足" });
    deps.transport.get = async () => ({ task: { status: "queued" } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "pending" });
    deps.transport.get = async () => ({ task: { status: "mystery" } }) as never;
    await expect(pollMetasoVideoTask(deps, config as never, task)).rejects.toThrow("未知任务状态");
});

test("成功响应中 error.code=0 不应成为失败；成功但无 URL 归为失败", async () => {
    const task = { id: "task-1", provider: "metaso-minimax", model: config.model } as never;
    const deps = { transport: { get: async () => ({ task: { status: "succeeded", error: { code: 0 }, content: { url: "https://media.example.com/result.mp4" } } }) }, response: { ...videoResponseTools, videoResultFromUrl: async (url: string) => ({ url }) } } as unknown as VideoProviderDeps;
    expect((await pollMetasoVideoTask(deps, config as never, task)).status).toBe("completed");
    deps.transport.get = async () => ({ task: { status: "succeeded" } }) as never;
    expect(await pollMetasoVideoTask(deps, config as never, task)).toEqual({ status: "failed", error: "密塔 H3 任务已完成但没有返回视频地址" });
});
```

- Step 2: 运行测试确认失败

Run: `cd web && bun test test/metaso-minimax-api.test.ts`
Expected: FAIL，`Cannot find module "../src/services/api/video-provider-metaso"`。

- Step 3: 实现 `web/src/services/api/video-provider-metaso.ts`

```ts
import { buildMetasoPayload } from "@/lib/metaso-minimax-video";
import { modelOptionName } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";
import { resolveVideoReferenceImageUrl, resolveVideoReferenceMediaUrl } from "./video-reference-media";
import { resolveVideoImageReferences } from "./video-reference-roles";
import type { ResolvedAiConfig, RequestOptions, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";
import type { VideoProviderDeps } from "./video-provider-deps";

export async function createMetasoVideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, model: string, prompt: string, references: ReferenceImage[], videoReferences: ReferenceVideo[], audioReferences: ReferenceAudio[], options?: RequestOptions): Promise<VideoGenerationTask> {
    const input = { model: modelOptionName(model), prompt, resolution: config.vquality, duration: config.videoSeconds, ratio: config.size };
    const imagePlan = resolveVideoImageReferences(references, options);
    // 与 Fisher provider 相同的两次构建：先用占位 URL 完成档位/角色校验，避免为非法请求上传或解析素材。
    const pending = <T extends { id: string }>(media: T) => ({ ...media, url: `https://pending.invalid/${encodeURIComponent(media.id)}` });
    buildMetasoPayload({ ...input, images: imagePlan.map(({ image, role }) => ({ ...pending(image), role })), videos: videoReferences.map(pending), audios: audioReferences.map(pending) });
    const [imageUrls, videoUrls, audioUrls] = await Promise.all([
        Promise.all(references.map(async (image) => resolveVideoReferenceImageUrl(image, "密塔 H3"))),
        Promise.all(videoReferences.map(async (video) => resolveVideoReferenceMediaUrl(video, "密塔 H3 参考视频", options?.signal))),
        Promise.all(audioReferences.map(async (audio) => resolveVideoReferenceMediaUrl(audio, "密塔 H3 参考音频", options?.signal))),
    ]);
    const payload = buildMetasoPayload({
        ...input,
        images: imagePlan.map(({ image, role }, index) => ({ id: image.id, url: imageUrls[index], role })),
        videos: videoReferences.map((video, index) => ({ id: video.id, url: videoUrls[index] })),
        audios: audioReferences.map((audio, index) => ({ id: audio.id, url: audioUrls[index] })),
    });
    try {
        const created = await deps.transport.post<{ task_id?: unknown }>(metasoVideoUrl(config, "/video_generation"), payload, options);
        const id = validMetasoTaskId(created?.task_id, config.apiKey);
        if (!id) throw new Error("密塔 H3 未返回 task_id，创建结果待核对，请勿直接重发");
        return { id, provider: "metaso-minimax", model };
    } catch (error) {
        throw new Error(deps.response.readAxiosError(error, "密塔 H3 创建结果不确定，请勿直接重发付费任务"));
    }
}

export async function pollMetasoVideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    const response = await deps.transport.get<{ task?: { status?: string; content?: { url?: string }; error?: { code?: string | number; message?: string } | null } }>(metasoVideoUrl(config, `/query/video_generation/${encodeURIComponent(task.id)}`), options);
    const state = response?.task;
    if (!state) throw new Error("密塔 H3 查询没有返回 task");
    const errorCode = state.error?.code;
    const failed = errorCode !== undefined && errorCode !== "" ? String(errorCode) !== "0" : Boolean(state.error?.message?.trim());
    if (failed || ["failed", "cancelled"].includes(state.status ?? "")) return { status: "failed", error: `${errorCode ? `${errorCode}：` : ""}${state.error?.message || (state.status === "cancelled" ? "密塔 H3 任务已取消" : "密塔 H3 视频生成失败")}` };
    if (state.status === "succeeded") {
        if (!state.content?.url) return { status: "failed", error: "密塔 H3 任务已完成但没有返回视频地址" };
        return { status: "completed", result: await deps.response.videoResultFromUrl(state.content.url, options) };
    }
    if (!["queued", "running"].includes(state.status ?? "")) throw new Error("密塔 H3 返回未知任务状态");
    return { status: "pending" };
}

function metasoVideoUrl(config: ResolvedAiConfig, path: string) {
    const base = config.baseUrl.replace(/\/+$/, "");
    return /\/v2$/i.test(base) ? `${base}${path}` : `${base}/v2${path}`;
}

function validMetasoTaskId(value: unknown, apiKey: string) {
    return typeof value === "string" && !(apiKey && value.includes(apiKey)) && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/.test(value) ? value : "";
}
```

实现说明（实现者请按此意图核对，不要照抄后自行偏离语义）：
- payload 完全由 `buildMetasoPayload` 组装（content[]、归一后的 resolution/duration/ratio），前端校验与 manifest 校验天然同构；预检用占位 URL 先行失败，正式构建复用同一函数，不存在第二套字段映射。
- `resolveVideoImageReferences` 复用统一图片角色解析：`reference_to_video` 操作 → 全部 `reference_image`；显式帧节点 → first/last（`last_frame` 会被 `buildMetasoPayload` 的 role 白名单拒绝，符合设计“不支持尾帧”）；多模态上下文 → `reference_image`；传统单图 → `first_frame`。
- 上游错误码为 0 时不判失败（与 Fisher poll 语义一致，`error.code=0` 是成功任务上的成功标志）。
- `request_id`/追踪编号不进 task_id（manifest 层 taskId 只 coalesce `task_id`，前端 `validMetasoTaskId` 同步兜底，且排除含 API Key 的编号）。

- Step 4: 修改 `web/src/services/api/video-contracts.ts:51`

```ts
export type VideoGenerationTask = { id: string; provider: "openai" | "agnes" | "seedance" | "video-generations" | "gemini-veo" | "novita" | "minimax" | "fisher-h3" | "metaso-minimax"; model: string };
```

- Step 5: 修改 `web/src/services/api/video.ts` 分发

import 区（L13-14 附近）加：

```ts
import { createMetasoVideoTask, pollMetasoVideoTask } from "./video-provider-metaso";
```

`createVideoGenerationTask` 中 `if (requestConfig.interfaceType === "fisher-h3-video") ...` 行（video.ts:49）之后加：

```ts
    if (requestConfig.interfaceType === "metaso-minimax-video") return createMetasoVideoTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
```

`pollVideoGenerationTask` 中 `if (task.provider === "fisher-h3") ...` 行（video.ts:64）之后加：

```ts
    if (task.provider === "metaso-minimax") return pollMetasoVideoTask(deps, requestConfig, task, options);
```

- Step 6: 运行测试确认通过

Run: `cd web && bun test test/metaso-minimax-api.test.ts`
Expected: 4 个用例全部 PASS。

- Step 7: Commit

```bash
git add web/src/services/api/video-provider-metaso.ts web/src/services/api/video-contracts.ts web/src/services/api/video.ts web/test/metaso-minimax-api.test.ts
git commit -m "feat(video): 密塔 H3 - 前端 provider、任务分发与结果不确定语义"
```

---

<!-- comet-task-ref:cf23cc552a674d4bb890dd80a919b871 -->
<!-- comet-task-ref:ce73eef836c3425d91c60a3dfbc90d17 -->
<!-- comet-task-ref:f6456828f8224f01a8e37e51bb981687 -->
<!-- comet-task-ref:b01ecdb7d16145dead7d0b42d7f7de53 -->
### Task 6: 能力映射、渠道默认值与预设按钮

**Files:**
- Modify: `web/src/lib/model-capabilities.ts:397-420`（协议特化块）
- Modify: `web/src/stores/use-config-store.ts:515-517`（`createFisherH3Channel` 旁）、`use-config-store.ts:595-605`（`defaultBaseUrlForChannelInterface`）
- Modify: `web/src/pages/settings/channel-settings-pane.tsx:64-65,185`（preset 与按钮）

**Interfaces:**
- Consumes: 协议 ID `metaso-minimax-video`（Task 1–3 已注册，后端能力默认值是前端映射的行为基准）；`defaultModelCapabilityConfig`、`createModelChannel`、`ModelChannel.modelCosts` 形状（use-config-store.ts:91-112）。
- Produces: 绑定密塔渠道的生成面板展示 480P/768P/1080P/2K（默认 768P）、4–15 enum 时长、六档比例；渠道设置页出现“添加密塔 H3”按钮，一键创建带默认 Base URL 与模型条目的渠道。

- Step 1: `web/src/lib/model-capabilities.ts` 加协议块

在 `if (protocol === "fisher-h3-video")` 块（model-capabilities.ts:397-404）之后加（与后端 `model_capability_defaults.go` 的密塔 case 数值逐项一致）：

```ts
    if (protocol === "metaso-minimax-video") {
        Object.assign(video.references, { promptMaxChars: 10000, maxImages: 9, maxVideos: 3, maxVideoBytes: 50 * 1024 * 1024, maxVideoDurationSeconds: 15, maxAudios: 3, maxAudioBytes: 15 * 1024 * 1024, maxAudioDurationSeconds: 15 });
        video.duration = { selection: "enum", values: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], default: 5 };
        video.ratios = ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"];
        video.defaultRatio = "16:9";
        video.resolutions = ["480P", "768P", "1080P", "2K"];
        video.defaultResolution = "768P";
        video.operations.push("reference_to_video");
    }
```

- Step 2: `web/src/stores/use-config-store.ts` 加默认 Base URL 与渠道工厂

`defaultBaseUrlForChannelInterface`（use-config-store.ts:601-602 附近）在 `fisher-h3-video` 行后加：

```ts
    if (interfaceType === "metaso-minimax-video") return "https://metaso.cn/api/minimax";
```

`createFisherH3Channel`（use-config-store.ts:515-517）之后加：

```ts
export function createMetasoChannel(): ModelChannel {
    return createModelChannel({ name: "密塔 H3", baseUrl: "https://metaso.cn/api/minimax", apiKey: "", models: ["MiniMax-H3"], modelCosts: [{ model: "MiniMax-H3", displayName: "MiniMax-H3 (Metaso)", capability: "video", protocol: "metaso-minimax-video", billingMode: "per_second", unitPriceMicrocredits: 0, capabilityConfig: defaultModelCapabilityConfig("metaso-minimax-video", "MiniMax-H3"), description: "密塔（metaso.cn）MiniMax V2 中转：480P/768P/1080P/2K，4–15 秒，六档固定比例；价格以密塔控制台为准，待确认后补充。" }] });
}
```

- Step 3: `web/src/pages/settings/channel-settings-pane.tsx` 加 preset

import 区（L12 附近）加 `createMetasoChannel`；`addChannel`（channel-settings-pane.tsx:64-65）改为：

```ts
    const addChannel = (preset?: "fisher" | "metaso") => {
        const channel = preset === "fisher" ? createFisherH3Channel() : preset === "metaso" ? createMetasoChannel() : createModelChannel({ name: `渠道 ${userChannels.length + 1}` });
```

按钮区（channel-settings-pane.tsx:185）在 Fisher 按钮后加：

```tsx
                    <Button className="h-10 sm:h-8" onClick={() => addChannel("metaso")}>添加密塔 H3</Button>
```

- Step 4: 构建验证

Run: `cd web && bun run build`
Expected: 构建成功、无类型错误。

- Step 5: Commit

```bash
git add web/src/lib/model-capabilities.ts web/src/stores/use-config-store.ts web/src/pages/settings/channel-settings-pane.tsx
git commit -m "feat(video): 密塔 H3 - 能力映射、默认 Base URL 与渠道预设"
```

---

<!-- comet-task-ref:a6b0813f24df4682945e96275c6a3af3 -->
<!-- comet-task-ref:e0a4ae638bbf4b619f7fc56b523546fc -->
### Task 7: 文档同步与待验收登记

**Files:**
- Modify: `docs/content/docs/backend/http-api.mdx`（顶部“Fisher-H3 模型中转”节后新增小节）
- Modify: `docs/content/docs/progress/pending-test.mdx`（顶部追加一节）

**Interfaces:**
- Consumes: 本计划 Global Constraints 的协议事实（端点、鉴权、档位）。
- Produces: 文档站可构建；待验收项进入 pending-test 清单。

- Step 1: `docs/content/docs/backend/http-api.mdx` 补密塔协议说明

在“## Fisher-H3 模型中转”整节（L7-12）之后插入：

```markdown
## 密塔 H3 视频中转

`metaso-minimax-video` 是 MiniMax V2 协议中转插件：创建 `POST {baseUrl}/v2/video_generation`、查询 `GET /v2/query/video_generation/{taskId}`，Bearer API Key 鉴权，默认 Base URL 为 `https://metaso.cn/api/minimax`。参数档位：模型固定 `MiniMax-H3`；分辨率 480P/768P/1080P/2K（默认 768P，按大写原样透传，2K 为密塔开放档位）；时长 4–15 整数秒（默认 5）；比例 21:9/16:9/4:3/1:1/3:4/9:16（默认 16:9，无任何参考素材时必须显式指定）。参考素材以公网 URL 提交（`requiresPublicMediaUrls`），角色映射为 first_frame / reference_image / reference_video / reference_audio，首帧至多 1 张、参考图 9 张、参考视频与参考音频各 3 条。密塔不接收 `audio_control`、`start_time_seconds`、`generate_audio` 与 `aigc_watermark`，插件从源头不发送。创建只读取 `task_id`；查询响应按 `task.status` 归一，成片 URL 取自 `task.content.url` 并由宿主立即下载持久化（`resultEphemeral`）。错误消息按 `task.error.message` → `error.message` → `base_resp.status_msg` → `task.message` 兜底。
```

- Step 2: `docs/content/docs/progress/pending-test.mdx` 登记待验收

在 `# 待测试` 标题后（Fisher-H3 节之前）插入：

```markdown
## 密塔 H3 视频生成（2026-10-08）

- 已接入 `metaso-minimax-video` 独立插件与“添加密塔 H3”渠道预设，Key 留空；档位、content[] 组装、响应映射由插件契约测试与前端专项测试覆盖，没有调用真实上游，没有浏览器验收。
- （浏览器验收项，登记于 pending-test.mdx）在渠道设置点“添加密塔 H3”，填写自己的 Key；确认协议选择器显示密塔身份、默认 Base URL 为 `https://metaso.cn/api/minimax`。
- （浏览器验收项，登记于 pending-test.mdx）真实 Key 下验证三种模式：纯文本（须选比例）、首帧图生视频、参考视频 + 参考音频；确认 480P/768P/1080P/2K 四档与 4–15 秒档位可选、2K 正常出片。
- （浏览器验收项，登记于 pending-test.mdx）验证查询归一与结果入库：成片 URL 过期前由宿主下载持久化；失败任务呈现上游错误（含 `base_resp.status_msg` 形态）。
- （浏览器验收项，登记于 pending-test.mdx）确认 `reference_image`（纯参考图）在密塔的真实支持度：不支持则由上游报错呈现，不阻塞本期。
- （浏览器验收项，登记于 pending-test.mdx）确认密塔计费单价并回填渠道描述；验证超档参数被本地拒绝且不产生上游请求。
```

- Step 3: 文档站类型检查

Run: `cd docs && bun run types:check`
Expected: 通过（MDX 无 frontmatter/链接错误）。

- Step 4: Commit

```bash
git add docs/content/docs/backend/http-api.mdx docs/content/docs/progress/pending-test.mdx
git commit -m "docs(video): 密塔 H3 - 补充渠道协议说明并登记待浏览器验收"
```

---

<!-- comet-task-ref:d5bff2af85704366886ff9e752a60e71 -->
<!-- comet-task-ref:33941c9132994153a011c2059410941c -->
### Task 8: 集成验证

**Files:**
- 无新改动；全量回归。

**Interfaces:**
- Consumes: Task 1–7 全部产物。
- Produces: 全量验证证据；如回归失败，回到对应任务修复（不跳过失败用例）。

- Step 1: 后端全量测试

Run: `cd backend && go test ./...`
Expected: 新增 `MetasoMinimax*` 用例与既有用例全部通过。若出现与本改动无关的预存环境失败（如本机 Redis），如实记录并与基线对比确认非本次引入。

- Step 2: 前端构建

Run: `cd web && bun run build`
Expected: 构建成功、无类型错误。

- Step 3: 前端专项测试

Run: `cd web && bun test src/lib/metaso-minimax-video.test.ts test/metaso-minimax-api.test.ts`
Expected: 全部 PASS。

- Step 4: 插件目录测试（官方清单回归）

Run: `cd backend && go test ./internal/protocol/ -run TestOfficialProtocolPackages -v`
Expected: PASS——新插件包满足自包含契约（documentation 全文一致、README/interface 存在、字段名可检索），且 provider ID 不与既有包重复。

- Step 5: 交付检查（不改代码）

- `git status --short` 只剩本 change 的改动与用户已有修改；未提交数据库、Key、构建临时文件。
- `git log --oneline -8` 的提交说明符合 `<type>(<scope>): 密塔 H3 - <摘要>` 格式。
- 对照 tasks.md 勾选已完成的条目（1.1–1.3、2.1–2.3、3.1–3.4、4.1–4.2、5.1–5.2）：勾选由主会话在用户确认后进行，实现者只报告。

---

## 验证矩阵（与 Design Doc 测试策略对齐）

| 层 | 文件/命令 | 断言 |
| --- | --- | --- |
| 插件契约 | `backend/internal/protocol/metaso_minimax_video_test.go`（Task 3） | 包↔manifest 一致；身份（Label/BaseURL/requiresPublicMediaUrls/bearer）；三种模式 content[] 组装（文生仅 text、首帧 role、视频+音频 role、无 start_time_seconds）；档位校验（拒 2160p、拒 3 秒、拒 adaptive、拒 last_frame）；创建/查询路径与 Bearer；响应 taskId/status/videos/message 映射（含 base_resp 兜底）；resultEphemeral |
| 前端纯函数 | `web/src/lib/metaso-minimax-video.test.ts`（Task 4） | payload 三种模式；档位拒绝；首帧唯一；role 缺省；错误消息 |
| 前端 provider | `web/test/metaso-minimax-api.test.ts`（Task 5） | URL 拼接（/v2 兼容）；task_id 校验与“结果不确定”；轮询状态归一；error.code=0 不误判 |
| 集成 | `go test ./...` + `bun run build`（Task 8） | 新增用例与既有用例全过、构建无类型错误 |
| 浏览器验收 | `pending-test.mdx` 登记（Task 7） | 真实 Key 下三种模式生成、查询、结果入库（不在本计划内执行） |

## 执行顺序与依赖

Task 1 → Task 2 → Task 3（依赖插件包）→ Task 4（独立）→ Task 5（依赖 Task 4）→ Task 6（依赖 Task 1 的协议 ID 稳定）→ Task 7 → Task 8。Task 4 可与 Task 3 并行。
