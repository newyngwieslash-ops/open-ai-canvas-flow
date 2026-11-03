# 验证报告：fix-yuxibox-seedance-model-validation

- 日期：2026-10-22
- 验证模式：full（规模评估建议 full：tasks=6 > 阈值 3）
- 提交：`f5962a8e fix(plugins): yuxibox Seedance - 移除写死模型名校验，使同端点全部上游模型可用`

## 结论

PASS。根因已消除：`yuxibox-seedance-video` 插件不再把 `request.model` 写死为 `seedance2.5-yx-count`，同一上游端点的全部 Seedance 模型都能通过本地校验并原样透传。

## 检查项

| # | 检查 | 结果 | 证据 |
| --- | --- | --- | --- |
| 1 | tasks.md 全部完成 | PASS | 6/6 `[x]` |
| 2 | 改动文件与 tasks 描述一致 | PASS | 实际改动 6 个文件：`generate-catalog.mjs`、插件 `manifest.json`、`docs/interface.md`、`.yingce-plugin`、`yuxibox_seedance_video_test.go`、`http-api.mdx` |
| 3 | 构建通过 | PASS | `cd backend && go build ./...` → exit 0 |
| 4 | 相关测试通过 | PASS | `go test ./internal/protocol/ -count=1` → ok 5.26s（含新增回归用例） |
| 5 | 无硬编码密钥/新增不安全操作 | PASS | 改动仅删除一条 validation 与更新 notes/文档；无密钥、无新增 IO |
| 6 | 代码审查 | SKIP | hotfix 默认 `review_mode: off`（按 comet-verify 记录跳过原因） |

## 根因验证（RED → GREEN）

RED（修复前，在原包上运行新用例）：

```
--- FAIL: TestYuxiboxSeedanceAcceptsEveryUpstreamModel/seedance-2.5（YX）
    yuxibox_seedance_video_test.go:37: upstream model "seedance-2.5（YX）" rejected by local validation: yuxibox 渠道模型必须为 seedance2.5-yx-count。
--- FAIL: .../seedance-2.5（YX）-按次
--- FAIL: .../seedance-2.0（YX）
--- FAIL: .../seedance-2.5-yx-second
```

报错文案与用户生产任务 `9ff2f82493e165e00ea6d2a58796243c` 的 `error` 完全一致，确认为同一根因。

GREEN（修复后）：

```
--- PASS: TestYuxiboxSeedanceAcceptsEveryUpstreamModel (0.00s)
    --- PASS: .../seedance2.5-yx-count
    --- PASS: .../seedance-2.5（YX）
    --- PASS: .../seedance-2.5（YX）-按次
    --- PASS: .../seedance-2.0（YX）
    --- PASS: .../seedance-2.5-yx-second
--- PASS: TestYuxiboxSeedancePackage
--- PASS: TestYuxiboxSeedanceCreateAssembly
--- PASS: TestYuxiboxSeedanceValidation（保留的 7 条契约校验仍全部生效）
--- PASS: TestYuxiboxSeedancePollAndResponse
ok  	yingce/backend/internal/protocol	1.308s
```

包一致性验证：`TestOfficialProtocolPackagesAreSelfContainedDeclarativePlugins` 通过（重建后的 `.yingce-plugin` 内 manifest 与源码逐字节一致，全仓 70+ 插件无重复 provider id）。

## 上游事实核对（不属于代码缺陷，移交用户）

用渠道 `CHANNEL_000006` 的真实 Key 直接请求 `GET https://yuxibox.cn/v1/models`（返回 20 个模型）与 `POST https://yuxibox.cn/v1/videos`（仅发 `model` 字段，缺 prompt 在上游创建前被拒，不产生任务）：

| 模型名 | `/v1/models` 中 | `/v1/videos` 响应 |
| --- | --- | --- |
| `seedance2.5-yx-count` | 有 | `400 YX: prompt must contain 1-16000 characters` |
| `seedance-2.5（YX）` | 有 | 同上 |
| `seedance-2.5（YX）-按次` | 有 | 同上 |
| `seedance-2.0（YX）` | 有 | 同上 |
| `seedance-2.5-yx` | 无 | `503 model_not_found` |
| `seedance-2.5-yx-second` | 无 | `503 model_not_found: the model is claimed by a task plugin, which has no enabled channel serving it` |

**因此 `seedance-2.5-yx-second` 在本修复后仍无法生成**——上游认领了这个模型名但没有可用渠道在服务。用户需要在管理后台把 `宇熙Box` 渠道下该模型的「上游模型标识」改为 `seedance-2.5（YX）`（0.30 元/秒，5–30 秒）或 `seedance2.5-yx-count`（4 元/条，10–30 秒）。这属于渠道配置数据，不在本 change 范围内。

## 探针副作用说明

核对上游契约时，一次探测带了合法参数（prompt/seconds/ratio + 一个不可达的 `reference_images`），意外在上游创建了两个任务：

- `task_obid05uPqkzSEQqT4zPQqhioPA0vuDQV`（`seedance2.5-yx-count`）
- `task_wKghR4ePcYu8uWgEJxDgoU0ApxVQ7q4i`（`seedance-2.5（YX）`）

两者均因参考图不可访问而 `status: failed`（`YX_VIDEO_FAILED`），未产生成片。上游不提供取消接口（`DELETE /v1/videos/{id}` 与 `POST .../cancel` 均 404）。两个任务都是失败态，未消耗成功出片费用。

## 已知的预存测试失败（与本改动无关）

`go test ./internal/app/ -run 'Channel|Model'` 有 9 项失败，根因均为 Windows 临时目录 `sync ... Access is denied`。已在基线 commit `7b7e6c70`（回退本改动后）复现同样失败，确认是环境预存问题，不是本 change 引入的回归。

## 未覆盖项

- 未做真实付费生成验收（需用户用自己的 Key 与额度）。插件请求形状由 `TestYuxiboxSeedanceCreateAssembly` 逐字段覆盖。
- 未验证前端 UI 中该模型下拉/时长档位的展示（本 change 不改前端；各模型时长档位差异属于后续独立能力）。
