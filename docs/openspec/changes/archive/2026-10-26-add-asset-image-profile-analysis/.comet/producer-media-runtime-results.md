# P3 真实媒体链路补验

本包已完成 Windows C1 受控真实媒体链路、Linux 普通媒体链路和浏览器原生播放/跳转；不是完整 OS 崩溃恢复、正常产品启用、任务5.2或最终Verify。用户追加允许下载ffmpeg，仅下载到忽略的本地工具目录；系统编码器及全局PATH未改，业务库未操作。

## 下载与测试边界

使用 FFmpeg 官方下载页列出的 Gyan Windows 9.0.2 essentials 固定构建。ZIP SHA256 `60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba` 与发布方校验一致；ffmpeg `3256173f3f8bffd7df12227c68adf68025edb1832273a9530688a7bb1ed8edec`、ffprobe `f0d36ecbbdd3bcfac3efa078c96c7271c2e68b3810595552ac3b7f17e9a65c52`，实际具备libx264/libx265/mpeg4/aac。只在测试进程PATH中选择私有目录，不作全局安装。记录位于 `.local/tools/ffmpeg-9.0.2-gyan/`；发布方校验不是FFmpeg源码GPG签名。

新增四个验收文件：共享真实handler专项、默认与Windows tagged测试工厂、仅 `windows && profile_media_acceptance` 存在的app验收构造器。默认产品构建、Service.New、HTTP/env/部署不安装C1控制器。`PROFILE_REAL_MEDIA=1` 启用后缺工具/encoder必须失败；显式C1请求缺Windows/tag/opt-in时拒绝skip或普通路径降级。原C1十二文件hash保持不变，无新schema或依赖。

## 实际结果

| 检查 | 证据与结果 |
| --- | --- |
| 首轮真实上传 | Linux2.08s、Windows12.608s均返回503，测试组合根遗漏ConfigureRuntime；失败日志保留，仅补与main一致的初始化和cleanup，不放宽断言或改生产代码 |
| Linux普通路径 | fix1全2313文件冻结manifest `b378f070320850c28d0a9d254b9ead8f6fcfcc113dc5890f10840b8997ab6c69`；binary `bbbfd81e414a9479b6f5539b2718dcd93d7c325bed6e8d818d7b6434645c7cb5`；四组PASS4.87s，独立见证PASS5.95s，无SKIP |
| Windows C1 fix1 | 四组PASS191.840s，含浏览器等待；66x50素材只记录原生解码，未冒称播放/跳转通过 |
| Windows C1 fix2 | 最终四文件manifest `50f56eb82cc8f52bd2162450db33a3774a58892ee8f50b2a9b187eda4bb83056`；四组PASS156.504s，含浏览器等待；最后可选浏览器样本640x360/6秒，其他样本及Linux无hook路径不变 |
| 浏览器 | IAB直接访问同一临时Gin的真实签名public资源URL，未生成HTML；原生时间轴跳至3秒、播放至3.204461秒、暂停在3.323616秒，readyState4、无媒体错误，稳定截图画面时间码3.300秒与原生0:03/0:06一致 |
| 独立审查 | `.local/profile-media-runtime-review.md`，独立核对四文件接线、默认拒绝降级负检、Linux固定binary运行及Windows实际日志/浏览器证据；原C1初审及唯一复核历史保持 |
| Runtime完整构建 | `568be78e-ef06-4457-9ac2-c0b4d41fba2a`，`go build ./...`，cwd backend，exit0、reused=false、tier=full；首次PowerShell参数封送usage及旧Go1.22选择失败保留，修正为现有Go1.25后按原命令rerun，无源码修改 |

四组为H.265/MPEG4各含/无音频：实际认证multipart上传→原件及playback操作settled/success→H.264/yuv420p/偶数尺寸/AAC按输入存在→普通faststart MP4→实际HTTP全量/范围重构/后缀/416/匿名及跨用户拒绝→完整解码。C1额外断言protocol、原owner、stopped/success及controlled-domain-completed；不能用普通路径通过替代。

日志：`.local/profile-media-runtime-windows-c1{,-fix1,-fix2}.log`、`.local/profile-media-runtime-env/runtime-media{,-v2-fix1}.log`。浏览器证据 `.local/profile-media-browser-c1-fix2-20261024/ui-state.json`、`seek-paused.png`；release只释放测试等待，不构成浏览器通过。第一次小素材交互未完成和工具接口失败也保留，不混作成功证据。全部媒体/SQLite/会话为自产临时夹具，临时HTTP已随测试结束，浏览器页关闭。

Linux复用两个既有离线Debian12镜像：Go1.25/GCC编译自产二进制，原ffmpeg5.1.6镜像运行。UID1000、无网络、cap-drop ALL、只读输入及临时写目录；未拉镜像、移植编码器或挂业务数据。源码/副本/manifest mismatch0，所有自产容器精确名称查询为空。fix2与Linux实际fix1冻结只差可选浏览器样本，分别记录，未伪称同一hash运行。

## 剩余条件

全C1句柄丢失且终态未提交仍unknown；正常产品入口不启用。只读检查已有运行WSL发现UID1000没有可写cgroup委派，未启动/修改发行版、systemd或cgroup，C2尚无可用环境和实施授权。P4仍缺具名端点终态合同、隔离资源/权限/费用/凭据范围；P5仍缺可信manifest和历史writer证据，未读业务库或猜旧key。保持12/13、5.2未勾选、Verify pending、archived=false，不运行Guard推进。
