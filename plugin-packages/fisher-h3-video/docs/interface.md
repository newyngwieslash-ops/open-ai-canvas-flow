# Fisher-H3 接口字段

## 协议身份

- 插件 ID：`fisher-h3-video`。
- Provider ID：`fisher-h3-video`。
- 能力：`video`。
- 默认 Base URL：`https://api.work-fisher.com`。
- 鉴权驱动：`bearer`。
- 创建：`POST /v2/video_generation`。
- 查询：`GET /v2/query/video_generation/{{taskId}}`。

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
| `videos` | media[] | 否 | `reference video` | 参考视频。 |
| `audios` | media[] | 否 | `reference audio/voice` | 参考音频或音色。 |
| `duration` | integer | 是 | `duration/seconds` | 时长秒数。 |
| `aspectRatio` | string | 否 | `ratio/aspect_ratio/size` | 画幅比例或尺寸。 |
| `resolution` | string | 是 | `resolution` | 分辨率档位。 |
| `providerOptions` | object | 否 | `provider-specific fields` | 插件命名空间内的厂商扩展字段。 |

## 上游请求模板逐字段清单

下表由插件请求模板生成，覆盖 body、query、headers 和 multipart 文件声明中的每个字段。

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v2/video_generation"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `{"$ref":"request.model"}` |
| `create.body.content` | `{"$concatArrays":[[{"type":"text","text":{"$ref":"request.prompt"}}],{"$map":{"from":{"$sortByOrder":{"$ref":"request.images"}},"as":"media","in":{"type":"image_url","image_url":{"url":{"$ref":"media.value"}},"role":{"$coalesce":[{"$ref":"media.role"},"first_frame"]}}}},{"$map":{"from":{"$sortByOrder":{"$ref":"request.videos"}},"as":"media","in":{"type":"video_url","video_url":{"url":{"$ref":"media.value"}},"role":"reference_video","start_time_seconds":{"$ref":"media.metadata.start_time_seconds"}}}},{"$map":{"from":{"$sortByOrder":{"$ref":"request.audios"}},"as":"media","in":{"type":"audio_url","audio_url":{"url":{"$ref":"media.value"}},"role":{"$coalesce":[{"$ref":"media.role"},"reference_audio"]}}}}]}` |
| `create.body.resolution` | `{"$upper":{"$ref":"request.resolution"}}` |
| `create.body.duration` | `{"$ref":"request.duration"}` |
| `create.body.ratio` | `{"$coalesce":[{"$ref":"request.aspectRatio"},"16:9"]}` |
| `create.body.audio_control.mode` | `{"$coalesce":[{"$ref":"request.providerOptions.fisher-h3-video.audio_control.mode"},{"$if":{"condition":{"$gt":[{"$len":{"$filter":{"from":{"$ref":"request.audios"},"as":"media","where":{"$eq":[{"$coalesce":[{"$ref":"media.role"},"reference_audio"]},"drive_audio"]}}}},0]},"then":"lock_source","else":"native"}}]}` |
| `create.body.audio_control.denoise_strength` | `{"$coalesce":[{"$ref":"request.providerOptions.fisher-h3-video.audio_control.denoise_strength"},{"$if":{"condition":{"$gt":[{"$len":{"$filter":{"from":{"$ref":"request.audios"},"as":"media","where":{"$eq":[{"$coalesce":[{"$ref":"media.role"},"reference_audio"]},"drive_audio"]}}}},0]},"then":0,"else":0.35}}]}` |
| `create.body.audio_control.add_drive_as_reference` | `{"$coalesce":[{"$ref":"request.providerOptions.fisher-h3-video.audio_control.add_drive_as_reference"},{"$gt":[{"$len":{"$filter":{"from":{"$ref":"request.audios"},"as":"media","where":{"$eq":[{"$coalesce":[{"$ref":"media.role"},"reference_audio"]},"drive_audio"]}}}},0]}]}` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/v2/query/video_generation/{{taskId}}"` |
| `poll.contentType` | `"application/json"` |

## Provider 扩展键

- `providerOptions.fisher-h3-video.audio_control.add_drive_as_reference`
- `providerOptions.fisher-h3-video.audio_control.denoise_strength`
- `providerOptions.fisher-h3-video.audio_control.mode`

动态模型或工作流允许使用文档声明的完整 `parameters/input/extra_body` 对象；该对象是协议本身的开放 schema，不会被宿主裁剪。

## 响应映射逐字段清单

| 映射位置 | 上游路径或转换表达式 |
| --- | --- |
| `response.taskId` | `{"$coalesce":[{"$ref":"response.task_id"},{"$ref":"taskId"}]}` |
| `response.status` | `{"$coalesce":[{"$ref":"response.task.status"},"pending"]}` |
| `response.message` | `{"$coalesce":[{"$ref":"response.task.error.message"},{"$ref":"response.error.message"},{"$ref":"response.task.message"}]}` |
| `response.videos` | `{"$ref":"response.task.content.url"}` |
| `response.errorPaths[0]` | `"task.error.code"` |
| `response.errorPaths[1]` | `"error.type"` |
| `response.resultEphemeral` | `true` |

## 响应与错误

插件把上游 task/status/text/media/usage 映射为统一结果。临时媒体 URL 标记为 ephemeral，由宿主立即下载持久化。HTTP 错误、业务 code 和 error object 保持失败语义，不包装成成功。

## 兼容边界

Fisher 服务 MiniMax V2 扩展，模型名称精确为 MiniMax-H3。普通任务 4–15 秒整数，独立驱动音频可达 60 秒；最多 9 张参考图、各 1 张首尾帧、3 条参考视频、3 条参考音频和额外 1 条驱动音频，允许混合和仅尾帧。纯文本必须指定固定比例，参考省略时 16:9；adaptive/auto 只适用于关键帧，选择最近固定比例。每条视频及偏移后剩余为 2–15 秒，start_time_seconds 按 24 FPS 换算。音频默认有驱动 lock_source/0/true，无驱动 native/0.35/false；reference_only 要求驱动作为参考。480P/768P/1080P 标准参考价每秒 0.15/0.30/0.45 元，实际取决于账户分组和折扣；1080P 16:9 原生对齐输出 1920×1088，17n+5 帧对齐可能略长，按请求秒数计费。不提供 2K、Context-IR、再生成、seed、采样步数或 flow shift；不接收 target/conditions。创建只读取 task_id；超时、5xx、无效响应不自动重建，宿主保留 X-Task-Id 后查询。真实 Key 的付费生成尚待用户验收。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "fisher-h3-video",
  "name": "Fisher-H3",
  "version": "2.0.0",
  "author": "Fisher / 源铭振跃",
  "description": "Fisher-H3 独立请求协议插件。",
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
        "id": "fisher-h3-video",
        "label": "MiniMax-H3",
        "providerId": "fisher-h3-video",
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
            "name": "resolution",
            "type": "string",
            "required": true,
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
          "model": "MiniMax-H3",
          "resolution": "768P",
          "duration": 5,
          "aspectRatio": "16:9"
        }
      }
    ],
    "providers": [
      {
        "id": "fisher-h3-video",
        "label": "Fisher-H3",
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
        "baseUrl": "https://api.work-fisher.com",
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
            "name": "resolution",
            "type": "string",
            "required": true,
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
                "MiniMax-H3"
              ]
            },
            "message": "Fisher-H3 模型必须为 MiniMax-H3。"
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
                    10000
                  ]
                }
              ]
            },
            "message": "Fisher-H3 提示词不能为空且最多 10000 字符。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$lower": {
                    "$ref": "request.resolution"
                  }
                },
                [
                  "480p",
                  "768p",
                  "1080p"
                ]
              ]
            },
            "message": "Fisher-H3 分辨率仅支持 480P、768P、1080P。"
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
                    {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$ref": "request.audios"
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$coalesce": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "reference_audio"
                                        ]
                                      },
                                      "drive_audio"
                                    ]
                                  }
                                }
                              }
                            },
                            0
                          ]
                        },
                        "then": 60,
                        "else": 15
                      }
                    }
                  ]
                }
              ]
            },
            "message": "Fisher-H3 时长为 4–15 秒；独立驱动音频为 4–60 秒。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$coalesce": [
                    {
                      "$ref": "request.aspectRatio"
                    },
                    {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$add": [
                                {
                                  "$len": {
                                    "$ref": "request.images"
                                  }
                                },
                                {
                                  "$len": {
                                    "$ref": "request.videos"
                                  }
                                },
                                {
                                  "$len": {
                                    "$ref": "request.audios"
                                  }
                                }
                              ]
                            },
                            0
                          ]
                        },
                        "then": "16:9",
                        "else": ""
                      }
                    }
                  ]
                },
                {
                  "$if": {
                    "condition": {
                      "$gt": [
                        {
                          "$add": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$ref": "request.images"
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$coalesce": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      },
                                      "first_frame"
                                    ]
                                  }
                                }
                              }
                            },
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$ref": "request.images"
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$coalesce": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      },
                                      "last_frame"
                                    ]
                                  }
                                }
                              }
                            }
                          ]
                        },
                        0
                      ]
                    },
                    "then": [
                      "1:1",
                      "2:3",
                      "3:2",
                      "3:4",
                      "4:3",
                      "9:16",
                      "16:9",
                      "21:9",
                      "adaptive",
                      "auto"
                    ],
                    "else": [
                      "1:1",
                      "2:3",
                      "3:2",
                      "3:4",
                      "4:3",
                      "9:16",
                      "16:9",
                      "21:9"
                    ]
                  }
                }
              ]
            },
            "message": "Fisher-H3 需要固定比例；adaptive/auto 仅适用于首尾帧。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "first_frame"
                                ]
                              },
                              "reference_image"
                            ]
                          }
                        }
                      }
                    },
                    9
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "first_frame"
                                ]
                              },
                              "first_frame"
                            ]
                          }
                        }
                      }
                    },
                    1
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "first_frame"
                                ]
                              },
                              "last_frame"
                            ]
                          }
                        }
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "Fisher-H3 最多 9 张参考图及各 1 张首尾帧。"
          },
          {
            "assert": {
              "$eq": [
                {
                  "$len": {
                    "$filter": {
                      "from": {
                        "$ref": "request.images"
                      },
                      "as": "media",
                      "where": {
                        "$in": [
                          {
                            "$coalesce": [
                              {
                                "$ref": "media.role"
                              },
                              "first_frame"
                            ]
                          },
                          [
                            "reference_image",
                            "first_frame",
                            "last_frame"
                          ]
                        ]
                      }
                    }
                  }
                },
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                }
              ]
            },
            "message": "Fisher-H3 图片角色无效。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$lte": [
                    {
                      "$len": {
                        "$ref": "request.videos"
                      }
                    },
                    3
                  ]
                },
                {
                  "$eq": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.videos"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "reference_video"
                                ]
                              },
                              "reference_video"
                            ]
                          }
                        }
                      }
                    },
                    {
                      "$len": {
                        "$ref": "request.videos"
                      }
                    }
                  ]
                }
              ]
            },
            "message": "Fisher-H3 最多 3 条 reference_video。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "reference_audio"
                                ]
                              },
                              "reference_audio"
                            ]
                          }
                        }
                      }
                    },
                    3
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "reference_audio"
                                ]
                              },
                              "drive_audio"
                            ]
                          }
                        }
                      }
                    },
                    1
                  ]
                },
                {
                  "$eq": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$in": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "reference_audio"
                                ]
                              },
                              [
                                "reference_audio",
                                "drive_audio"
                              ]
                            ]
                          }
                        }
                      }
                    },
                    {
                      "$len": {
                        "$ref": "request.audios"
                      }
                    }
                  ]
                }
              ]
            },
            "message": "Fisher-H3 最多 3 条参考音频和 1 条独立驱动音频。"
          },
          {
            "assert": {
              "$in": [
                {
                  "$coalesce": [
                    {
                      "$ref": "request.providerOptions.fisher-h3-video.audio_control.mode"
                    },
                    {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$ref": "request.audios"
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$coalesce": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "reference_audio"
                                        ]
                                      },
                                      "drive_audio"
                                    ]
                                  }
                                }
                              }
                            },
                            0
                          ]
                        },
                        "then": "lock_source",
                        "else": "native"
                      }
                    }
                  ]
                },
                [
                  "native",
                  "lock_source",
                  "remix_source",
                  "reference_only"
                ]
              ]
            },
            "message": "Fisher-H3 音频模式无效。"
          },
          {
            "assert": {
              "$if": {
                "condition": {
                  "$ne": [
                    {
                      "$coalesce": [
                        {
                          "$ref": "request.providerOptions.fisher-h3-video.audio_control.mode"
                        },
                        {
                          "$if": {
                            "condition": {
                              "$gt": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$ref": "request.audios"
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$coalesce": [
                                              {
                                                "$ref": "media.role"
                                              },
                                              "reference_audio"
                                            ]
                                          },
                                          "drive_audio"
                                        ]
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            },
                            "then": "lock_source",
                            "else": "native"
                          }
                        }
                      ]
                    },
                    "native"
                  ]
                },
                "then": {
                  "$gt": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "reference_audio"
                                ]
                              },
                              "drive_audio"
                            ]
                          }
                        }
                      }
                    },
                    0
                  ]
                },
                "else": true
              }
            },
            "message": "Fisher-H3 非 native 模式要求驱动音频。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$eq": [
                    {
                      "$coalesce": [
                        {
                          "$ref": "request.providerOptions.fisher-h3-video.audio_control.denoise_strength"
                        },
                        {
                          "$if": {
                            "condition": {
                              "$gt": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$ref": "request.audios"
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$coalesce": [
                                              {
                                                "$ref": "media.role"
                                              },
                                              "reference_audio"
                                            ]
                                          },
                                          "drive_audio"
                                        ]
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            },
                            "then": 0,
                            "else": 0.35
                          }
                        }
                      ]
                    },
                    {
                      "$toFloat": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.fisher-h3-video.audio_control.denoise_strength"
                          },
                          {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$len": {
                                      "$filter": {
                                        "from": {
                                          "$ref": "request.audios"
                                        },
                                        "as": "media",
                                        "where": {
                                          "$eq": [
                                            {
                                              "$coalesce": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "reference_audio"
                                              ]
                                            },
                                            "drive_audio"
                                          ]
                                        }
                                      }
                                    }
                                  },
                                  0
                                ]
                              },
                              "then": 0,
                              "else": 0.35
                            }
                          }
                        ]
                      }
                    }
                  ]
                },
                {
                  "$gte": [
                    {
                      "$coalesce": [
                        {
                          "$ref": "request.providerOptions.fisher-h3-video.audio_control.denoise_strength"
                        },
                        {
                          "$if": {
                            "condition": {
                              "$gt": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$ref": "request.audios"
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$coalesce": [
                                              {
                                                "$ref": "media.role"
                                              },
                                              "reference_audio"
                                            ]
                                          },
                                          "drive_audio"
                                        ]
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            },
                            "then": 0,
                            "else": 0.35
                          }
                        }
                      ]
                    },
                    0
                  ]
                },
                {
                  "$lte": [
                    {
                      "$coalesce": [
                        {
                          "$ref": "request.providerOptions.fisher-h3-video.audio_control.denoise_strength"
                        },
                        {
                          "$if": {
                            "condition": {
                              "$gt": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$ref": "request.audios"
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$coalesce": [
                                              {
                                                "$ref": "media.role"
                                              },
                                              "reference_audio"
                                            ]
                                          },
                                          "drive_audio"
                                        ]
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            },
                            "then": 0,
                            "else": 0.35
                          }
                        }
                      ]
                    },
                    1
                  ]
                }
              ]
            },
            "message": "Fisher-H3 去噪强度须为 0–1 数字。"
          },
          {
            "assert": {
              "$and": [
                {
                  "$in": [
                    {
                      "$coalesce": [
                        {
                          "$ref": "request.providerOptions.fisher-h3-video.audio_control.add_drive_as_reference"
                        },
                        {
                          "$gt": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$ref": "request.audios"
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$coalesce": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "reference_audio"
                                        ]
                                      },
                                      "drive_audio"
                                    ]
                                  }
                                }
                              }
                            },
                            0
                          ]
                        }
                      ]
                    },
                    [
                      true,
                      false
                    ]
                  ]
                },
                {
                  "$if": {
                    "condition": {
                      "$eq": [
                        {
                          "$coalesce": [
                            {
                              "$ref": "request.providerOptions.fisher-h3-video.audio_control.mode"
                            },
                            {
                              "$if": {
                                "condition": {
                                  "$gt": [
                                    {
                                      "$len": {
                                        "$filter": {
                                          "from": {
                                            "$ref": "request.audios"
                                          },
                                          "as": "media",
                                          "where": {
                                            "$eq": [
                                              {
                                                "$coalesce": [
                                                  {
                                                    "$ref": "media.role"
                                                  },
                                                  "reference_audio"
                                                ]
                                              },
                                              "drive_audio"
                                            ]
                                          }
                                        }
                                      }
                                    },
                                    0
                                  ]
                                },
                                "then": "lock_source",
                                "else": "native"
                              }
                            }
                          ]
                        },
                        "reference_only"
                      ]
                    },
                    "then": {
                      "$eq": [
                        {
                          "$coalesce": [
                            {
                              "$ref": "request.providerOptions.fisher-h3-video.audio_control.add_drive_as_reference"
                            },
                            {
                              "$gt": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$ref": "request.audios"
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$coalesce": [
                                              {
                                                "$ref": "media.role"
                                              },
                                              "reference_audio"
                                            ]
                                          },
                                          "drive_audio"
                                        ]
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            }
                          ]
                        },
                        true
                      ]
                    },
                    "else": true
                  }
                }
              ]
            },
            "message": "Fisher-H3 驱动作为参考须为布尔值，reference_only 必须为 true。"
          },
          {
            "assert": {
              "$eq": [
                {
                  "$len": {
                    "$filter": {
                      "from": {
                        "$ref": "request.videos"
                      },
                      "as": "media",
                      "where": {
                        "$and": [
                          {
                            "$eq": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.metadata.start_time_seconds"
                                  },
                                  0
                                ]
                              },
                              {
                                "$toFloat": {
                                  "$coalesce": [
                                    {
                                      "$ref": "media.metadata.start_time_seconds"
                                    },
                                    0
                                  ]
                                }
                              }
                            ]
                          },
                          {
                            "$gte": [
                              {
                                "$coalesce": [
                                  {
                                    "$ref": "media.metadata.start_time_seconds"
                                  },
                                  0
                                ]
                              },
                              0
                            ]
                          }
                        ]
                      }
                    }
                  }
                },
                {
                  "$len": {
                    "$ref": "request.videos"
                  }
                }
              ]
            },
            "message": "Fisher-H3 视频起始偏移须为非负数字。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v2/video_generation",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "content": {
              "$concatArrays": [
                [
                  {
                    "type": "text",
                    "text": {
                      "$ref": "request.prompt"
                    }
                  }
                ],
                {
                  "$map": {
                    "from": {
                      "$sortByOrder": {
                        "$ref": "request.images"
                      }
                    },
                    "as": "media",
                    "in": {
                      "type": "image_url",
                      "image_url": {
                        "url": {
                          "$ref": "media.value"
                        }
                      },
                      "role": {
                        "$coalesce": [
                          {
                            "$ref": "media.role"
                          },
                          "first_frame"
                        ]
                      }
                    }
                  }
                },
                {
                  "$map": {
                    "from": {
                      "$sortByOrder": {
                        "$ref": "request.videos"
                      }
                    },
                    "as": "media",
                    "in": {
                      "type": "video_url",
                      "video_url": {
                        "url": {
                          "$ref": "media.value"
                        }
                      },
                      "role": "reference_video",
                      "start_time_seconds": {
                        "$ref": "media.metadata.start_time_seconds"
                      }
                    }
                  }
                },
                {
                  "$map": {
                    "from": {
                      "$sortByOrder": {
                        "$ref": "request.audios"
                      }
                    },
                    "as": "media",
                    "in": {
                      "type": "audio_url",
                      "audio_url": {
                        "url": {
                          "$ref": "media.value"
                        }
                      },
                      "role": {
                        "$coalesce": [
                          {
                            "$ref": "media.role"
                          },
                          "reference_audio"
                        ]
                      }
                    }
                  }
                }
              ]
            },
            "resolution": {
              "$upper": {
                "$ref": "request.resolution"
              }
            },
            "duration": {
              "$ref": "request.duration"
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "16:9"
              ]
            },
            "audio_control": {
              "mode": {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.fisher-h3-video.audio_control.mode"
                  },
                  {
                    "$if": {
                      "condition": {
                        "$gt": [
                          {
                            "$len": {
                              "$filter": {
                                "from": {
                                  "$ref": "request.audios"
                                },
                                "as": "media",
                                "where": {
                                  "$eq": [
                                    {
                                      "$coalesce": [
                                        {
                                          "$ref": "media.role"
                                        },
                                        "reference_audio"
                                      ]
                                    },
                                    "drive_audio"
                                  ]
                                }
                              }
                            }
                          },
                          0
                        ]
                      },
                      "then": "lock_source",
                      "else": "native"
                    }
                  }
                ]
              },
              "denoise_strength": {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.fisher-h3-video.audio_control.denoise_strength"
                  },
                  {
                    "$if": {
                      "condition": {
                        "$gt": [
                          {
                            "$len": {
                              "$filter": {
                                "from": {
                                  "$ref": "request.audios"
                                },
                                "as": "media",
                                "where": {
                                  "$eq": [
                                    {
                                      "$coalesce": [
                                        {
                                          "$ref": "media.role"
                                        },
                                        "reference_audio"
                                      ]
                                    },
                                    "drive_audio"
                                  ]
                                }
                              }
                            }
                          },
                          0
                        ]
                      },
                      "then": 0,
                      "else": 0.35
                    }
                  }
                ]
              },
              "add_drive_as_reference": {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.fisher-h3-video.audio_control.add_drive_as_reference"
                  },
                  {
                    "$gt": [
                      {
                        "$len": {
                          "$filter": {
                            "from": {
                              "$ref": "request.audios"
                            },
                            "as": "media",
                            "where": {
                              "$eq": [
                                {
                                  "$coalesce": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    "reference_audio"
                                  ]
                                },
                                "drive_audio"
                              ]
                            }
                          }
                        }
                      },
                      0
                    ]
                  }
                ]
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v2/query/video_generation/{{taskId}}"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.task.status"
              },
              "pending"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.task.error.message"
              },
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.task.message"
              }
            ]
          },
          "videos": {
            "$ref": "response.task.content.url"
          },
          "errorPaths": [
            "task.error.code",
            "error.type"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
