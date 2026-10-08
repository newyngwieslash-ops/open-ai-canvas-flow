# MiniMax-H3 (Metaso)

该目录是独立官方协议插件源码。后端从生成的 `metaso-minimax-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

密塔（metaso.cn）提供 MiniMax V2 协议中转：创建与查询端点路径和官方 MiniMax 插件完全一致，差异仅在 Base URL（`https://metaso.cn/api/minimax`）与参数档位（分辨率 480P/768P/1080P/2K、时长 4–15 秒、六档固定比例）。密塔不接收 `audio_control`、`start_time_seconds`、`generate_audio` 与 `aigc_watermark`，本插件从源头不发送这些字段；不建议把密塔渠道配置成官方 `minimax-video` 协议，独立插件保证协议身份与专属默认值不被混淆。

完整接口见 [docs/interface.md](docs/interface.md)。
