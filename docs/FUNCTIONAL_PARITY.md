# 功能对等与验收矩阵

本文定义 FlyAiMovie 的 clean-room 目标：保持用户可观察的短剧生产能力对等，同时让代码、接口内部设计、提示词、视觉、文案、品牌与素材保持独立表达。

## 规格来源

- 公开功能基线：[huobao-drama README（固定版本）](https://github.com/chatfire-AI/huobao-drama/blob/ad1cd7cd0127389ce8304aa9ebda3cfc8f406a6d/README.md)
- 许可参考：[CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/)
- 不使用上游源码、提示词、CSS、测试、数据库脚本、截图或演示素材作为实现输入。
- 厂商适配只依据各厂商官方 API 文档和自行构造的请求/响应样例。

## 对等口径

`已实现` 表示存在可运行路径，不等于已经达到生产质量。只有绑定自动化用例后，能力才可标记为 `已验收`。

### 2026-07-18 公开能力差异审计

审计基线仍为上游公开 HEAD `ad1cd7cd0127389ce8304aa9ebda3cfc8f406a6d`。逐项对照公开 README 中的角色管理、分镜制作、视频生成、资源管理、五类 Agent、多厂商配置和 Docker 部署后，用户可观察的公开功能均已有独立实现与自动化证据。本轮关闭了最后两个证据缺口：

- 分镜板此前只能由宫格或素材复用写入，现在支持单镜头/批量直接生成、预览和严格帧类型校验；
- 浏览器 Live E2E 此前只覆盖账号、设置、邀请和素材，现在会从页面实际完成剧本改写、角色场景提取、分镜拆解、首帧/分镜板/视频/TTS、镜头合成与整集导出。

仍存在的差异属于有意的 clean-room 技术实现差异或外部验收门槛：后端使用 Go + PostgreSQL/SQLite，不采用上游 TypeScript/Mastra/Drizzle；真实厂商、真实 SMTP、公开 HTTPS 和许可证归档仍需对应账号或发布环境，不能由 Mock/契约测试替代。

| 编号 | 用户能力 | 可观察结果 | 当前实现 | 自动验收 |
|---|---|---|---|---|
| P-01 | 创建短剧与多集 | 项目和指定集数持久化，可继续编辑 | 已验收 | `TestMockAgentAndGridWorkflow` |
| P-02 | 小说/大纲改写 | 生成结构化剧本并写回单集 | 已验收：Mock Agent 通过公开 HTTP 路由写回剧集脚本 | `TestMockAgentAndGridWorkflow` |
| P-03 | 提取角色与场景 | 去重后关联项目/单集 | 已验收：Mock Agent 通过公开 HTTP 路由写回并按项目关联 | `TestMockAgentAndGridWorkflow` |
| P-04 | 角色资产 | 上传/生成形象，批量生成，分配音色并试听 | 已验收 Mock 主路径：角色创建/剧集关联、Mock 形象生成、音色样本生成、素材库登记 | `TestMockAssetGenerationWorkflow`、上传/角色归属回归测试 |
| P-05 | 场景与道具资产 | 创建、编辑、生成和复用素材 | 已验收 Mock 主路径：场景/道具创建、Mock 形象生成、素材库登记和跨项目归属校验 | `TestMockAssetGenerationWorkflow`、`TestImageUploadBindsPropAndRegistersAsset`、素材归属回归测试 |
| P-06 | 分镜拆解与编辑 | 得到有序镜头，可增删改镜头字段 | 已验收：Mock Agent 写回有序分镜；工作台支持手工新增、字段编辑和删除，API 另有归属回归覆盖 | `TestMockAgentAndGridWorkflow`、router 资源归属测试、`frontend/tests/e2e/workbench.spec.ts` |
| P-07 | 宫格工作流 | 生成提示词和宫格图，切分并分配到镜头帧 | 已验收：Mock HTTP 流程覆盖提示词、出图、FFmpeg 切分和首帧写回 | `TestMockAgentAndGridWorkflow` |
| P-08 | 镜头帧生成 | 支持首帧、尾帧、分镜板和批量生成 | 已验收 Mock 主路径：三类帧均可在工作台直接生成并预览；非法帧类型返回 400；帧/视频/TTS 批量入口在执行前校验分镜集归属 | `TestMockPipelineEndToEnd`、`TestMockAgentAndGridWorkflow`、`TestStoryboardFrameGenerationValidatesAndPersistsComposedFrame`、`TestBatchGenerationRejectsStoryboardsFromAnotherEpisode`、Live browser E2E |
| P-09 | 图生视频 | 单镜头/批量提交，展示状态和结果 | 已验收 Mock 主路径：单镜头生成、异步任务轮询、结果写回和批量提交 | `TestMockPipelineEndToEnd`、`TestAsyncVideoPollingCompletesPersistentJob` |
| P-10 | TTS 配音 | 单镜头/批量生成并可试听 | 已验收 Mock 主路径：队列任务、音频落盘、分镜写回和试听 URL | `TestMockPipelineEndToEnd` |
| P-11 | 镜头合成 | 视频、音频、字幕合成为镜头成片 | 已验收：持久任务执行 FFmpeg，支持字幕、取消、租约和恢复 | Mock FFmpeg compose E2E；`TestComposeShotAndMergeEpisode`、`TestComposeAndMergeValidationAndCancellation` |
| P-12 | 整集导出 | 按分镜顺序拼接并写回单集视频 | 已验收 Mock 主路径：按分镜顺序合并、任务恢复和缺失镜头返回 409 | `TestMockPipelineEndToEnd`、`TestComposeShotAndMergeEpisode` |
| P-13 | 生成历史与进度 | 查询图片、视频、宫格和合成状态 | 已验收：图片、视频、TTS、镜头合成与整集导出统一进入任务系统 | jobs 并发、恢复、重试、claim；`TestAsyncImagePollingCompletesPersistentJob`、`TestAsyncVideoPollingCompletesPersistentJob` |
| P-14 | AI 服务设置 | 配置文本/图片/视频/音频供应商，密钥不回显 | 已验收 | `TestAIConfigResponsesNeverExposeAPIKey` |
| P-15 | Agent 配置与 Skills | 运行时读取独立编写的技能说明并覆盖 Agent 配置 | 已验收：模型、系统提示、温度、最大 token 与模型迭代限制生效；五类 Agent 离线流程写回数据库 | `TestChatWithMaxTokensForwardsLimit`、`TestOfflineFallbackAgentsPersistWorkflow` |
| P-16 | 本地离线演示 | 无外部密钥时通过 mock 走完整流程 | 已验收 | `TestMockPipelineEndToEnd` |
| P-17 | 统一素材库 | 按项目、集、镜头和类型浏览、复用与删除素材 | 已实现：专用项目素材页支持筛选、图片/视频/音频预览、收藏、编辑、删除及分镜帧复用 | `TestAssetLibraryWorkflow`；桌面与 390px 浏览器验收 |
| P-18 | 上传并绑定资产 | 从工作台上传角色、场景、道具与参考图并持久绑定 | 已实现：素材页可绑定项目、剧集、角色、场景、道具或分镜，拒绝多目标和跨项目关联 | `TestImageUploadBindsPropAndRegistersAsset`、`TestImageUploadRejectsMultipleBindingTargets` |
| P-19 | 厂商官方协议 | 每个声明支持的厂商通过官方 API 契约测试 | 部分验收：OpenAI 图片与 Sora 视频、Chatfire、Gemini、MiniMax、Volcengine、Vidu、Aliyun 已使用独立协议 adapter；Sora 尾帧/多参考输入会明确拒绝；真实账号 smoke test 待执行 | `official_image_test.go`、`official_video_test.go`、`minimax_tts_test.go` |
| P-20 | 资源归属一致性 | 跨项目/剧集资源不能被错误关联或修改 | 已实现：角色、场景、道具、分镜、生成、宫格、素材和 Agent 入口统一校验，分镜替换使用事务 | 跨项目资源、Agent、宫格与生成入口回归测试 |
| P-21 | 组织数据生命周期 | owner 可导出本组织数据，并通过密码与 slug 双重确认删除组织 | 已验收：导出按组织隔离且排除凭据；删除事务同时写入媒体补偿队列，文件失败可退避重试且服务启动会恢复，同时保留仍属于其他组织的共享用户 | `TestOrganizationExportIsScopedAndRedactsCredentials`、`TestOrganizationDeletionPurgesDataMediaAndSessionsButKeepsSharedUser`、`mediacleanup/service_test.go` |
| P-22 | 跨项目角色模板 | 模板独立增删改并复制到项目，项目修改不影响模板 | 已验收 API 主路径与组织隔离；角色库页面已接入 | `TestCharacterTemplateImportCreatesIndependentCharacter` |
| P-23 | 场景复制与迁移 | 复制到目标集；迁移时事务更新场景、剧集关联与可选关联分镜 | 已验收后端事务和前端操作；跨项目必须显式确认 | `TestSceneMoveAcrossDramaRequiresExplicitOptions`、`frontend/tests/e2e/workbench.spec.ts` |
| P-24 | 媒体元数据与迁移 | 上传视频/音频后读取真实时长；历史外链可 dry-run 和断点迁移 | 已实现：FFprobe 失败保存错误而非错误的 0；迁移仅在验证成功后替换 URL | `mediainfo/probe_test.go`、`mediamigrate/service_test.go` |
| P-25 | 任务日志与 Agent 历史 | 持久化阶段、失败、重试、工具调用和运行结果 | 已验收任务事件、批量取消、Agent 运行历史；Agent 取消在上下文/工具边界生效，服务重启遗留运行标记为中断失败 | jobs service/API tests、`TestMockAgentAndGridWorkflow`、`frontend/tests/e2e/jobs.spec.ts` |
| P-26 | 统一缓存生命周期 | AI 请求、外部媒体、上传、生成、TTS、合成和任务结果按组织隔离缓存，可去重、计数、过期和手动清理 | 已验收：图片/视频/音频上传及所有生成链路均接入；物理对象按组织/类型/内容哈希去重，逻辑引用独立计数；过期对象进入可重试删除补偿队列，设置页展示容量并提供管理员清理入口；组织导出/删除包含缓存记录 | `mediacache/service_test.go`、`TestPostgresCacheLifecycle`、`TestMediaCacheStatsAndPurgeWorkflow`、图片/媒体重复上传测试、`TestChatCachesIdenticalRequestsWithinOrganization`、live browser E2E |

## 非功能验收

| 编号 | 要求 | 当前状态 | 自动验收 |
|---|---|---|---|
| N-01 | API 密钥不出现在读取、创建或更新响应中 | 已验收 | `TestAIConfigResponsesNeverExposeAPIKey` |
| N-02 | 非法分页参数不会导致 panic 或无界查询 | 已验收 | `TestDramaPaginationClampsInvalidPageSize` |
| N-03 | 未列入配置的跨域来源不获得 CORS 授权 | 已验收 | `TestCORSRejectsUntrustedOrigin` |
| N-04 | 写入操作具备 schema 校验、事务和明确错误码 | 已验收主要更新路径：所有 JSON 解析错误均返回 400；短剧、剧集、角色、场景、道具、分镜、素材、AI 与 Agent 配置更新均拒绝空对象、混合未知字段、错误类型和越界值 | `write_contracts_test.go`、`write_validation.go`、jobs/assets/AI config 测试 |
| N-05 | 异步任务可幂等、重试、取消并在重启后恢复 | 已验收主路径：所有生成/合成任务具备状态、取消、幂等创建、重试、租约 claim、owner fencing 和恢复；失败/取消重试采用 5 秒起步的指数退避，最大 5 分钟 | jobs 并发、状态、恢复、重试、claim 测试；`TestRetryBackoffIsBoundedAndExponential` |
| N-06 | 上传和远程下载限制类型、尺寸、大小并防 SSRF | 已验收主路径：上传/远程媒体下载已有 SSRF 防护；公共 AI 配置拒绝回环、私网、链路本地 IP、元数据主机、用户信息、查询和片段；仅开发环境可为 `openai_local` 精确放行本地文本主机，生产模式拒绝该白名单；所有主要 AI adapter 通过连接级 DNS/IP 校验 Transport，禁止代理绕过 | `mediafetch`、`storage` 单元测试；AI 配置私网白名单与生产配置测试；`TestUnsafeProviderIPRejectsPrivateAndSpecialRanges` |
| N-07 | Webhook 校验签名、时间戳并防重放 | 已验收 | `TestWebhookRequiresValidSignatureAndRejectsReplay` |
| N-08 | 核心 Go 包测试覆盖率不少于 80% | 已验收：2026-07-18 全包实测 80.1%；`go test ./... -race -count=1`、`go vet ./...` 通过 | `go test ./... -coverprofile=...` |
| N-09 | 桌面和移动端关键流程无阻塞性交互问题 | 已验收 Chromium：Mock 桌面/390px 移动流程覆盖登录、邀请、设置、素材、工作台、场景迁移和任务中心；PostgreSQL Live Chrome 从页面完成剧本到整集成片以及账号管理；Safari/WebKit 仍需在发布 CI 补测 | `frontend/tests/e2e/*.spec.ts`、`frontend/tests/live/local-system.spec.ts` |
| N-10 | 所有 API 具备来源隔离的有界限流 | 已验收：覆盖 health、Webhook 与 CORS 预检，不信任转发头 | `rate_limit_test.go` |
| N-11 | 关键写操作具备组织级审计追踪 | 已验收：记录成员、动作、资源、状态和来源 IP，不保存请求体；仅 owner/admin 可按组织查询 | `audit_test.go` |
| N-12 | 生成任务具备租户配额与并发成本保护 | 已验收：统一 Job 入口原子检查每日任务、活跃任务和每日金额预算；按厂商/任务类型预留估算成本，成功任务记录实际成本，幂等请求不重复计额，超限返回 429，并暴露预算使用量和预警状态 | `TestOrganizationQuotaLimitsActiveAndDailyJobs`、`TestCostEstimationAndBudgetReservation`、`TestOrganizationQuotaIsScopedAndAdminManaged` |
| N-13 | 多用户组织具备成员管理与组织切换 | 已验收主路径：owner/admin 角色边界、owner 保护、移除成员会话撤销、切换组织旋转 Session；已有账号必须通过邮箱绑定的一次性邀请接受，新账号在接受时设置密码；邀请支持状态查询、撤销和重发，旧 token 立即失效 | `TestMemberManagementAndOrganizationSwitch`、`TestAdminCannotManageOwnerOrGrantAdmin`、`TestOrganizationInvitationAcceptsNewUserOnce`、`TestOrganizationInvitationRequiresExistingUserPassword`、`TestOrganizationInvitationExpires`、`TestOrganizationInvitationCanBeRevokedAndResent` |
| N-14 | 密码变更具备身份校验与会话撤销 | 已验收：校验当前密码、旋转自助会话、旧密码和旧 Session 失效；组织管理员不得修改成员的全局账号凭据 | `TestChangePasswordRotatesSessionsAndInvalidatesOldPassword`、`TestOrganizationAdminCannotResetGlobalMemberPassword` |
| N-16 | 忘记密码具备安全恢复流程 | 已验收核心及 SMTP 投递实现：请求响应不枚举账号；SMTP 支持 587 STARTTLS/465 隐式 TLS；token 只存哈希、30 分钟过期、一次性消费；成功后更新密码并撤销所有 Session；主动改密和组织删除会清理未消费 token。真实 SMTP 账号 smoke test 待部署 | `TestPasswordResetRequestDoesNotEnumerateAccounts`、`TestPasswordResetConsumesTokenAndRevokesSessions`、`TestPasswordResetExpiredTokenRejected`、`TestSMTPPasswordResetSenderRequiresHTTPSAndCredentials` |
| N-15 | 敏感组织操作具备 owner 权限与二次确认 | 已验收：组织导出仅 owner 可用；组织删除要求当前密码及组织 slug，在事务内撤销会话、删除租户数据并写入媒体补偿任务；失败任务可审计和重试 | 组织导出、删除与媒体补偿测试 |

## 独立表达红线

1. 不使用“火宝/Huobao”及其 logo、域名风格、宣传语或演示素材。
2. 不逐文件翻译上游实现，不复制其提示词、Skills、测试、CSS、数据库定义或独特文案。
3. UI 以制作效率和本项目信息架构为依据独立设计，不以截图像素相似为目标。
4. 对外宣传使用“同类功能”或“功能对等”，不暗示官方、授权或兼容关系。
5. 商业发布前单独审计依赖、FFmpeg 构建、字体、图标、模型服务条款、声音和肖像授权。

## 下一阶段完成标准

- 使用真实 OpenAI、MiniMax、火山、Vidu、阿里账号执行 smoke test，并归档供应商协议版本与测试证据。
- 使用真实 SMTP 账号验收组织邀请和密码恢复投递。
- 在发布 CI 补跑 WebKit，并在干净 Docker/PostgreSQL 环境验证初始化、数据卷恢复和数据库备份回滚。
- 生成 SBOM、执行依赖漏洞与许可证扫描，并完成 FFmpeg 构建、字体、图标、声音、肖像和模型条款的法务归档。
