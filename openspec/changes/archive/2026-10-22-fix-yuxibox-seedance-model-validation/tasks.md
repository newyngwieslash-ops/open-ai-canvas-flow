# 任务

- [x] 1. 写出 RED 回归测试 `TestYuxiboxSeedanceAcceptsEveryUpstreamModel`，确认当前包对 `seedance-2.5（YX）`/`按次`/`2.0（YX）` 报 "yuxibox 渠道模型必须为 seedance2.5-yx-count。"
- [x] 2. 删除 `generate-catalog.mjs` 中 `yuxibox-seedance-video` 的写死模型名 validation，更新 notes 说明多模型共用端点。
- [x] 3. 重新生成 `plugin-packages/yuxibox-seedance-video` 的 manifest.json 与 docs/interface.md，按 build-packages.sh 同款清单重打 `.yingce-plugin`。
- [x] 4. 删除 `TestYuxiboxSeedanceValidation` 中"改模型名应被拒"的用例（它断言的正是被移除的行为）。
- [x] 5. 运行 `go test ./internal/protocol/ -run 'TestYuxibox|TestOfficialProtocolPackagesAreSelfContainedDeclarativePlugins'`，确认 GREEN。
- [x] 6. 同步 `docs/content/docs/backend/http-api.mdx` 中 yuxibox-seedance-video 的模型与计费描述。
