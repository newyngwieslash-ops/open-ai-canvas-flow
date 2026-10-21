# Design

## 方案选择

新增独立插件而非改造现有 `yuxibox-seedance-video`。

理由：现有插件的 `create.body` 没有 `$merge` / `providerOptions` 透传口，`resolution` 与 `seed` 无处安放；且它默认发送 `videos`（Seedance 支持参考视频），而 `minimax-h3-quality` 明确不支持。在同一插件里按模型分支会让请求模板产生条件逻辑，回归风险高于新建一份。

复用点：`baseUrl`、`poll.path`、`response.status` 归一、`resultEphemeral` 语义直接沿用现有 Seedance 插件的已验证写法。

## 请求模板

`create.body`（`application/json`，`POST /v1/videos`）：

```json
{
  "model": { "$ref": "request.model" },
  "prompt": { "$ref": "request.prompt" },
  "seconds": { "$toInt": { "$ref": "request.duration" } },
  "resolution": { "$lower": { "$coalesce": [{ "$ref": "request.resolution" }, "768p"] } },
  "ratio": { "$coalesce": [{ "$ref": "request.aspectRatio" }, "9:16"] },
  "reference_images": { "$omitEmpty": { "$map": { "from": { "$sortByOrder": { "$ref": "request.images" } }, "as": "media", "in": { "$ref": "media.value" } } } },
  "audios": { "$omitEmpty": { "$map": { "from": { "$sortByOrder": { "$ref": "request.audios" } }, "as": "media", "in": { "$ref": "media.value" } } } },
  "seed": { "$omitEmpty": { "$ref": "providerOptions.yuxibox-minimax-h3-quality.seed" } }
}
```

要点：

- **不出现 `videos` 键**：上游不支持，且 `parameters` 里 `videos` 声明为 `mapping: "unsupported"`，宿主准入阶段即拒绝。
- `resolution` 用 `$lower` 归一，配合能力档位只暴露 `480P`/`768P`，`720p` 输入映射到 `768p`。
- `seed` 走 `providerOptions` 命名空间，不占用统一字段，避免影响其他视频协议。
- `seconds` 用 `$toInt`：上游声明 integer，`duration` 统一字段是整数但模板层需显式转换。

## 响应模板

```json
{
  "taskId": { "$coalesce": [{ "$ref": "response.id" }, { "$ref": "response.task_id" }] },
  "status": { "$coalesce": [{ "$ref": "response.status" }, "queued"] },
  "message": { "$coalesce": [{ "$ref": "response.error.message" }, { "$ref": "response.message" }] },
  "videos": { "$coalesce": [{ "$ref": "response.video_url" }, { "$ref": "response.download_url" }] },
  "resultEphemeral": true
}
```

`status` 归一由宿主 `videoAdapter` 统一处理（`queued`/`in_progress` → pending，`completed` → completed，`failed` → failed），插件只声明取值域。

## 能力配置

| 字段 | 值 | 依据 |
| --- | --- | --- |
| `references.promptMaxChars` | 20000 | 上游文档 |
| `references.minImages` / `maxImages` | 1 / 9 | 上游 `reference_images` 至少 1、最多 9 |
| `references.maxImageBytes` | 20 MiB | 上游 `input_reference` 单个最大 20 MiB |
| `references.maxVideos` | 0 | 上游不支持参考视频 |
| `references.maxAudios` / `maxAudioBytes` | 3 / 15 MiB | 上游 0–3 个音频 |
| `duration` | range 1–15，step 1，default 5 | 上游 `seconds` |
| `ratios` / `defaultRatio` | `16:9`,`9:16`,`1:1` / `9:16` | 上游 `ratio` |
| `resolutions` / `defaultResolution` | `480P`,`768P` / `768P` | 上游 `resolution`（720p 为别名） |

## 计费

`per_second` 按 `vquality` 两档配置：`480p` 与 `768p`（默认价目取高峰档 0.040 / 0.060）。空闲时段（00:00–08:00）单价更低，但当前价格档模型没有时间窗口选择器，**由运营在管理后台按需调整**；跨时段完成不改已锁定单价属于上游计费口径，本插件不干预。

## 测试策略

契约测试放在 `backend/internal/protocol/yuxibox_minimax_h3_quality_test.go`，覆盖：

1. 请求体逐字段（含 `720p → 768p`、`seed` 透传、`audios` 空时省略）。
2. `videos` 非空时返回参数错误，不构造请求。
3. 响应映射：`queued`/`in_progress`/`completed`/`failed` 四态，`video_url` 优先于 `download_url`。
4. manifest JSON 合法、`generate-catalog.mjs` 能生成包。
