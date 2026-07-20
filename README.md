# FlyAiMovie

本地 AI 短剧制作工作台（clean-room 实现）：从大纲/小说到分镜、宫格帧、视频、配音与成片导出。

## 快速开始

```bash
# 构建前端、后端并在本机端口 8088 启动
./scripts/dev-up.sh

# 停止服务
./scripts/dev-down.sh
```

打开：http://127.0.0.1:8088

### Docker 两种模式

```bash
# PostgreSQL：先创建 .env 并设置 POSTGRES_PASSWORD
cp .env.example .env
docker compose up --build

# 单机 SQLite + 本地 data 目录
docker compose -f docker-compose.sqlite.yml up --build
```

两个模式都会检查 HTTP 健康状态，应用启动时自动迁移数据库，并要求镜像内存在 FFmpeg/FFprobe。生产环境必须设置 `APP_ENV=production`、`AUTH_SECURE_COOKIES=true`，并通过 Secret Manager 或部署环境注入 `WEBHOOK_SECRET` 与 `AI_CONFIG_ENCRYPTION_KEY`。

### 本地正式使用

首次打开会进入“初始化制作空间”，创建 owner 账号后再登录。默认本地配置会启用认证、PostgreSQL 持久化和本地素材存储；已有数据库和 `data/storage/` 会继续使用，不会在重启时清空。

本地配置文件为 `configs/config.yaml`（已被 git 忽略），本机 HTTP 使用非 Secure Cookie 以支持 `127.0.0.1`。在设置页添加真实 AI 厂商配置即可使用真实生成；没有密钥时可选择内置 Mock 厂商完成离线流程。需要重置本地账号时，应先备份数据库再执行数据库级操作，不要直接删除素材目录。

全新检出首次运行时，启动脚本会从 `configs/config.example.yaml` 创建本地配置并停止。请填写 `database.dsn`，本机 HTTP 使用时将 `auth.secure_cookies` 改为 `false`，再运行启动脚本。

启动脚本会将运行文件发布到 `~/.local/share/flyaimovie` 后交给 PM2 托管，避免 macOS 后台进程无法读取 `Documents`。运行日志和新生成的本地素材也保存在该目录。

## 制作流水线

工作台支持“自动制作”一次编排整集，也保留以下阶段的手动控制。自动制作使用持久化父流程记录各阶段和子任务，服务重启后可继续推进，并支持取消、失败原因查看和从当前阶段重试。确认启动前会展示本次使用的文本、图片、视频和音频服务；外部厂商可能产生费用。

1. **项目 / 集**：创建短剧项目，为每集绑定图片 / 视频 / 音频 AI 配置
2. **剧本**：粘贴大纲 → AI 改写为格式化剧本
3. **角色 / 场景**：提取去重、分配音色、生成形象与场景图
4. **宫格帧**：生成宫格提示词 → 出图 → 切分 → 按切片重新分配到镜头首帧、尾帧或分镜板
5. **分镜 / 视频**：拆镜、首尾帧、图生视频、配音、镜头合成
6. **导出**：批量合成镜头 → 拼接整集成片

## 内置能力

- Agent：`script_rewriter` / `extractor` / `storyboard_breaker` / `voice_assigner` / `grid_prompt_generator`
- 提示词：11 个内置/自定义模板（Agent、镜头图/视频、宫格、角色图、场景图、道具图）、搜索与分类、保存前草稿校验预览、不可变版本历史、旧版本恢复与 Agent 运行版本追踪；宫格、镜头图片/视频、角色/场景/道具形象与自动制作媒体阶段会自动套用组织模板，工作台也可手动套用
- 媒体：图片 / 视频异步轮询 + webhook（`/api/v1/webhooks/vidu`、`/api/v1/webhooks/generic`）；OpenAI Sora 支持创建、轮询和鉴权下载
- 宫格历史、切片持久分配与冲突替换、任务查询/取消、统一素材库 API、批量帧/视频/配音、道具管理、本地存储 `/static`
- 跨项目角色模板库、项目内场景复制/迁移、视频/音频上传与 FFprobe 元数据
- 组织音色库：MiniMax/Mock 增量同步、失效标记、搜索和自定义文本试听；工作台只分配可用音色
- 任务中心：阶段日志、筛选、单任务重试/取消与批量取消；Agent 运行历史、失败恢复及运行中工具调用/结果审计
- 组织隔离缓存：AI 请求、外部媒体、上传、生成、TTS、合成和任务结果按内容哈希去重；设置页可查看容量并清理过期项
- **Mock 厂商**（`provider=mock`）：无外网密钥时也可跑通演示链路

功能对等范围、自动验收状态和 clean-room 红线见 [`docs/FUNCTIONAL_PARITY.md`](docs/FUNCTIONAL_PARITY.md)。
商业发行前的许可证、FFmpeg 和生产上线门槛见 [`docs/COMMERCIAL_RELEASE.md`](docs/COMMERCIAL_RELEASE.md)。
厂商 adapter 的公开协议来源和契约测试见 [`docs/PROVIDER_CONTRACTS.md`](docs/PROVIDER_CONTRACTS.md)。

设置页可添加 OpenAI 兼容、OpenAI Sora、MiniMax、Volcengine、Vidu、Aliyun 等；演示环境已种子 `mock-text/image/video/audio`。Sora 的真实账号、模型权限和额度需要部署方自行 smoke test。

## 配置

- `configs/config.yaml`：端口默认 5679，启动脚本通过 `PORT=8088` 覆盖
- 数据库：本地正式配置使用 PostgreSQL（`flyaimovie` 数据库）；SQLite 文件 `data/flyaimovie.db` 保留作旧数据备份
- 素材：源码目录中的既有素材会复制到运行目录，服务新生成的素材保存在 `~/.local/share/flyaimovie/data/storage/`
- `server.rate_limit_per_minute`：所有 `/api` 路由按直连来源 IP 限流，默认每分钟 600 次；不信任 `X-Forwarded-For`
- `AI_CONFIG_ENCRYPTION_KEY`：生产环境必填；启用后新旧 AI 厂商密钥会以 AES-GCM 密文保存在数据库中
- 本地 Ollama 等 OpenAI-compatible 文本服务使用 `provider=openai_local`，并将主机名精确加入 `ai.allowed_private_base_url_hosts`；生产模式会拒绝非空白名单
- 密码恢复生产配置：`SMTP_HOST`、`SMTP_PORT`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`EMAIL_FROM`、`PASSWORD_RESET_URL_BASE`；默认支持 SMTP 587 STARTTLS 与 465 隐式 TLS。

本地 PostgreSQL 配置位于被 git 忽略的 `configs/config.yaml`，服务启动时会自动执行 GORM 迁移并补齐厂商、Agent、Mock 默认数据。切换数据库前请先备份数据；当前不会自动把旧 SQLite 业务数据复制到 PostgreSQL。

## 外部媒体本地化

迁移命令默认只扫描，不写数据库：

```bash
cd backend
go run ./cmd/migrate-media
go run ./cmd/migrate-media --apply --backup-confirmed
```

SQLite apply 前会生成 `.pre-media-migration` 备份；PostgreSQL 必须先由运维完成备份并显式传入 `--backup-confirmed`。迁移记录可重复执行，原外链保存在 `media_migrations` 中，只有本地副本校验并落盘后才更新业务 URL。

## 健康检查

```bash
curl http://127.0.0.1:8088/api/v1/health
```

## 端到端验收

以下 live 测试会创建并永久删除测试组织，只能用于空的 disposable 数据库：

```bash
E2E_DISPOSABLE=1 E2E_PASSWORD='12-128 位临时密码' ./scripts/e2e-live.sh
./scripts/dev-up.sh  # 重新播种 Mock 配置
cd frontend && E2E_DISPOSABLE=1 E2E_PASSWORD='12-128 位临时密码' npm run test:e2e:live
cd .. && ./scripts/dev-up.sh  # 恢复为空安装并重新播种
```
