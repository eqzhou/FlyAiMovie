# AI 厂商协议矩阵

本文件记录 FlyAiMovie 厂商适配器的 clean-room 输入来源和可验证协议边界。实现只依据厂商公开 API 文档与自行构造的请求/响应样例，不使用参考项目的 adapter 源码或测试。

| 服务 | 厂商 | 提交协议 | 查询协议 | 自动验证 |
|---|---|---|---|---|
| 图片 | OpenAI | `POST /v1/images/generations` | 同步 | OpenAI-compatible adapter tests |
| 图片 | Gemini | `models/:model:generateContent`，`responseModalities=[TEXT,IMAGE]` | 同步 | `TestGeminiImageContract` |
| 图片 | MiniMax | `POST /v1/image_generation` | 同步 | `TestMiniMaxImageContract` |
| 图片 | Volcengine | `POST /api/v3/images/generations` | `GET /api/v3/images/generations/:id` | `TestVolcengineImageContractAndPoll` |
| 图片 | Aliyun | DashScope async image synthesis | `GET /api/v1/tasks/:id` | `TestDashScopeImageContractAndPoll` |
| 视频 | MiniMax | `POST /v1/video_generation` | query task + files retrieve | `TestMiniMaxVideoSubmitPollAndFileRetrieve` |
| 视频 | Volcengine | Ark `content[]` generation task | `GET /api/v3/contents/generations/tasks/:id` | `TestVolcengineVideoUsesContentContract` |
| 视频 | Vidu | Enterprise v2 `img2video`，Token auth | task creations | `TestViduVideoUsesTokenAndCreationsContract` |
| 视频 | Aliyun | DashScope async video synthesis | `GET /api/v1/tasks/:id` | `TestAliyunVideoUsesAsyncTaskContract` |
| TTS | MiniMax | `POST /v1/t2a_v2` | 同步 hex audio | `minimax_tts_test.go` |

## 官方文档入口

- OpenAI: <https://platform.openai.com/docs/guides/image-generation>
- Google Gemini: <https://ai.google.dev/gemini-api/docs/image-generation>
- MiniMax: <https://platform.minimax.io/docs>
- Volcengine Ark: <https://www.volcengine.com/docs/82379>
- Vidu: <https://platform.vidu.com/docs>
- Aliyun Model Studio / DashScope: <https://help.aliyun.com/zh/model-studio/>

## 验收口径

`httptest` 契约测试证明本项目会生成预期的 URL、鉴权头、JSON 字段并解析公开响应形状，但不能证明厂商账号、模型白名单、额度、区域或未公告变更仍然有效。商业发布前应使用各厂商测试账号执行非 Mock smoke test，并归档日期、区域、模型、request ID 与脱敏响应。

未知厂商或不支持的服务/厂商组合必须明确报错，不得静默落到其他厂商 adapter。API 配置入口和设置页均执行同一支持矩阵。
