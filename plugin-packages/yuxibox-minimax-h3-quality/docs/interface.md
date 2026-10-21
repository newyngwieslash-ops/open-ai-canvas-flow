# yuxibox minimax-h3-quality 接口字段

## 协议身份

- 插件 ID：`yuxibox-minimax-h3-quality`。
- Provider ID：`yuxibox-minimax-h3-quality`。
- 能力：`video`。
- 默认 Base URL：`https://yuxibox.cn`。
- 鉴权驱动：`bearer`。
- 创建：`POST /v1/videos`。
- 查询：`GET /v1/videos/{{taskId}}`。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 视频模型 ID。 |
| `prompt` | string | 是 | `prompt/content/input` | 视频提示词。 |
| `images` | media[] | 否 | `first/last/reference image` | 显式 role 图片输入。 |
| `videos` | media[] | 否 | `unsupported` | 参考视频。 |
| `audios` | media[] | 否 | `reference audio/voice` | 参考音频或音色。 |
| `duration` | integer | 否 | `duration/seconds` | 时长秒数。 |
| `aspectRatio` | string | 否 | `ratio/aspect_ratio/size` | 画幅比例或尺寸。 |
| `resolution` | string | 否 | `resolution` | 分辨率档位。 |
| `providerOptions` | object | 否 | `provider-specific fields` | 插件命名空间内的厂商扩展字段。 |

## 上游请求模板逐字段清单

下表由插件请求模板生成，覆盖 body、query、headers 和 multipart 文件声明中的每个字段。

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v1/videos"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `{"$ref":"request.model"}` |
| `create.body.prompt` | `{"$ref":"request.prompt"}` |
| `create.body.seconds` | `{"$toInt":{"$coalesce":[{"$ref":"request.duration"},5]}}` |
| `create.body.resolution` | `{"$switch":{"cases":[{"when":{"$eq":[{"$lower":{"$coalesce":[{"$ref":"request.resolution"},"768p"]}},"720p"]},"then":"768p"}],"default":{"$lower":{"$coalesce":[{"$ref":"request.resolution"},"768p"]}}}}` |
| `create.body.ratio` | `{"$coalesce":[{"$ref":"request.aspectRatio"},"9:16"]}` |
| `create.body.reference_images` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.images"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.audios` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.audios"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.seed` | `{"$omitEmpty":{"$ref":"request.providerOptions.yuxibox-minimax-h3-quality.seed"}}` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/v1/videos/{{taskId}}"` |
| `poll.contentType` | `"application/json"` |
| `result.method` | `"GET"` |
| `result.path` | `"/v1/videos/{{taskId}}/content"` |
| `result.contentType` | `"application/json"` |
| `result.headers.Accept` | `"video/mp4"` |

## Provider 扩展键

- `providerOptions.yuxibox-minimax-h3-quality.seed`

动态模型或工作流允许使用文档声明的完整 `parameters/input/extra_body` 对象；该对象是协议本身的开放 schema，不会被宿主裁剪。

## 响应映射逐字段清单

| 映射位置 | 上游路径或转换表达式 |
| --- | --- |
| `response.taskId` | `{"$coalesce":[{"$ref":"response.id"},{"$ref":"response.task_id"},{"$ref":"response.taskId"},{"$ref":"response.data.id"}]}` |
| `response.status` | `{"$coalesce":[{"$ref":"response.status"},{"$ref":"response.state"},{"$ref":"response.data.status"},"queued"]}` |
| `response.message` | `{"$coalesce":[{"$ref":"response.error.message"},{"$ref":"response.message"},{"$ref":"response.fail_reason"}]}` |
| `response.videos` | `{"$coalesce":[{"$ref":"response.video_url"},{"$ref":"response.download_url"},{"$ref":"response.downloadUrl"},{"$ref":"response.videoUrl"},{"$ref":"response.result_url"},{"$ref":"response.url"},{"$ref":"response.data.video_url"}]}` |
| `response.errorPaths[0]` | `"error.code"` |
| `response.resultEphemeral` | `true` |

## 响应与错误

插件把上游 task/status/text/media/usage 映射为统一结果。临时媒体 URL 标记为 ephemeral，由宿主立即下载持久化。HTTP 错误、业务 code 和 error object 保持失败语义，不包装成成功。

## 兼容边界

yuxibox（yuxibox.cn）minimax-h3-quality：OpenAI Video 风格异步合同，创建 POST /v1/videos JSON、查询 GET /v1/videos/{taskId}、二进制下载 GET /v1/videos/{taskId}/content（需带令牌并跟随重定向）。提示词 1–20000 字符，seconds 1–15 整数默认 5，resolution 480p/768p 默认 768p（720p 为 768p 别名，发往上游前归一为 768p），ratio 仅 16:9、9:16、1:1 默认 9:16。reference_images 至少 1 张、最多 9 张公网 HTTPS；audios 0–3 条公网 HTTPS（MP3/WAV/MP4/FLAC）。该模型不支持参考视频：videos 映射为 unsupported，携带参考视频在准入阶段被拒绝，不发上游请求。响应完成态取 video_url，缺失时回退 download_url；/v/ 短链自带签名、有效期 7 天，宿主按 resultEphemeral 立即下载持久化。计费按请求秒数（per_second）：高峰 08:00–24:00 为 480p 0.040、768p 0.060 元/秒；空闲 00:00–08:00 为 480p 0.030、768p 0.050 元/秒，跨时段完成不改已锁定单价。当前价格档模型没有时间窗口选择器，空闲档差价需运营在管理后台单独调整；基础价目按高峰档配置。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "yuxibox-minimax-h3-quality",
  "name": "yuxibox minimax-h3-quality",
  "version": "2.0.0",
  "author": "yuxibox / 影策",
  "description": "yuxibox minimax-h3-quality 独立请求协议插件。",
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
        "id": "yuxibox-minimax-h3-quality",
        "label": "yuxibox minimax-h3-quality",
        "providerId": "yuxibox-minimax-h3-quality",
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
            "mapping": "unsupported",
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
            "required": false,
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
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位。"
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
          "model": "minimax-h3-quality",
          "duration": 5,
          "aspectRatio": "9:16",
          "resolution": "768p"
        }
      }
    ],
    "providers": [
      {
        "id": "yuxibox-minimax-h3-quality",
        "label": "yuxibox minimax-h3-quality",
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
            "mapping": "unsupported",
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
            "required": false,
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
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位。"
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
                "minimax-h3-quality"
              ]
            },
            "message": "yuxibox minimax-h3-quality 渠道模型必须为 minimax-h3-quality。"
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
                    20000
                  ]
                }
              ]
            },
            "message": "提示词不能为空且最多 20000 字符。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$gte": [
                    {
                      "$ref": "request.duration"
                    },
                    1
                  ]
                },
                {
                  "$lte": [
                    {
                      "$ref": "request.duration"
                    },
                    15
                  ]
                }
              ]
            },
            "message": "时长为 1–15 秒整数，默认 5。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$coalesce": [
                    {
                      "$ref": "request.aspectRatio"
                    },
                    "9:16"
                  ]
                },
                [
                  "16:9",
                  "9:16",
                  "1:1"
                ]
              ]
            },
            "message": "比例仅支持 16:9、9:16 或 1:1，默认 9:16。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$lower": {
                    "$coalesce": [
                      {
                        "$ref": "request.resolution"
                      },
                      "768p"
                    ]
                  }
                },
                [
                  "480p",
                  "768p",
                  "720p"
                ]
              ]
            },
            "message": "分辨率仅支持 480p 或 768p，720p 作为 768p 别名。"
          },
          {
            "assert": {
              "$gte": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                1
              ]
            },
            "message": "该模型至少需要 1 张参考图。"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                9
              ]
            },
            "message": "该模型最多支持 9 张参考图。"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.audios"
                  }
                },
                3
              ]
            },
            "message": "该模型最多支持 3 条参考音频。"
          },
          {
            "assert": {
              "$eq": [
                {
                  "$len": {
                    "$ref": "request.videos"
                  }
                },
                0
              ]
            },
            "message": "该模型不支持参考视频，请移除参考视频后重试。"
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
              "$toInt": {
                "$coalesce": [
                  {
                    "$ref": "request.duration"
                  },
                  5
                ]
              }
            },
            "resolution": {
              "$switch": {
                "cases": [
                  {
                    "when": {
                      "$eq": [
                        {
                          "$lower": {
                            "$coalesce": [
                              {
                                "$ref": "request.resolution"
                              },
                              "768p"
                            ]
                          }
                        },
                        "720p"
                      ]
                    },
                    "then": "768p"
                  }
                ],
                "default": {
                  "$lower": {
                    "$coalesce": [
                      {
                        "$ref": "request.resolution"
                      },
                      "768p"
                    ]
                  }
                }
              }
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "9:16"
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
            },
            "seed": {
              "$omitEmpty": {
                "$ref": "request.providerOptions.yuxibox-minimax-h3-quality.seed"
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "result": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}/content",
          "headers": {
            "Accept": "video/mp4"
          }
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
              "queued"
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
                "$ref": "response.download_url"
              },
              {
                "$ref": "response.downloadUrl"
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
