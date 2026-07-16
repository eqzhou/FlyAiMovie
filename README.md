# FlyAiMovie

本地 AI 短剧制作工作台（clean-room 实现）：从大纲/小说到分镜、宫格帧、视频、配音与成片导出。

## 快速开始

```bash
# 后端
cd backend && go build -o ../.run/flyaimovie-server ./cmd/server

# 前端
cd frontend && npm install && npm run build

# PM2（本机约定端口 8088）
pm2 start ecosystem.config.cjs
# 或
pm2 restart flyaimovie
```

打开：http://127.0.0.1:8088

## 制作流水线

1. **项目 / 集**：创建短剧项目，为每集绑定图片 / 视频 / 音频 AI 配置
2. **剧本**：粘贴大纲 → AI 改写为格式化剧本
3. **角色 / 场景**：提取去重、分配音色、生成形象与场景图
4. **宫格帧**：生成宫格提示词 → 出图 → 切分写回分镜首帧
5. **分镜 / 视频**：拆镜、首尾帧、图生视频、配音、镜头合成
6. **导出**：批量合成镜头 → 拼接整集成片

## 内置能力

- Agent：`script_rewriter` / `extractor` / `storyboard_breaker` / `voice_assigner` / `grid_prompt_generator`
- 媒体：图片 / 视频异步轮询 + webhook（`/api/v1/webhooks/vidu`、`/api/v1/webhooks/generic`）
- 宫格历史、任务查询/取消、统一素材库 API、批量帧/视频/配音、道具管理、本地存储 `/static`
- **Mock 厂商**（`provider=mock`）：无外网密钥时也可跑通演示链路

功能对等范围、自动验收状态和 clean-room 红线见 [`docs/FUNCTIONAL_PARITY.md`](docs/FUNCTIONAL_PARITY.md)。
商业发行前的许可证、FFmpeg 和生产上线门槛见 [`docs/COMMERCIAL_RELEASE.md`](docs/COMMERCIAL_RELEASE.md)。
厂商 adapter 的公开协议来源和契约测试见 [`docs/PROVIDER_CONTRACTS.md`](docs/PROVIDER_CONTRACTS.md)。

设置页可添加 OpenAI 兼容、MiniMax、Volcengine、Vidu、Aliyun 等；演示环境已种子 `mock-text/image/video/audio`。

## 配置

- `configs/config.yaml`：端口默认 5679，PM2 通过 `PORT=8088` 覆盖
- 数据：`data/flyaimovie.db`、`data/storage/`
- `server.rate_limit_per_minute`：所有 `/api` 路由按直连来源 IP 限流，默认每分钟 600 次；不信任 `X-Forwarded-For`
- `AI_CONFIG_ENCRYPTION_KEY`：生产环境必填；启用后新旧 AI 厂商密钥会以 AES-GCM 密文保存在数据库中
- 密码恢复生产配置：`SMTP_HOST`、`SMTP_PORT`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`EMAIL_FROM`、`PASSWORD_RESET_URL_BASE`；默认支持 SMTP 587 STARTTLS 与 465 隐式 TLS。

## 健康检查

```bash
curl http://127.0.0.1:8088/api/v1/health
```
