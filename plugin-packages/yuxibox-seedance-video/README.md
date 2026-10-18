# yuxibox Seedance

该目录是独立官方协议插件源码。后端从生成的 `yuxibox-seedance-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

yuxibox（yuxibox.cn）中转 seedance2.5-yx-count 视频模型，提供 OpenAI Video 风格异步协议（`POST /v1/videos` 创建 + `GET /v1/videos/{task_id}` 查询）：文生视频与多模态参考生视频（参考图 ≤ 30、参考视频 ≤ 10、参考音频 ≤ 10），时长 4–30 秒默认 30，输出固定 720P，七档比例统一走 `ratio` 单字段（不发 `size`）。参考素材走 JSON 数组（`reference_images`/`videos`/`audios`），须为公网 HTTPS 或 data URL；不发送 multipart `input_reference`。按条计费，基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响。

完整接口见 [docs/interface.md](docs/interface.md)。
