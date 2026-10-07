# Fisher 插件工作包证据

任务：`72abd7c7-a9e6-4141-b88d-343d256d5a6c`。实现范围为独立声明式插件及真实安装包协议检查。

## 文件

- `plugin-packages/generate-catalog.mjs`：新增 Fisher spec，复用既有表达式、映射、文档生成；增加可选 workflow 输出。
- `plugin-packages/fisher-h3-video/manifest.json`、`README.md`、`docs/interface.md`：仅生成 Fisher 目录，声明独立身份、默认地址、Bearer、工作流、参数、校验和响应合同。
- `plugin-packages/fisher-h3-video.yingce-plugin`：包含上述三项，使用标准 ZIP 容器。
- `backend/internal/protocol/fisher_h3_video_test.go`：真实包载入、目录与包清单一致、工作流身份、请求、校验和生命周期检查。

## RED

在 `backend/` 运行：

```powershell
go test ./internal/protocol -run '^TestFisherH3' -count=1
```

退出 1，三个专项测试因 `fisher-h3-video.yingce-plugin` 缺失失败；失败来自真实插件载入路径。

## GREEN

根目录运行 `node plugin-packages/generate-catalog.mjs fisher-h3-video`，仅生成及嵌入 Fisher 文档各一个包。使用 PowerShell 的 `System.IO.Compression.ZipArchive` 将 manifest、README 与 interface 写入 `.yingce-plugin`，未添加依赖。第一次打包后误在根目录运行 Go 检查，因该目录无 Go 包失败；随后在正确 `backend/` 目录运行专项检查成功。

最终检查：

```powershell
gofmt -w internal/protocol/fisher_h3_video_test.go
go test ./internal/protocol -run 'TestFisherH3|TestMiniMaxVideoFailureMessages|TestOfficialVideoProfilesPreserveExplicitMediaRoles|TestOfficialProtocolPackagesAreSelfContainedDeclarativePlugins' -count=1
```

退出 0：`ok infinite-canvas/backend/internal/protocol 2.637s`。

覆盖：仅尾帧与九张参考图混合、三参考音频加一驱动、60 秒驱动输出、1080P/固定比例、偏移内容项、音频默认、显式 0/false、音频冲突、分辨率/比例/角色/独立数量拒绝、严格 task_id、不把 request_id 当任务、queued/running/succeeded/failed/cancelled，以及既有 MiniMax 和所有官方包的文档/注册合同。

`git diff --check` 通过；插件范围 Git 状态仅包含生成器、新 Fisher 目录及安装包，没有其他目录的生成变更。没有真实 Key，未进行付费上游生成。

## 边界

已知素材时长和稳定媒体 ID 的信任边界验证由后端工作包负责；未在 DSL 重复加入完整时长探测策略。创建错误 `X-Task-Id` 的保留和恢复由宿主负责，插件响应只读取真正 `task_id`，不接受追踪 ID。

## 独立审查后的参数合同修复

独立审查发现 P2：通用 videoParams 宣告 generateAudio/watermark，但 Fisher payload 不发送这两个字段；duration/resolution 的必填标记也与 Fisher 合同不符。

新增真实安装包 `TestFisherH3ParameterContract`，先运行 `go test ./internal/protocol -run '^TestFisherH3ParameterContract$' -count=1`：退出 1，实际报两个不支持参数仍被声明以及两个必填字段未标记。然后在 Fisher spec 内过滤不支持字段并单独标记 duration/resolution 必填，同时用于 provider 和 workflow；没有改通用 videoParams 或官方 MiniMax。

仅执行 `node plugin-packages/generate-catalog.mjs fisher-h3-video` 并重新打包 Fisher。最终原专项及目录/相邻 MiniMax 命令退出 0：`ok infinite-canvas/backend/internal/protocol 2.710s`。补 provider/workflow 参数一致断言后，仅重新运行两个相关检查通过。修复后交独立审查者复查，没有对自己实现出具审查 PASS。
