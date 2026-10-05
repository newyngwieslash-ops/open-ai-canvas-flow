# 本地使用排查手册

面向使用者的两个高频问题排查记录。结论均可回溯到代码位置，便于后续版本对照。

## 一、如何替换资产图片（角色定妆 / 场景定稿）

场景：在别处已经生成好了角色定妆照、三视图、场景定稿图，想把它们绑定到项目资产上，而不走平台内 AI 生成。

### 1. 先把图片上传到素材库

绑定选择器**只列出素材库已有的图片素材**，且「绑定图片」弹窗本身没有上传入口（上传属性未传给该弹窗；同页的声音绑定弹窗才有）。

1. 左侧导航栏（最左列）→「资源与工具」分组 → **资产**，进入素材库页（路由 `/assets`）。
2. 页头右侧 → **上传图片**（批量上传按钮）。
3. 上传会自动同步到后端资源库（`POST /api/resources`）。若图片只在浏览器本地缓存、未同步服务端，绑定时会报「所选图片尚未同步到后端资源库」。

注意：项目页（`/projects/:id/assets`，「角色与资产」标签）**不是**素材库页，页头只有「新建文件夹 / 引用素材 / 新建角色」，没有上传功能；「引用素材」也只是引用素材库已有文件，不含上传。

### 2. 绑定到角色卡

- 入口 1：角色卡状态行「图片已绑定」旁的 **更换** 按钮。
- 入口 2（任何角色卡可用，包括未绑定的）：**点击卡片封面** → 「角色卡预览」弹窗 → 右下角 **替换图片** → 选择素材库中的图 → **绑定并生成新版本**。

不要点「初始化 / 初始化三视图 / 重新生成三视图」——那是 AI 三视图生成任务，不是本地图片绑定。

### 生效规则

- 绑定时前端调用 `PUT /projects/:id/characters/:assetId/representations`，这一张图会同时写入 `turnaround_sheet`（三视图）与 `primary`（主图）两个视角，并**创建新的角色版本**（历史版本保留）。
- 后端约束：1–8 张图；视角只能是 `primary / front / side / back / turnaround_sheet / expression_sheet`；同视角不重复；资源必须属于当前用户且为 ready 状态的 image。
- 分镜生成取角色图的优先级：`turnaround_sheet → primary → front → 任意图片`。换图后**新生成**的镜头用新图；已锁定旧版本引用的镜头仍用旧图，需要重新绑定/同步版本引用。
- 场景 / 道具类资产没有独立“representations”概念：上传新图 → 项目页「引用素材」加入 → 在卡片上把分类改成「场景 / 道具」。卡片上的「创建版本」按钮只新增版本记录，不会替换媒体文件。

### 代码位置

- 角色卡与按钮状态：`web/src/pages/projects/detail/project-character-card.tsx`
- 绑定逻辑（含角色、场景入口）：`web/src/pages/projects/detail/assets.tsx`
- 素材库上传按钮：`web/src/pages/assets/index.tsx`
- 上传实现：`web/src/services/api/resources.ts`（`uploadResourceFile`）
- 后端校验：`backend/internal/app/project_character.go`

## 二、画布 Agent 报「请求已被服务端拒绝：请选择后端受管文本模型」

### 原因

报错来自后端校验（`backend/internal/app/cloud_agent.go`）：

```go
} else if req.ChannelID == "" || req.ChannelModelKey == "" {
    return BadAuthRequest("请选择后端受管文本模型；Agent 不接受浏览器自定义密钥或上游地址")
}
```

云端 Agent 只接受两种模型标识：

- `logicalModelId`：管理员配置的逻辑模型（系统渠道 + 价格目录）；
- `channelId + channelModelKey`：且渠道必须是 `scope === "system"` 的**系统渠道**。

如果 Agent 面板当前选中的模型来自**个人渠道**（在浏览器设置里自己填的 Base URL + API Key），前端既给不出 `logicalModelId`（个人渠道没有价格目录），也给不出系统渠道 `channelId`，请求被后端按安全边界拒绝。这是刻意设计：Agent 的模型调用由服务端执行、计费、审计，不能替任意浏览器密钥向上游发请求。普通图片/视频/文本生成任务允许个人渠道密钥，Agent 不允许。

### 解决：在后台把模型配置成系统渠道

1. 管理员账号进入 **后台管理 → 模型与资源 → 系统渠道**（`/admin/channels`）。
2. 新增（或选择）渠道，填上游地址、API Key。
3. **拉取模型**只是把模型名单同步进来，同步后的模型初始为「停用 + 无已启用规格」，**不会出现在前台目录里**。
4. 对需要的模型逐个（或勾选后统一调价）：
   - 配置规格价格并启用（文本模型按 token 计费填输入/输出价；免费可填 0，但必须配置过）；
   - 把模型状态改为 **启用**。
5. 完成后确认：模型行状态为「启用」、规格列不再显示「无已启用规格」、渠道卡片显示「N 个模型」。
6. **刷新浏览器**（或退出重登），让前台重新拉取 `GET /api/model-catalog`。
7. 回到画布 Agent 面板，在输入框下方的模型选择器里**重新选择系统模型**，再发送。

### 目录发布的过滤条件（后台配置自查清单）

`backend/internal/app/model_catalog.go` 中，系统模型进入前台目录需同时满足：

- 渠道已启用；
- 模型已启用（`cm.Enabled`）；
- 模型存在已启用且价格自洽的价格档（`tier.Enabled && tier.PriceConfigured`）。

前台侧还会按「系统模型必须有价格」过滤选项目录（`web/src/stores/config-model-options.ts`，0 元合法，未配置非法）。

### 代码位置

- 后端拒绝与请求校验：`backend/internal/app/cloud_agent.go`
- 前端 Agent 提交逻辑：`web/src/components/canvas/canvas-cloud-agent-panel.tsx`
- 个人渠道/系统渠道解析：`web/src/stores/use-config-store.ts`（`resolveModelRequestConfig`、`logicalModelIDForConfig`）
- 模型目录组装与过滤：`backend/internal/app/model_catalog.go`
- 前台目录拉取与合并：`web/src/lib/user-session.ts`、`web/src/services/api/logical-models.ts`（`GET /api/model-catalog`）
