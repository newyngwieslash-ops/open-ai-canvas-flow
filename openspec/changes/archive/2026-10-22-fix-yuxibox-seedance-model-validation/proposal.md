# 修复 yuxibox Seedance 渠道模型被写死导致的生成失败

## 现象

用户在画布用视频节点调用 `seedance-2.5-yx-second`（系统渠道 `宇熙Box` / `CHANNEL_000006`）生成，任务立刻失败，错误文案：

```
yuxibox 渠道模型必须为 seedance2.5-yx-count。
```

失败任务：`9ff2f82493e165e00ea6d2a58796243c`（`canvas_video`，`reference_to_video`，`CHANNEL_000006::seedance-2.5-yx-second`），上游从未收到请求，`api_call_logs` 中 `/task/provider-preflight` 记录 `status=failed status_code=0`。

## 根因

`plugin-packages/generate-catalog.mjs` 中 `yuxibox-seedance-video` 插件的第一条 validation 把模型名写死：

```js
{ assert: eq(ref("request.model"), "seedance2.5-yx-count"), message: "yuxibox 渠道模型必须为 seedance2.5-yx-count。" }
```

该断言生成的 `manifest.json` 与 `.yingce-plugin` 包一起入库，后端 `manifestAdapter.BuildCreate` 在 `protocol/manifest_adapter.go:39` 调用 `validateManifestRequest`，因此在**发出任何网络请求之前**就把所有非 `seedance2.5-yx-count` 的模型名拒绝掉，错误文案原样上抛到任务 `error` 字段，前端节点显示同一条消息。

但这个插件本身是多模型协议：yuxibox 的 `/v1/videos` 用同一套 OpenAI Videos 异步合同服务一整个 Seedance 家族。用渠道真实 Key 查 `GET https://yuxibox.cn/v1/models` 得到 20 个模型，其中 Seedance 系为：

- `seedance2.5-yx-count`
- `seedance-2.5（YX）`
- `seedance-2.5（YX）-按次`
- `seedance-2.0（YX）`

仓库里同样是"一个端点服务多个模型"的插件（`openai-videos`、`newapi-media-task-v1`、`newapi-video-generations-v1`）都不做请求级模型名校验，把模型可用性交给上游。因此这里的写死断言既与协议事实不符，也与仓库既有约定不符——它是这个 bug 的根因，不是保护。

## 另一个独立问题（本 change 不修，另行处理）

`seedance-2.5-yx-second` 这个模型名**不在上游可用清单中**。用该渠道真实 Key 直接向 `POST https://yuxibox.cn/v1/videos` 探测：

| 模型名 | 响应 |
| --- | --- |
| `seedance2.5-yx-count` | `400 YX: prompt must contain 1-16000 characters`（模型存在，仅参数缺失） |
| `seedance-2.5（YX）` | 同上 |
| `seedance-2.5（YX）-按次` | 同上 |
| `seedance-2.0（YX）` | 同上 |
| `seedance-2.5-yx` | `503 model_not_found`（模型不存在） |
| `seedance-2.5-yx-second` | `503 model_not_found: the model is claimed by a task plugin, which has no enabled channel serving it` |

结论：上游确实"认领"了 `seedance-2.5-yx-second` 这个名字，但它当前没有可用渠道在服务，因此**即使移除本地校验，用这个名字生成仍会在上游失败**。用户需要把该渠道模型的「上游模型标识」改成真实模型之一。

这条属于渠道数据配置，不是代码缺陷；本 change 只修"插件把合法模型当非法模型拒绝"的代码 bug。

## 修复目标

1. 移除 `yuxibox-seedance-video` 插件里写死的模型名断言，使同一端点的全部上游模型都能正常构建请求。
2. 保留其余真实存在的请求契约校验（prompt 长度、时长、比例、素材数量）。
3. 重新生成 `manifest.json`、`docs/interface.md` 与 `.yingce-plugin` 包，保持源码与包一致。
4. 补测试覆盖"每个上游模型都能通过本地校验"，并保留一个"未知模型名不再被本地拦截"的行为说明。

## 不在范围内

- 修改渠道 `MODEL_000075` 的 `provider_model_key`（用户配置数据）。
- 为不同模型做时长档位差异化（2.0（YX）5/10/15 秒、按次固定 30 秒等）——这需要按模型名分支，属于新能力设计，不是 hotfix。
- 上游 `seedance-2.5-yx-second` 无可用渠道的问题（上游侧配置）。
