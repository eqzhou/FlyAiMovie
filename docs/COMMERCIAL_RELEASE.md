# 商业发布检查清单

本文记录 FlyAiMovie 商业发布前必须完成的技术与许可检查。它不是法律意见。

## 代码来源边界

- 本仓库原创实现按 Apache-2.0 发布。
- 不得引入 `chatfire-AI/huobao-drama` 的源码、数据库脚本、提示词、Skills、CSS、截图、演示素材、品牌或独特文案。
- 上游 README 标示 CC BY-NC-SA 4.0；任何直接复制都可能触发非商业和相同方式共享限制。
- 对外使用 FlyAiMovie 自有名称、视觉和文案，只描述“同类短剧生产能力”，不暗示官方授权或兼容关系。

## 依赖与媒体工具

当前直接代码依赖以宽松许可证为主，但正式发行仍应从锁文件生成完整 SBOM 并保存审计结果：

```bash
cd backend && go list -m -json all
cd frontend && npm ls --package-lock-only --all --json > npm-dependency-tree.json
```

`npm ls` 只提供依赖树，不等于许可证结论；正式发行仍需使用已审核的许可证扫描工具生成 SPDX/CycloneDX SBOM，并将结果归档到发布制品中。

FFmpeg 是独立进程依赖，许可证取决于实际分发的构建参数。本机 `ffmpeg -L` 显示启用了 `--enable-gpl`、x264 和 x265，因此该二进制按 GPLv3 分发。若在安装包或容器中附带该构建，发布方必须满足相应的许可证、源代码提供和声明义务。可选择：

1. 保留 GPL 构建并完成合规分发；
2. 使用经过审计的 LGPL-only FFmpeg 构建，并禁用 GPL/nonfree 组件；
3. 不随产品分发 FFmpeg，由部署方提供，并在启动时验证版本和能力。

Docker 运行镜像当前通过 Debian 包安装 FFmpeg，发布镜像前必须记录该具体包版本、构建配置和许可证文本。

## AI 与内容权利

- 分别审阅文本、图片、视频和 TTS 厂商的商业使用、训练数据、输出归属、地域和内容审核条款。
- 不将用户 API Key 写入日志、响应、前端存储或示例配置；生产环境设置 `AI_CONFIG_ENCRYPTION_KEY`，启动时会事务迁移旧明文记录。
- 用户必须拥有小说、剧本、图片、声音、肖像、音乐、字体及输出内容的必要权利。
- 声音克隆、真人肖像和未成年人内容应增加明确授权、撤回和审计流程。

## 上线门槛

当前适合本地演示、客户 PoC 和受控 Beta；不应视为已完成的公网商业发行版。公开多用户收费服务上线前至少需要：

本地正式使用方面，PostgreSQL/SQLite 两种 Docker 配置、自动迁移、健康检查、FFmpeg/FFprobe 检查、角色模板、场景迁移、媒体元数据、任务日志和 Agent 审计已经可用。它们不等于商业发布验收完成。

生产进程设置 `APP_ENV=production` 后，服务会拒绝认证关闭、debug、非 Secure Cookie、缺少 webhook secret 或缺少 AI 密钥加密密钥的配置。

- 已实现登录、RBAC、角色管理、组织切换、自助改密、安全组织邀请和密码恢复核心/SMTP 投递流程，以及租户级数据与存储隔离；已有账号必须用当前密码接受邮箱绑定的一次性邀请，组织管理员不得修改全局账号密码；正式发布仍需使用真实 SMTP 账号完成 smoke test，并完成 MFA/SSO 策略；
- 已有按直连来源 IP 的全 API 固定窗口限流，以及组织级每日任务、活跃任务和金额预算配额；金额按任务类型/厂商进行保守预估并在任务成功后记录实际成本。公开服务仍需结合真实供应商账单、代理信任策略及滥用检测校准预算模型；
- 异步任务重试已使用持久化 `available_at` 做指数退避（5 秒起步、最大 5 分钟）；正式发布仍需结合厂商 SLA 和金额预算调优上限。
- AI provider 出站请求已统一使用连接级 DNS/IP 校验 Transport，并禁用环境代理绕过；正式发布仍需在实际网络、IPv6 和企业 DNS 环境中进行渗透 smoke test。
- 已实现组织级写操作审计日志、组织 JSON 导出、双重确认删除及本地媒体补偿重试；正式发布仍需定义审计/补偿记录留存周期、可恢复备份流程和隐私政策；
- Mock 全链路 E2E、厂商契约测试和关键包 80% 以上覆盖率；
- 当前核心 Go 全包覆盖率实测为 80.1%，Mock/契约测试和 Chromium 桌面/移动浏览器 E2E 已通过；仍没有真实 OpenAI/MiniMax/火山/Vidu/阿里账号 smoke test，不得把真实供应商链路标记为已完成。
- OpenAI/Sora 视频 Adapter 已依据官方 Videos API 完成创建、轮询、鉴权下载和本地首帧契约测试；尾帧与多参考输入会明确拒绝。真实账号 smoke test 通过前不得标记为生产可用。
- Docker CLI 在当前验收机不可用，Compose 冷启动、PostgreSQL 干净环境初始化和数据卷恢复仍须在发布 CI/候选机执行。
- 依赖漏洞扫描、SBOM、许可证归档和法务复核；
- 生产密钥管理、TLS、反向代理、监控和告警。
- `AI_CONFIG_ENCRYPTION_KEY` 或外部 Secret Manager 保护 AI 厂商密钥；旧明文配置需迁移并轮换。
- PM2 启动器会显式传递 `APP_ENV`、`CONFIG_PATH`、`WEBHOOK_SECRET` 和 `AI_CONFIG_ENCRYPTION_KEY`；正式切换前必须通过环境注入这些值并将 `FLYAIMOVIE_BIN` 指向经过验收的候选二进制。
- 密码恢复支持 SMTP STARTTLS（587）和隐式 TLS（465）；生产必须配置 `SMTP_HOST`、`SMTP_PORT`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`EMAIL_FROM`、`PASSWORD_RESET_URL_BASE`，并验证邮件模板不包含 API Key、密码或内部路径，且 token 仅通过 HTTPS 邮件链接传递。

## 发布证据

每个版本应归档：

- Git 提交和构建产物摘要；
- Go/npm 锁文件与 SBOM；
- `ffmpeg -version`、`ffmpeg -buildconf` 和许可证文本；
- 模型供应商条款版本与配置清单；
- 自动测试、安全扫描、E2E 和人工验收报告；
- 品牌、素材、声音和肖像授权记录。
