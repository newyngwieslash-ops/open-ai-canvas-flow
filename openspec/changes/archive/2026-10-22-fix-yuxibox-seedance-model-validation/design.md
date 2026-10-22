# 修复方案：去掉 yuxibox Seedance 的写死模型校验

## 方案

只有一处根因，单点修改：

1. `plugin-packages/generate-catalog.mjs` 的 `yuxibox-seedance-video` spec：
   - 删除 `validations[0]`（`eq(ref("request.model"), "seedance2.5-yx-count")`）。
   - 更新 `notes`，说明这是 Seedance 家族共用端点、插件不锁定单一模型名、各模型时长档位由渠道 capabilityConfig 与上游校验决定。
2. 重新生成 `plugin-packages/yuxibox-seedance-video/{manifest.json,docs/interface.md}`。
3. 按 `build-packages.sh` 的同一文件清单重新打包 `plugin-packages/yuxibox-seedance-video.yingce-plugin`（zip，`ZIP_STORED`，条目顺序 README → docs/interface.md → manifest.json）。

不新增校验白名单。理由：仓库里服务多模型同端点的插件（`openai-videos`、`newapi-media-task-v1`、`newapi-video-generations-v1`）都不做请求级模型名校验。加白名单会把"上游新增模型"变成需要改代码的发布，且 `seedance-2.5-yx-second` 这类上游认领但暂无渠道的名字会被误判为不存在——模型可用性是上游 + 渠道配置的职责。

## 数据流

修改后请求路径：

```
画布视频节点 → provider 调度 → manifestAdapter.BuildCreate
            → validateManifestRequest（仅 prompt/时长/比例/素材数量）
            → buildManifestOperation（model 原样进 create.body.model）
            → POST https://yuxibox.cn/v1/videos
```

模型名从"被本地拦截"变为"原样透传到上游"，上游用 `model_not_found` 表达不可用，错误语义更准确。

## 回归策略

`backend/internal/protocol/yuxibox_seedance_video_test.go` 新增 `TestYuxiboxSeedanceAcceptsEveryUpstreamModel`：

- 表驱动覆盖上游 4 个真实模型 + `seedance-2.5-yx-second`，断言 `BuildCreate` 不返回校验错误，且 `create.body.model` 原样等于传入模型名。
- 删除原 `TestYuxiboxSeedanceValidation` 中的 `{"model renamed", r.Model = "seedance2.5"}` 用例——它断言的正是本 change 要移除的行为。

保留的校验用例（prompt 空/超长、时长上下界、比例、素材数量上限）不变。

## 影响面

| 项 | 影响 |
| --- | --- |
| 后端代码 | 无（`manifest_adapter.go` 逻辑不动） |
| 插件产物 | `manifest.json`、`docs/interface.md`、`.yingce-plugin` 重新生成 |
| 已有渠道配置 | 不需要改；原本被误拒的模型立即可用 |
| 其它插件 | 生成器按 id 定向运行，未触碰其它包（`git diff --stat` 仅 3 个文件） |
| 文档 | `docs/content/docs/backend/http-api.mdx` 中的 yuxibox 段落仍写"模型精确 seedance2.5-yx-count"，需同步 |

## 风险

- 移除模型名校验后，拼错的模型名不再被本地拦截，会以一次上游 503 的形式失败。可接受：错误文案来自上游且语义清楚，成本是一次未计费请求；换来的是新增模型不需要发版。
- `seedance-2.5-yx-second` 仍会失败（上游无可用渠道），这是配置层事实，不是本 change 引入的回归；proposal 已记录并移交用户处理。
