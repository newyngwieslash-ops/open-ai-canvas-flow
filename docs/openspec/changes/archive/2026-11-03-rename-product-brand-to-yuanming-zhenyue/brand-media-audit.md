# Task10 产品素材与媒体核查

本次从 `7de76015` 开始，仅更新三张已确认含旧品牌的产品截图、移除无运行引用的旧截图并记录素材归属。不改变应用逻辑、主题、布局、第三方内容或任何计划复选框。此处的“已查看”仅指明确列出的图片或视频采样，不代表全部二进制像素均已核验。

## 当前产品截图

截图均由 CUA 浏览器直接捕获当前源码的真实页面；未使用生成界面、DOM mock、请求拦截或独立 HTML。浏览器输出 JPEG，PNG 文件仅做无缩放格式转换。临时截图与运行日志均在 ignored `.local/cache/task10/`。

| 文件 | 真实来源与状态 | 尺寸 / 主题 | 处理 |
| --- | --- | --- | --- |
| `assets/login.png` | `http://localhost:3010/login`，干净 origin，无账号输入，品牌、版权为源铭振跃 | 1280 × 720，浅色 | 重截替换旧字标；影片为运行时真实默认视频帧 |
| `assets/create.png` | `http://127.0.0.1:3010/`，真实首管理员注册后打开的创作页 | 1440 × 1000，浅色 | 重截；空画布，灵感大厅为应用实际内置内容，没有伪造项目 |
| `assets/tldraw-license-key-settings.png` | `http://127.0.0.1:3010/admin/settings/drawing-engine` | 1440 × 1000，浅色 | 重截；真实“未配置”状态、空 License Key；未填写或保存密钥 |
| `web/public/welcome/workbench-create-branded.jpg` | 既有已批准暗色截图；本次直接查看，左上、面包屑和主标题均为源铭振跃 | 1440 × 650，暗色 | 保留原字节和原 untracked 状态，不纳入本次提交。曾临时替换为浅色截图，收到协调者纠正后从 before 副本完全恢复 |
| `web/public/welcome/workbench-create.webp` | 旧产品截图；`story.ts` 已引用上述暗色 JPG | 原路径无运行引用 | 删除不用的重复素材；历史方案与待测试说明中的路径提及不视为运行引用 |

暗色 JPG 恢复后为 50,300 bytes，SHA-256 `7b583b9184219d6daec711132c99eece9da80ad67abbcfec21cbfa59fca4d1fc`，与本任务覆盖前的 ignored before 副本一致。`fantasy-dawn.png`、`paper-grain.svg`、`story.ts`、`index.tsx`、`welcome.css`、`credits.html` 均保留任务开始时的字节。没有暂存这些既有 UI 修改。

`assets/backend.png`、`assets/huabu.png` 为“无限画布”，`assets/canvas.png`、`assets/huabu-info.png`、`assets/project-detail.png`、欢迎页 `workbench-canvas.webp` / `workbench-project.webp` 沿用盘点阶段已经直接查看的证据；本任务不将该历史证据冒充重截或真实路由验收。

## 511 个非文本独立文件的归属闭合

输入复用 `.local/cache/product-brand-task1-baseline.json` 中 511 条 `entry=null / line=0 / non-text` 记录，完整路径副本保留在 ignored `task10/binary-baseline-paths.json`。以下分组互斥、合计 511；这是归属与引用盘点，不是 511 项视觉 PASS。删除旧 WebP 后该基线少一项。

| 组别 / 基线路径边界 | 数量 | 引用或来源依据 | 检查与限制 |
| --- | ---: | --- | --- |
| `assets/` 当前产品截图 | 8 | 三张目标截图与上述五张已有画布/后台截图 | 三张真实更新；其余复用既有人工证据 |
| `assets/user-*` | 28 | 贡献者头像，欢迎页演职员与项目署名 | 第三方个人形象，保留；未逐张检查全部像素 |
| `assets/` 赞助商图片 | 8 | README 赞助商表：artdance、soonstudio、fluxion、metaso、fruivision、xmzm、yuyutech、panda | 第三方 Logo，保留；未逐张像素核验 |
| `assets/qq.jpg`、`assets/wx.jpg` | 2 | 既有上游社群邀请截图，不是赞助商 Logo；本次逐张直接查看，具体证据见下 | 保留二维码和真实群身份；不重绘、不改名、不冒充新品牌官方群 |
| `web/public/inspiration-thumbs/` | 355 | `web/src/lib/inspirations/`、`web/public/inspirations/` 外部来源目录及同步脚本；真实创作页显示作者和 CC BY 4.0 | 第三方灵感缩略图；不改艺术创作、水印、作者；未逐张像素核验 |
| `web/public/short-drama-styles/` | 18 | `canvas-style-picker-modal.tsx`、`canvas-style-system.ts`、工作台示例引用 | 风格/创作样本，保留；未逐张像素核验 |
| `web/public/lighting-presets/` | 8 | `canvas-node-lighting-dialog.tsx` 的光照示例 | 艺术/效果样本，保留；未逐张像素核验 |
| `web/public/images/canvas/` | 4 | `canvas-folder-theme.ts` 的默认/主题封面 | 非字标封面，保留；未逐张像素核验 |
| `web/public/welcome/` | 22 | `story.ts` 与既有 `credits.html`：16 张 Spring / Charge / Wing It! 剧照、1 个 Charge 视频、fantasy-dawn、4 张工作台截图（含旧 WebP） | 视频采样见下；暗色 JPG 直接查看；奇幻概念画与其来源说明保留；其余 16 张剧照未逐张复审，不将第三方内容改成产品所有 |
| `.github/pr-screenshots/` | 16 | PR 证据目录；含 agnes-image 1、canvas-image-tools 4、fork-creation-config 8 与根目录 3 张 before/after/模型参数截图 | 本次查看全部缩略拼图，确认属于历史对比、红箭头说明及用户创作；不是当前营销截图，不重写历史。缩略查看不能证明细小文字全部无旧名 |
| `docs/public/images/canvas-version-history.png` | 1 | 文件是版本预览/恢复功能说明；定向扫描未找到当前源码/专题文档运行引用 | 本次直接查看 1440 × 900 原图；未见目标品牌，保留功能说明及示例文本 |
| `backend/server` 与五个官方支付插件 `backend/provider*` | 36 | 1 个后端程序、每个支付插件 7 个平台二进制 | 执行文件，非产品字标图片；不执行、不改写、不声称二进制内字符串通过 |
| 运行模型 / WASM / 支付 Logo | 5 | `canvas/models/{blaze-face-full-range-sparse.tflite,facecap.glb}`、`mediapipe/wasm/vision_wasm_module_internal.wasm`、`three/basis/basis_transcoder.wasm`、`icons/zhifufm-logo.png` | 技术资源与第三方支付 Logo，保留；没有展开模型、WASM 或 Logo 像素检查 |

`web/public/logo.svg` 是已确认的中性几何 Logo，未重画；`assets/sponsor1.svg` 属赞助商。SVG 不在上述非文本 511 项内，不能把未计入二进制解释为已漏删。其他 SVG/第三方协议图形不因本任务改写。

## 联系方式与二维码归属（交给 Task11 的文案依据）

| 素材 | 直接查看的具体内容 | 归属和后续标签 |
| --- | --- | --- |
| `assets/qq.jpg`，1279 × 2275 | QQ 群邀请卡、已有群号、截断标题“做一个开源并好用的…”、QQ群二维码与“扫一扫二维码，加入群聊”；主标题未见目标旧名 | 原仓库遗留的上游项目 QQ 联系入口；群实际所有人未向服务端核实，不是新品牌官方社群证明。Task11 若介绍它，应标为“上游项目 QQ 群（原有联系入口）” |
| `assets/wx.jpg`，1376 × 1933 | 顶部明确为“群聊：影策”，下方为微信群二维码；底部写有“该二维码7天内（9月3日前）有效，重新进入将更新” | 图中旧名是实际原社群名称，不能改成源铭振跃或重绘二维码。Task11 若介绍它，应标为“上游项目微信群（群名：影策）”，并说明原邀请图可能需更新 |

定向检索 README、`docs/`、`web/src/` 和 `web/public/` 未发现这两张图片的现行文字引用（`rg` 无命中，退出 1）；本任务不自行新增联系标签。没有扫码、访问二维码目标、加群、联系原作者，或验证邀请有效期。二维码、头像、原始群名和原作者身份均原样保留；不能将“旧名保留”误报为新品牌素材替换遗漏。

## 视频和海报的真实检查范围

| 媒体 | 可访问性 / 技术信息 | 看到的范围与结论 |
| --- | --- | --- |
| `/welcome/charge/sequence.mp4` | 本地实际文件，H.264，1280 × 720，24 fps，7.000 秒；全段 ffmpeg 解码退出 0，无音轨 | `fps=1` 查看 0–7 秒范围的 7 张代表帧：工业设施、人物、机器人、电池；未见目标旧品牌。来源按既有 credits 为 Blender Charge teaser 0.5–7.5 秒剪辑。未逐帧阅读全部 168 帧，不宣称全帧像素通过 |
| 默认认证页海报 | `https://i0.hdslb.com/bfs/aitool/aniforge/image/02933f26-5f1b-49ff-a811-b7f95ee5e5b8_m1hc-u0-sau.jpg`；HTTP 200，36,678 bytes，1920 × 1080 | 本次直接查看：暗色背景上的蝴蝶，未见产品字标；保留原来源 |
| 默认认证页视频 | `https://boss-shjd.biliapi.net/updream/aniforge/video/video_bbcb00bd-650d-4249-9346-5cd21fd2484c_m1hc-u0-1pu13x-3v1s.mp4`；HTTP 200 / 断点续传 206，完整 103,857,693 bytes，HEVC 1920 × 1080，24 fps，180.209 秒，AAC 音轨 | 真实登录页能播放；完整文件全段解码退出 0。先查看 `fps=1/5` 的首张 16 格拼图（约前 80 秒），再以原始帧号 `n % 360 == 0` 查看 0、15、30…180 秒共 13 帧，覆盖全片时间跨度：蝴蝶、旋涡、武侠、动漫、演唱会、田园、城市场景、水墨鱼。采样未见目标旧品牌，保留原媒体；未逐帧阅读约 4,325 帧、未听音轨，不宣称全部像素或音频通过 |

默认视频和海报 URL 在 `appearance.go` 与 `use-appearance-store.ts` 中一致，是第三方原始媒体来源；来源名称、人物、艺术内容或水印不能据旧品牌任务擅改。没有更换默认 URL。

## 欢迎页与引用检查

- 真实 `/welcome` 桌面宽度 1440：DOM `scrollWidth=1440`，暖色概念画显示正常；`workbench-create-branded.jpg` 正常加载。最终证据是在恢复暗色原图后重新截取。
- 真实 `/welcome` 窄屏 390 × 844：DOM `scrollWidth=375`（扣除滚动条），四字标题可见；实际滚动到 `scrollY=844`、工作台顶部约 6.68px。截图仅证明此宽度，不延伸到全部设备。
- Tab 首次焦点为“前往工作台介绍”，`:focus-visible=true`，2px 实线焦点；Enter 到 `#workbench`，`scrollY=382`，目标顶部约 15.93px。
- 绘图设置浅色、深色均通过真实主题按钮查看；没有保存任何系统设置。欢迎页保持已批准纸张/胶片视觉，不额外改主题。
- 定向 `rg --hidden --no-ignore` 排除了依赖和构建目录；旧 WebP 仅有历史方案、盘点与待测试文字提及，没有现行应用引用；无需修改 mixed `story.ts` / `credits.html`。历史 `pending-test.mdx` 的旧待办由本核查记录补充，不在此修改其复选框。

没有运行全量 build、测试或类型检查；这些留给 Task13。此任务未改行为逻辑，RED / GREEN 均为 N/A（纯素材、引用盘点及文档），没有伪造失败测试或镜像测试。
