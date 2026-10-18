# yuxibox-seedance-video-provider Specification

## Purpose
yuxibox seedance2.5 视频生成渠道能力：定义渠道协议身份、任务创建与查询合同、参数档位约束、参考素材映射与计费边界，使画布与创作页能通过 yuxibox 渠道完成文生视频和多模态参考生视频。

## Requirements

### Requirement: 渠道协议身份

系统 SHALL 提供声明式协议插件 `yuxibox-seedance-video`，默认 Base URL 为 `https://yuxibox.cn`，Bearer API Key 鉴权，创建端点为 `POST /v1/videos`（JSON 请求体），查询端点为 `GET /v1/videos/{task_id}`。管理员 SHALL 能在渠道配置中一键创建该渠道预设，预设模型为 `seedance2.5-yx-count`。

#### Scenario: 管理员一键创建渠道

- **WHEN** 管理员在个人渠道设置中点击"添加 yuxibox Seedance"按钮
- **THEN** 系统创建一个 Base URL 为 `https://yuxibox.cn`、协议为 `yuxibox-seedance-video`、含模型 `seedance2.5-yx-count` 的渠道，管理员只需填入 API Key 即可使用

#### Scenario: 模型名单校验

- **WHEN** 通过该渠道提交的生成请求未携带模型 `seedance2.5-yx-count`
- **THEN** 请求在提交上游前被拒绝，并提示该渠道模型必须为 `seedance2.5-yx-count`

### Requirement: 参数档位约束

系统 SHALL 将 yuxibox seedance2.5 的生成参数约束为：`prompt` 1–16000 字符；`seconds` 为 4–30 的整数秒且默认 30；画幅二选一——`size`（`1280x720` / `720x1280` / `720x720`）或 `ratio`（`adaptive` / `16:9` / `9:16` / `1:1` / `4:3` / `3:4` / `21:9`），二者不可同时发送；输出分辨率固定 720P，系统 MUST NOT 暴露分辨率档位选择。超出档位的请求 MUST 在提交上游前被拒绝，不得产生计费请求。

#### Scenario: 时长档位校验

- **WHEN** 用户提交时长为 3 秒或 31 秒的生成请求
- **THEN** 请求在提交上游前被拒绝，并提示时长范围为 4–30 秒

#### Scenario: 默认时长

- **WHEN** 用户未显式选择时长提交生成请求
- **THEN** 系统按上游默认 30 秒发送 `seconds`

#### Scenario: 画幅映射

- **WHEN** 用户选择横屏 16:9 画幅
- **THEN** 系统发送 `size: "1280x720"`（或对应 ratio 档位），且不同时发送 size 与 ratio 两个字段

### Requirement: 参考素材映射

系统 SHALL 把统一参考素材映射为上游 JSON 字段：参考图片映射为 `reference_images`（最多 30 条），参考视频映射为 `videos`（最多 10 条），参考音频映射为 `audios`（最多 10 条）；素材以公网 HTTPS URL 或 data URL 提交。超出数量上限的请求 MUST 在提交上游前被拒绝。

#### Scenario: 多参考图图生视频

- **WHEN** 用户连接 3 张参考图提交生成请求
- **THEN** 系统发送 `reference_images` 数组包含 3 个素材 URL

#### Scenario: 参考数量上限

- **WHEN** 用户连接 31 张参考图或 11 条参考视频提交生成请求
- **THEN** 请求在提交上游前被拒绝，并提示该渠道参考素材数量上限

### Requirement: 异步任务查询与结果归一

系统 SHALL 以 `GET /v1/videos/{task_id}` 轮询任务状态，将上游 `status` 归一为本系统状态（`queued`/`pending` 等为等待，`succeeded`/`completed` 为成功，`failed`/`cancelled` 为失败），成功时从响应 URL 字段提取视频地址并持久化，失败时向上呈现上游错误消息。系统 MUST NOT 为同一视频重复提交创建请求：恢复任务已有任务 ID 时只查询不重建。创建响应缺失任务 ID 时 MUST 按"结果不确定"处理，不得引导用户直接重发付费任务。

#### Scenario: 创建与轮询

- **WHEN** 创建请求返回 `{"id": "task_example", "status": "queued"}`
- **THEN** 系统保存任务 ID 并以 `GET /v1/videos/task_example` 轮询，直至状态归一为成功或失败

#### Scenario: 恢复任务不重复计费

- **WHEN** 任务在创建后因轮询失败进入恢复流程且已有任务 ID
- **THEN** 系统只继续查询该任务，不再次调用创建端点

#### Scenario: 结果不确定语义

- **WHEN** 创建请求 HTTP 成功但响应缺失任务 ID
- **THEN** 系统按"结果不确定"报告错误，不自动重发创建请求
