# yuxibox Seedance 接口字段

## 协议身份

- 插件 ID：`yuxibox-seedance-video`。
- Provider ID：`yuxibox-seedance-video`。
- 能力：`video`。
- 默认 Base URL：`https://yuxibox.cn`。
- 鉴权驱动：`bearer`（配置字段 `apiKey`）。
- 创建：`POST /v1/videos`（application/json）。
- 查询：`GET /v1/videos/{{taskId}}`。
- 取消：上游未提供，manifest 不声明 cancel。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key（Bearer） |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 视频模型 ID，精确 `seedance2.5-yx-count`。 |
| `prompt` | string | 是 | `prompt` | 视频提示词，1–16000 字符。 |
| `images` | media[] | 否 | `reference_images` | 参考图，最多 30 张，URL 数组。 |
| `videos` | media[] | 否 | `videos` | 参考视频，最多 10 条，URL 数组。 |
| `audios` | media[] | 否 | `audios` | 参考音频，最多 10 条，URL 数组。 |
| `duration` | integer | 是 | `seconds` | 时长秒数 4–30，默认 30；以字符串发送。 |
| `aspectRatio` | string | 否 | `ratio` | 画幅比例：adaptive/16:9/9:16/1:1/4:3/3:4/21:9，缺省 16:9；不发 `size`。 |
| `providerOptions` | object | 否 | provider-specific fields | 插件命名空间内的厂商扩展字段。 |

不暴露 `resolution`（输出固定 720P）、`generateAudio`、`watermark`——上游合同无这些字段。

## 请求示例

```json
POST /v1/videos
Authorization: Bearer <apiKey>
Content-Type: application/json

{
  "model": "seedance2.5-yx-count",
  "prompt": "少女在雨巷中转身回望",
  "seconds": "30",
  "ratio": "16:9",
  "reference_images": ["https://example.com/ref-1.jpg"],
  "videos": [],
  "audios": []
}
```

空素材组（`reference_images`/`videos`/`audios` 为空数组）不发送字段键；`seconds` 为字符串（上游示例 `"seconds": "30"`，整数同样接受）。

## 响应与错误

- 任务 ID 提取：`response.id` → `response.task_id` → `response.taskId` → `response.data.id`，提取失败按“结果不确定”失败上抛，不自动重发。
- 状态归一：`response.status`/`response.state`/`response.data.status`，缺省 `pending`。
- 错误消息：`response.error.message` → `response.message` → `response.fail_reason`；`errorPaths: ["error.code"]`。
- 视频地址 coalesce：`response.video_url` → `response.videoUrl` → `response.result_url` → `response.url` → `response.data.video_url` → `response.output.url`；`resultEphemeral: true`，宿主立即下载持久化。
- HTTP 400/401/403/429/5xx 为真实失败上抛，不包装成成功。

## 兼容边界

- 同一视频不重复 POST：宿主恢复合同保证已有任务 ID 只查询不重建（上游错误处理章节要求）。
- 参考素材须为公网 HTTPS URL 或 data URL（`requiresPublicMediaUrls: true`），裸 Base64/内网地址不可用。
- 不发送 multipart `input_reference`（JSON `reference_images` 路径覆盖全部用例）。
- 画幅统一 `ratio` 单字段，`size` 与 `ratio` 不会同时发送。
- 计费按条：基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响，以 yuxibox 控制台为准。
- 真实 Key 的付费生成尚待用户验收。

## 上游请求模板逐字段清单

下表由插件请求模板生成，覆盖 body、query、headers 和 multipart 文件声明中的每个字段。

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v1/videos"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `{"$ref":"request.model"}` |
| `create.body.prompt` | `{"$ref":"request.prompt"}` |
| `create.body.seconds` | `{"$toString":{"$ref":"request.duration"}}` |
| `create.body.ratio` | `{"$coalesce":[{"$ref":"request.aspectRatio"},"16:9"]}` |
| `create.body.reference_images` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.images"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.videos` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.videos"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.audios` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.audios"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/v1/videos/{{taskId}}"` |
| `poll.contentType` | `"application/json"` |

## Provider 扩展键

- 无额外扩展键。

动态模型或工作流允许使用文档声明的完整 `parameters/input/extra_body` 对象；该对象是协议本身的开放 schema，不会被宿主裁剪。

## 响应映射逐字段清单

| 映射位置 | 上游路径或转换表达式 |
| --- | --- |
| `response.taskId` | `{"$coalesce":[{"$ref":"response.id"},{"$ref":"response.task_id"},{"$ref":"response.taskId"},{"$ref":"response.data.id"},{"$ref":"taskId"}]}` |
| `response.status` | `{"$coalesce":[{"$ref":"response.status"},{"$ref":"response.state"},{"$ref":"response.data.status"},"pending"]}` |
| `response.message` | `{"$coalesce":[{"$ref":"response.error.message"},{"$ref":"response.message"},{"$ref":"response.fail_reason"}]}` |
| `response.videos` | `{"$coalesce":[{"$ref":"response.video_url"},{"$ref":"response.videoUrl"},{"$ref":"response.result_url"},{"$ref":"response.url"},{"$ref":"response.data.video_url"},{"$ref":"response.output.url"}]}` |
| `response.errorPaths[0]` | `"error.code"` |
| `response.resultEphemeral` | `true` |

## 响应与错误

插件把上游 task/status/text/media/usage 映射为统一结果。临时媒体 URL 标记为 ephemeral，由宿主立即下载持久化。HTTP 错误、业务 code 和 error object 保持失败语义，不包装成成功。

## 兼容边界

yuxibox（yuxibox.cn）中转 seedance2.5-yx-count：OpenAI Videos 风格异步合同，创建 POST /v1/videos JSON、查询 GET /v1/videos/{task_id}。模型名称精确为 seedance2.5-yx-count，时长 4–30 秒整数默认 30，输出固定 720P，七档比例（adaptive/16:9/9:16/1:1/4:3/3:4/21:9）统一走 ratio 单字段（不发 size）。参考素材 JSON 数组：reference_images ≤ 30、videos ≤ 10、audios ≤ 10，须为公网 HTTPS 或 data URL；不发 multipart input_reference。seconds 以字符串发送（上游示例 "seconds": "30"）。按条计费基础价 4.5 元/条，最终费用受令牌分组倍率及请求规格影响，以 yuxibox 控制台为准。同一视频不重复提交：宿主恢复合同保证已有任务 ID 只查询不重建；创建响应缺失任务 ID 按“结果不确定”失败上抛。上游未提供取消接口。真实 Key 的付费生成尚待用户验收。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "yuxibox-seedance-video",
  "name": "yuxibox Seedance",
  "version": "2.0.0",
  "author": "yuxibox / 影策",
  "description": "yuxibox Seedance 独立请求协议插件。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "workflows": [
      {
        "id": "yuxibox-seedance-video",
        "label": "yuxibox Seedance",
        "providerId": "yuxibox-seedance-video",
        "capability": "video",
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "视频模型 ID。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt/content/input",
            "description": "视频提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "first/last/reference image",
            "description": "显式 role 图片输入。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "reference video",
            "description": "参考视频。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "reference audio/voice",
            "description": "参考音频或音色。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": true,
            "mapping": "duration/seconds",
            "description": "时长秒数。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "ratio/aspect_ratio/size",
            "description": "画幅比例或尺寸。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "插件命名空间内的厂商扩展字段。"
          }
        ],
        "defaults": {
          "model": "seedance2.5-yx-count",
          "duration": 30,
          "aspectRatio": "16:9"
        }
      }
    ],
    "providers": [
      {
        "id": "yuxibox-seedance-video",
        "label": "yuxibox Seedance",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://yuxibox.cn",
        "requiresPublicMediaUrls": true,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "视频模型 ID。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt/content/input",
            "description": "视频提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "first/last/reference image",
            "description": "显式 role 图片输入。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "reference video",
            "description": "参考视频。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "reference audio/voice",
            "description": "参考音频或音色。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": true,
            "mapping": "duration/seconds",
            "description": "时长秒数。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "ratio/aspect_ratio/size",
            "description": "画幅比例或尺寸。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "插件命名空间内的厂商扩展字段。"
          }
        ],
        "validations": [
          {
            "assert": {
              "$eq": [
                {
                  "$ref": "request.model"
                },
                "seedance2.5-yx-count"
              ]
            },
            "message": "yuxibox 渠道模型必须为 seedance2.5-yx-count。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$gt": [
                    {
                      "$len": {
                        "$trim": {
                          "$ref": "request.prompt"
                        }
                      }
                    },
                    0
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$ref": "request.prompt"
                      }
                    },
                    16000
                  ]
                }
              ]
            },
            "message": "yuxibox 提示词不能为空且最多 16000 字符。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$gte": [
                    {
                      "$ref": "request.duration"
                    },
                    4
                  ]
                },
                {
                  "$lte": [
                    {
                      "$ref": "request.duration"
                    },
                    30
                  ]
                }
              ]
            },
            "message": "yuxibox 时长为 4–30 秒整数，默认 30。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$coalesce": [
                    {
                      "$ref": "request.aspectRatio"
                    },
                    "16:9"
                  ]
                },
                [
                  "adaptive",
                  "16:9",
                  "9:16",
                  "1:1",
                  "4:3",
                  "3:4",
                  "21:9"
                ]
              ]
            },
            "message": "yuxibox 比例仅支持 adaptive、16:9、9:16、1:1、4:3、3:4、21:9。"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                30
              ]
            },
            "message": "yuxibox 参考图最多 30 张。"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.videos"
                  }
                },
                10
              ]
            },
            "message": "yuxibox 参考视频最多 10 条。"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.audios"
                  }
                },
                10
              ]
            },
            "message": "yuxibox 参考音频最多 10 条。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "seconds": {
              "$toString": {
                "$ref": "request.duration"
              }
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "16:9"
              ]
            },
            "reference_images": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.images"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "videos": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.videos"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "audios": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.audios"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.taskId"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.status"
              },
              {
                "$ref": "response.state"
              },
              {
                "$ref": "response.data.status"
              },
              "pending"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.fail_reason"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.videoUrl"
              },
              {
                "$ref": "response.result_url"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.data.video_url"
              },
              {
                "$ref": "response.output.url"
              }
            ]
          },
          "errorPaths": [
            "error.code"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
