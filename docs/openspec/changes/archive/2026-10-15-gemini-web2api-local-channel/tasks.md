# Tasks

## 1. 上游就绪检查（失败则停，不配影策）

- [x] 1.1 从上游 kv 取 API key：`python -c "import sqlite3;print(sqlite3.connect(r'F:\AI_APIS\gemini-web2api-go\docs\local\data\gemini.db').execute(\"select v from kv where k='api_key'\").fetchone()[0])"`
- [x] 1.2 `GET http://127.0.0.1:8083/v1/models` 含 `gemini-image` 与 `gemini-video`（缺失 = Cookie 池空/失效，停止）
- [x] 1.3 图片探针：`POST /v1/images/generations` 错模型返回 400 "must be an image model"（证明适配层存在）
- [x] 1.4 视频探针：`POST /v1/videos` JSON `{"model":"gemini-video","prompt":"..."}` 返回 `{"id":"video_...","status":"queued"}`

## 2. 影策渠道配置

- [x] 2.1 取影策管理员会话（本地 dev 账号），确认后端 `127.0.0.1:8178` 在跑（`restart.bat`）
- [x] 2.2 创建渠道：name=`local-gemini-web2api`，baseUrl=`http://127.0.0.1:8083/v1`，apiKey=1.1 的 key，enabled=true
- [x] 2.3 创建图片模型：capability=`image`，protocol=`openai-image`，providerModelKey=`gemini-image`，modelKey=`gemini-nano-banana`，能力配置关闭蒙版/参考图/透明底
- [x] 2.4 创建视频模型：capability=`video`，protocol=`newapi`，providerModelKey=`gemini-video`，modelKey=`gemini-veo-web`，不勾参考视频/参考音频
- [x] 2.5 后台确认两个模型在前台模型列表可见且已启用

## 3. 验收

- [x] 3.1 图片：画布/工作台新建图片任务，模型选 `gemini-nano-banana`，prompt=`白底、一只蓝色陶瓷杯，无文字`，无参考图/蒙版 → 1–3 分钟内出图
- [x] 3.2 视频：新建视频任务，模型选 `gemini-veo-web`，短 prompt，无参考媒体 → 立即返回任务 id 并进入轮询（完成作为加分）

## 4. 收尾

- [x] 4.1 记录渠道 id、前台模型名、成功任务 id（写入变更回复）
- [x] 4.2 若配错需修正的渠道/模型记录已清理或修正，不留悬空配置
