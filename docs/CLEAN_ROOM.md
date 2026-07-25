# Clean-room 复刻说明

## 目标

在功能上对齐「AI 一站式短剧生成」工作流，使用 **Go 后端 + Vue 前端** 独立实现，避免直接复制 [chatfire-AI/huobao-drama](https://github.com/chatfire-AI/huobao-drama)。

## 上游许可风险

- 上游 README 徽章为 **CC BY-NC-SA 4.0**（禁止商业使用；衍生作品需同样协议）。
- GitHub `license` 字段为空，以 README 声明为准时应按最严格解释处理。
- **直接 fork / 复制源码 / 复制 UI 品牌与素材** 无法规避版权与协议约束。

## 允许 vs 不允许

| 允许 | 不允许 |
|------|--------|
| 阅读公开 README/文档理解功能边界 | 复制 TypeScript/Vue 源码文件 |
| 自研 API 形状与数据模型 | 复制 `skills/` 原文、logo、demo 视频 |
| 调用同一第三方 AI 厂商官方 API | 假冒「火宝/Huobao」品牌 |
| 使用通用算法（FFmpeg 合成等） | 把 CC BY-NC-SA 代码塞进 Apache 仓库 |

## Go 复刻是否可行？

**可行，且更有利于 clean-room。**

| 维度 | 说明 |
|------|------|
| HTTP API | Gin 完全可覆盖 REST CRUD |
| SQLite | GORM/modernc 等价 Drizzle |
| Agent | 不依赖 Mastra；用 OpenAI tool-plan + 服务端工具执行 |
| 图/视频/TTS | Adapter 模式对接多厂商 |
| FFmpeg | `os/exec` 调用，与 Node fluent-ffmpeg 能力等价 |
| 前端 | 保持 Vue 或可换任意 SPA；与后端语言解耦 |

## 功能对等矩阵（实现状态）

| 能力 | 状态 |
|------|------|
| 项目/集/角色/场景/分镜 CRUD | ✅（项目页与工作台入口均已齐） |
| 5 类 Agent + skills 注入 | ✅（自研 runner） |
| AI 配置/厂商模板/音色 | ✅ |
| 图片/视频/TTS 生成管线 | ✅（adapter 可按厂商继续加深） |
| FFmpeg 合成/拼接/宫格切分 | ✅ |
| 工作台 UI 全流程 | ✅ |
| 项目资产页角色/场景/道具管理 | ✅ |
| 全厂商细节字段 100% 对齐 | 🔄 非阻塞；按真实账号 smoke 加深 |

## 建议

1. 商用发布前请法务复核第三方模型服务条款。
2. 不要把上游仓库作为 git submodule 或 vendored 源码。
3. 文档中可致谢「公开产品思路参考」，但避免「官方复刻/兼容火宝」等易混淆表述。
