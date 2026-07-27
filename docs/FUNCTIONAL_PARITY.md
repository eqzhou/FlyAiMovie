# 功能对等与验收矩阵

本文定义 FlyAiMovie 的 clean-room 目标：保持用户可观察的短剧生产能力对等，同时让代码、接口内部设计、提示词、视觉、文案、品牌与素材保持独立表达。

## 规格来源

- 正式发布基线：[huobao-drama v1.0.4](https://github.com/chatfire-AI/huobao-drama/releases/tag/v1.0.4)。截至 2026-07-19，它仍是最新 GitHub Release。
- 开发版体验基线：[huobao-drama README（固定提交）](https://github.com/chatfire-AI/huobao-drama/blob/ad1cd7cd0127389ce8304aa9ebda3cfc8f406a6d/README.md)。该 README 的更新日志另行声明 `v2.0.0 (2026-04)`，包含紧凑单集工作台、重做分镜编辑以及配音、镜头图、视频、合成、导出连续流程；它不等同于已经发布的 GitHub Release。
- 许可参考：[CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/)
- 不使用上游源码、提示词、CSS、测试、数据库脚本、截图或演示素材作为实现输入。
- 厂商适配只依据各厂商官方 API 文档和自行构造的请求/响应样例。

## 对等口径

`已实现` 表示存在可运行路径，不等于已经达到生产质量。只有绑定自动化用例后，能力才可标记为 `已验收`。

### 2026-07-19 公开能力差异审计

审计分别采用最新正式 Release `v1.0.4` 与公开 HEAD `ad1cd7cd0127389ce8304aa9ebda3cfc8f406a6d`。逐项对照公开 README 中的角色管理、分镜制作、视频生成、资源管理、五类 Agent、多厂商配置和 Docker 部署后，用户可观察的公开功能均已有独立实现与自动化证据。本轮继续补齐了自动编排与前端制作体验：

- 分镜板此前只能由宫格或素材复用写入，现在支持单镜头/批量直接生成、预览和严格帧类型校验；
- 浏览器 Live E2E 此前只覆盖账号、设置、邀请和素材，现在会从页面实际完成剧本改写、角色场景提取、分镜拆解、首帧/分镜板/视频/TTS、镜头合成与整集导出。
- 单集“自动制作”现在由持久化父状态机驱动，复用既有 Agent、图片、视频、TTS 和 FFmpeg 子任务，支持重启恢复、幂等推进、取消、失败原因和从失败阶段重试；启动前会显示实际服务配置和外部服务费用提示。
- 工作台阶段写入 URL 并可刷新恢复；分镜区改为紧凑镜头列表与当前镜头检查器，新增角色、场景和分镜使用独立弹窗，viewer 只保留查看能力，局部资源失败不会清空已加载页面。
- 宫格切分结果现在持久记录每个切片的来源与目标槽位，可在首帧、尾帧和分镜板之间重新分配；同一目标槽位只保留一个切片，移动切片时仅条件清理其旧值。缺少安全来源记录的旧历史保持可查看，并明确引导重新生成。
- 设置与角色库采用顶部分类导航和列表主体；新增、编辑、导入集中在弹窗，AI 服务可在保存前测试当前模型、Base URL 与密钥组合，并禁止把旧密钥复用到变更后的端点。
- 提示词设置支持搜索、分类和状态过滤；新建或编辑模板可在保存前由服务端按变量白名单检查并渲染草稿，不产生模板或版本记录；镜头帧、镜头视频、宫格构图、角色/场景/道具形象与自动制作媒体阶段会自动套用组织模板，内容为空时不会用纯模板兜底。

2026-07-25 产品入口收口：项目详情页补齐角色/场景 CRUD、生成形象/场景图、角色试听与存入角色库、场景复制/迁移，以及剧集标题与 AI 服务绑定编辑；首页项目元数据编辑与道具编辑删除此前已上主干。用户可观察的管理入口已与工作台/API 对齐。

2026-07-26 字段级入口收口：本轮审计从「后端字段可写、前端不可见」的角度复查资源模型，补齐四处真实缺口。分镜此前只能通过三个单字段小窗改图片提示词、视频提示词和对白，`location`、`time`、`angle`、`movement`、`action`、`result`、`atmosphere`、`bgm_prompt`、`sound_effect` 九个字段用户既看不到也改不了；现在工作台提供完整镜头编辑弹窗并在检查器同步展示。道具的 `type` 分类字段同样未暴露，现已可编辑并在列表显示。角色与道具的 `reference_images` 之前即使写入数据库也不会进入生成请求，参考图形同虚设；现在创建、更新与单张/批量形象生成全链路透传，并保持原有组织归属校验。镜头出场角色（`character_ids`）此前只能由 Agent 拆解时写入，UI 无法调整；现已接入编辑弹窗，绑定关系仍只存于后端 `storyboard_characters` 表，前端不保留关系副本。

关于状态归属：镜头与角色、场景的关联均以后端表为唯一事实来源，前端表单只在弹窗打开时读取服务端返回值、保存时整组提交，不做本地缓存或增量合并。场景与角色选择器只加载本集数据，而后端按项目校验，因此对跨集绑定补了只读占位项，避免保存时静默丢失既有关系。

仍存在的差异属于有意的 clean-room 技术实现差异或外部验收门槛：后端使用 Go + PostgreSQL/SQLite，不采用上游 TypeScript/Mastra/Drizzle；真实厂商、真实 SMTP、公开 HTTPS 和许可证归档仍需对应账号或发布环境，不能由 Mock/契约测试替代。

| 编号 | 用户能力 | 可观察结果 | 当前实现 | 自动验收 |
|---|---|---|---|---|
| P-01 | 创建短剧与多集 | 项目和指定集数持久化，可继续编辑 | 已验收 | `TestMockAgentAndGridWorkflow` |
| P-02 | 小说/大纲改写 | 生成结构化剧本并写回单集 | 已验收：Mock Agent 通过公开 HTTP 路由写回剧集脚本 | `TestMockAgentAndGridWorkflow` |
| P-03 | 提取角色与场景 | 去重后关联项目/单集 | 已验收：Mock Agent 通过公开 HTTP 路由写回并按项目关联 | `TestMockAgentAndGridWorkflow` |
| P-04 | 角色资产 | 上传/生成形象，批量生成，分配音色并试听 | 已验收 Mock 主路径：角色创建/剧集关联、Mock 形象生成、组织级音色分配和样本生成、素材库登记；角色试听拒绝跨项目剧集且只使用本组织音频配置。项目资产页与工作台均支持新增/编辑/删除、生成形象、试听与存入角色库；参考图可在项目资产页维护，创建与更新均校验组织归属，并在单张与批量形象生成时透传给厂商以保持角色一致性 | `TestMockAssetGenerationWorkflow`、`TestCharacterAndPropImageGenerationForwardsReferenceImages`、`TestCharacterVoiceSampleUsesOrganizationConfigAndEpisode`、上传/角色归属回归测试、`frontend/tests/e2e/account-settings-assets.spec.ts` |
| P-05 | 场景与道具资产 | 创建、编辑、生成和复用素材 | 已验收 Mock 主路径：场景/道具创建、Mock 形象生成、素材库登记和跨项目归属校验。项目资产页支持角色/场景/道具完整管理，场景可复制/迁移到剧集，道具支持编辑/删除/生成图、类型分类与参考图维护，道具参考图在创建、更新时校验归属并透传到生成 | `TestMockAssetGenerationWorkflow`、`TestCharacterAndPropImageGenerationForwardsReferenceImages`、`TestImageUploadBindsPropAndRegistersAsset`、素材归属回归测试、`frontend/tests/e2e/account-settings-assets.spec.ts` |
| P-06 | 分镜拆解与编辑 | 得到有序镜头，可增删改镜头字段 | 已验收：Mock Agent 写回有序分镜；工作台提供完整镜头编辑弹窗，覆盖标题、时长、所属场景、出场角色、景别/机位/运镜、地点/时间/氛围、动作/结果/对白/描述、图片与视频提示词、背景音乐与音效提示词及参考图，镜头检查器同步展示既有内容。镜头与角色的绑定只存于后端 `storyboard_characters`，创建、更新与列表接口完整往返，空数组显式解绑，前端不另存关系副本 | `TestMockAgentAndGridWorkflow`、`TestStoryboardCharacterBindingsRoundTripThroughAPI`、router 资源归属测试、`frontend/tests/e2e/workbench.spec.ts` |
| P-07 | 宫格工作流 | 生成提示词和宫格图，切分并分配到镜头帧 | 已验收：Mock HTTP 流程覆盖提示词、出图、FFmpeg 切分和首帧写回；切片与目标槽位持久化，可重新分配到首帧、尾帧或分镜板并原子处理槽位冲突；首尾模式在付费生成前校验宫格容量和镜头对；旧历史只读并提供安全重生成路径 | `TestMockAgentAndGridWorkflow`、`TestGridCellCanBeReassignedAndAssignmentPersists`、`TestGridRequestsRejectUnsafeStoryboardCountsBeforeGeneration`、工作台 E2E |
| P-08 | 镜头帧生成 | 支持首帧、尾帧、分镜板和批量生成 | 已验收 Mock 主路径：三类帧均可在工作台直接生成并预览；非法帧类型返回 400；帧/视频/TTS 批量入口在执行前校验分镜集归属 | `TestMockPipelineEndToEnd`、`TestMockAgentAndGridWorkflow`、`TestStoryboardFrameGenerationValidatesAndPersistsComposedFrame`、`TestBatchGenerationRejectsStoryboardsFromAnotherEpisode`、Live browser E2E |
| P-09 | 图生视频 | 单镜头/批量提交，展示状态和结果 | 已验收 Mock 主路径：单镜头生成、异步任务轮询、结果写回和批量提交 | `TestMockPipelineEndToEnd`、`TestAsyncVideoPollingCompletesPersistentJob` |
| P-10 | TTS 配音 | 单镜头/批量生成并可试听 | 已验收 Mock 主路径：队列任务、音频落盘、分镜写回和试听 URL；音色库支持 MiniMax/Mock 增量同步、失效标记、默认隐藏失效音色、搜索和自定义文本试听，同厂商配置和组织边界均强制校验 | `TestMockPipelineEndToEnd`、`TestVoiceCatalogSyncAndPreview`、设置页 E2E |
| P-11 | 镜头合成 | 视频、音频、字幕合成为镜头成片 | 已验收：持久任务执行 FFmpeg，支持字幕、取消、租约和恢复 | Mock FFmpeg compose E2E；`TestComposeShotAndMergeEpisode`、`TestComposeAndMergeValidationAndCancellation` |
| P-12 | 整集导出 | 按分镜顺序拼接并写回单集视频 | 已验收 Mock 主路径：按分镜顺序合并、任务恢复和缺失镜头返回 409 | `TestMockPipelineEndToEnd`、`TestComposeShotAndMergeEpisode` |
| P-13 | 生成历史与进度 | 查询图片、视频、宫格和合成状态 | 已验收：图片、视频、TTS、镜头合成与整集导出统一进入任务系统 | jobs 并发、恢复、重试、claim；`TestAsyncImagePollingCompletesPersistentJob`、`TestAsyncVideoPollingCompletesPersistentJob` |
| P-14 | AI 服务设置 | 配置文本/图片/视频/音频供应商，密钥不回显；支持编辑模型、保存前测试当前配置、已保存配置连接测试、启停和组织内原子切换默认服务 | 已验收：草稿测试不落库、不返回密钥；仅在厂商、类型和 Base URL 均未改变时复用已保存密钥；远程厂商强制 HTTPS | `TestAIConfigResponsesNeverExposeAPIKey`、`TestAIConfigDraftConnectionTest*`、`TestAIConfigConnectionTestRejectsLegacyInsecureBaseURL`、`TestAIConfigDefaultIsExclusiveAndMustBeActive`、`frontend/tests/e2e/account-settings-assets.spec.ts` |
| P-15 | Agent 配置与 Skills | 运行时读取独立编写的技能说明并覆盖 Agent 配置 | 已验收：模型、温度、最大 token 与模型迭代限制生效；五类 Agent 离线流程写回数据库 | `TestChatWithMaxTokensForwardsLimit`、`TestOfflineFallbackAgentsPersistWorkflow` |
| P-16 | 本地离线演示 | 无外部密钥时通过 mock 走完整流程 | 已验收 | `TestMockPipelineEndToEnd` |
| P-17 | 统一素材库 | 按项目、集、镜头和类型浏览、复用与删除素材 | 已验收：专用项目素材页支持筛选、图片/视频/音频预览、收藏、编辑、删除及分镜帧复用 | `TestAssetLibraryWorkflow`；桌面与 390px 浏览器验收 |
| P-18 | 上传并绑定资产 | 从工作台上传角色、场景、道具与参考图并持久绑定 | 已验收：素材页可绑定项目、剧集、角色、场景、道具或分镜，拒绝多目标和跨项目关联 | `TestImageUploadBindsPropAndRegistersAsset`、`TestImageUploadRejectsMultipleBindingTargets` |
| P-19 | 厂商官方协议 | 每个声明支持的厂商通过官方 API 契约测试 | 部分验收：OpenAI 图片与 Sora 视频、Chatfire、Gemini、MiniMax、Volcengine、Vidu、Aliyun 已使用独立协议 adapter；Sora 尾帧/多参考输入会明确拒绝；真实账号 smoke test 待执行 | `official_image_test.go`、`official_video_test.go`、`minimax_tts_test.go` |
| P-20 | 提示词模板 | 组织内管理、版本化、预览并应用 Agent、镜头图片、镜头视频和宫格提示词 | 已验收：11 个独立内置模板（Agent/镜头图/镜头视频/宫格/角色图/场景图/道具图），白名单变量插值、未知表达式拒绝、组织隔离、旧库自动补齐当前快照；创建、编辑和恢复均事务写入不可变版本，任一旧版可恢复为新版本；设置页提供搜索/分类/状态过滤、完整变量面板、按光标插入、未保存草稿的服务端校验预览、模板复制、内置恢复和只读历史，工作台可确认写回；草稿预览不写模板或版本；Agent 运行事件持久记录实际解析的模板 ID、key、版本和来源；镜头首帧/尾帧/分镜板、镜头视频、宫格构图、角色/场景/道具形象与自动制作媒体阶段均服务端自动套用组织模板，资产或镜头内容为空时拒绝用纯模板兜底生成 | `prompt_templates_test.go`、`db_test.go`、`template_test.go`、`apply_test.go`、`TestRunnerUsesOrganizationPromptTemplateAndVersion`、`TestStoryboardFrameUsesOrganizationPromptTemplate`、production service tests、设置页与工作台桌面/移动 E2E |
| P-21 | 资源归属一致性 | 跨项目/剧集资源不能被错误关联或修改 | 已验收：角色、场景、道具、分镜、生成、宫格、素材和 Agent 入口统一校验；宫格切片同时校验组织、历史、素材类别和服务端写入的来源标记，分镜替换使用事务 | 跨项目资源、Agent、宫格与生成入口回归测试；`TestGridSplitRejectsMediaOwnedByAnotherOrganization`、`TestMutableMediaReferencesCannotClaimUnownedLocalPaths` |
| P-22 | 组织数据生命周期 | owner 可导出本组织数据，并通过密码与 slug 双重确认删除组织 | 已验收：导出按组织隔离且排除凭据；删除事务同时写入媒体补偿队列，文件失败可退避重试且服务启动会恢复，同时保留仍属于其他组织的共享用户 | `TestOrganizationExportIsScopedAndRedactsCredentials`、`TestOrganizationDeletionPurgesDataMediaAndSessionsButKeepsSharedUser`、`mediacleanup/service_test.go` |
| P-23 | 跨项目角色模板 | 模板独立增删改并复制到项目，项目修改不影响模板 | 已验收 API 主路径与组织隔离；角色库支持列表搜索、弹窗新建/编辑、导入和删除 | `TestCharacterTemplateImportCreatesIndependentCharacter`、`frontend/tests/e2e/account-settings-assets.spec.ts` |
| P-24 | 场景复制与迁移 | 复制到目标集；迁移时事务更新场景、剧集关联与可选关联分镜 | 已验收后端事务和前端操作；跨项目必须显式确认 | `TestSceneMoveAcrossDramaRequiresExplicitOptions`、`frontend/tests/e2e/workbench.spec.ts` |
| P-25 | 媒体元数据与迁移 | 上传视频/音频后读取真实时长；历史外链可 dry-run 和断点迁移 | 已验收：FFprobe 失败保存错误而非错误的 0；迁移仅在验证成功后替换 URL | `mediainfo/probe_test.go`、`mediamigrate/service_test.go` |
| P-26 | 任务日志与 Agent 历史 | 持久化阶段、失败、重试、工具调用和运行结果 | 已验收任务事件、批量取消、Agent 运行历史；手动、重试和自动制作 Agent 均按 `started → tool_call → tool_result → terminal` 持久化有序事件，运行中详情自动刷新；任务中心支持筛选、中文状态、输入/结构化输出、审计详情、取消和失败/取消重试，只读成员仅可查看；重试创建带来源关系的独立异步运行并重新校验剧集归属，结构化失败不会误记成功；服务重启遗留运行标记为中断失败 | jobs service/API tests、`agent_runs_retry_test.go`、production service tests、`TestMockAgentAndGridWorkflow`、`frontend/tests/e2e/jobs.spec.ts` |
| P-27 | 统一缓存生命周期 | AI 请求、外部媒体、上传、生成、TTS、合成和任务结果按组织隔离缓存，可去重、计数、过期和手动清理 | 已验收：图片/视频/音频上传及所有生成链路均接入；物理对象按组织/类型/内容哈希去重，逻辑引用独立计数；过期对象进入可重试删除补偿队列，设置页展示容量并提供管理员清理入口；组织导出/删除包含缓存记录 | `mediacache/service_test.go`、`TestPostgresCacheLifecycle`、`TestMediaCacheStatsAndPurgeWorkflow`、图片/媒体重复上传测试、`TestChatCachesIdenticalRequestsWithinOrganization`、live browser E2E |
| P-28 | 单集自动制作 | 一次启动后按剧本、提取、分镜、首帧、视频、配音、合成和导出持续推进，可取消、恢复和重试 | 已验收 Mock 主路径：`ProductionRun` 持久化阶段、续租心跳和父子任务关系；媒体 Job 创建时立即绑定父流程，生成记录/素材写回与 Job 终态在同一条件事务内提交，取消与晚到结果不会同时成功；长阶段不会被其他 Worker 重复领取，服务重启后可继续领取；首帧、视频和 TTS 严格使用任务创建时绑定的配置；活跃子任务不会重复创建；取消会传递到正在运行的 Agent/媒体 Worker、同步媒体状态且不会触发离线回退写入；Worker 停止会释放正在执行的流程租约；旧失败流程不得与新流程并行重试；组织导出/删除包含流程记录 | `internal/services/production/service_test.go`、`generation/async_test.go`、`jobs/service_test.go`、`productions_test.go`、`frontend/tests/e2e/workbench.spec.ts` |

## 非功能验收

| 编号 | 要求 | 当前状态 | 自动验收 |
|---|---|---|---|
| N-01 | API 密钥不出现在读取、创建或更新响应中 | 已验收 | `TestAIConfigResponsesNeverExposeAPIKey` |
| N-02 | 非法分页参数不会导致 panic 或无界查询 | 已验收 | `TestDramaPaginationClampsInvalidPageSize` |
| N-03 | 未列入配置的跨域来源不获得 CORS 授权 | 已验收 | `TestCORSRejectsUntrustedOrigin` |
| N-04 | 写入操作具备 schema 校验、事务和明确错误码 | 已验收主要更新路径：所有 JSON 解析错误均返回 400；短剧、剧集、角色、场景、道具、分镜、素材、AI 与 Agent 配置更新均拒绝空对象、混合未知字段、错误类型和越界值；宫格提示词/生成/切分请求限制为 128 KiB，生成与切分只接受单一 JSON 值，镜头目标最多 25 个且单格提示词最多 4000 字符，数量校验先于归属查询与厂商调用 | `write_contracts_test.go`、`write_validation.go`、`TestGridRequestsRejectUnsafeStoryboardCountsBeforeGeneration`、jobs/assets/AI config 测试 |
| N-05 | 异步任务可幂等、重试、取消并在重启后恢复 | 已验收主路径：所有生成/合成任务具备状态、取消、幂等创建、重试、租约 claim、owner fencing 和恢复；失败/取消重试采用 5 秒起步的指数退避，最大 5 分钟 | jobs 并发、状态、恢复、重试、claim 测试；`TestRetryBackoffIsBoundedAndExponential` |
| N-06 | 上传和远程下载限制类型、尺寸、大小并防 SSRF | 已验收主路径：上传/远程媒体下载已有 SSRF 防护；公共 AI 配置拒绝回环、私网、链路本地、运营商级 NAT、基准测试、文档保留地址和云元数据主机，远程厂商必须使用 HTTPS；仅开发环境可为 `openai_local` 精确放行本地文本主机和 HTTP，生产模式拒绝该白名单；已保存的历史配置在测试连接和实际任务加载时都会重新校验；所有主要 AI adapter 通过连接级 DNS/IP 校验 Transport、禁用重定向和代理绕过。私有 HTTPS 端点必须同时提供可解析的 `AI_PROVIDER_CA_FILE` 与精确 `AI_PROVIDER_PRIVATE_HOSTS` 白名单，不关闭 TLS 校验，域名解析到私网 IP 时也按该契约放行；带 Bearer 的媒体下载拒绝跨 authority 重定向；厂商响应设有大小上限，图像、视频、TTS 和文本厂商错误均不持久化原始响应体或查询串密钥；本地宫格源图和切片来源需通过组织归属校验，不能用任意 `/static` 路径自声明归属 | `netguard`、`mediafetch`、`storage` 单元测试；`TestProviderHTTPClientRejectsReservedLiteralAddress`、`TestPrivateProviderHostnameRequiresExactAllowlistAndCustomCA`、`TestDownloadAuthorizedRejectsCrossAuthorityRedirect`、`TestGeminiImageNetworkErrorDoesNotExposeAPIKey`、`TestChatProviderErrorDoesNotExposeResponseBodyOrAPIKey`、`TestOpenAICompatImage*`、`TestPrivateProviderRequiresValidCAAndExactHostAllowlist`、`TestTaskConfigRejectsLegacyInsecureRemoteURL`、宫格归属测试 |
| N-07 | Webhook 校验签名、时间戳并防重放 | 已验收 | `TestWebhookRequiresValidSignatureAndRejectsReplay` |
| N-08 | 核心 Go 包测试覆盖率不少于 80% | 已验收：2026-07-19 全包精确实测 80.1248%（工具显示 80.1%），HTTP API 80.4%；`go test ./... -race -count=1`、`go vet ./...` 通过 | `go test ./... -race -coverprofile=coverage.out -count=1` |
| N-09 | 桌面和移动端关键流程无阻塞性交互问题 | 已验收 Chromium：43 条 Mock E2E 覆盖登录、邀请、设置、模型草稿测试、提示词草稿检查/过滤/竞态保护、角色库、素材、URL 阶段恢复、紧凑分镜编辑、宫格切片重分配、旧历史恢复、首尾生成前校验、自动制作确认、局部失败重试、AI 配置加载失败提示、viewer 项目/工作台/任务中心只读；桌面与 390px 移动端菜单均做真实命中检查；PostgreSQL Live 浏览器验证工作台与模型编辑弹窗，WebKit 已纳入 Playwright 项目与 CI（desktop grep 同跑）；真实设备 Safari 手测仍建议发布前完成 | `frontend/tests/e2e/*.spec.ts`、`frontend/tests/live/local-system.spec.ts` |
| N-10 | 所有 API 具备来源隔离的有界限流 | 已验收：覆盖 health、Webhook 与 CORS 预检，不信任转发头 | `rate_limit_test.go` |
| N-11 | 关键写操作具备组织级审计追踪 | 已验收：记录成员、动作、资源、状态和来源 IP，不保存请求体；仅 owner/admin 可按组织查询 | `audit_test.go` |
| N-12 | 生成任务具备租户配额与并发成本保护 | 已验收：统一 Job 入口原子检查每日任务、活跃任务和每日金额预算；按厂商/任务类型预留估算成本，成功任务记录实际成本，幂等请求不重复计额，超限返回 429，并暴露预算使用量和预警状态 | `TestOrganizationQuotaLimitsActiveAndDailyJobs`、`TestCostEstimationAndBudgetReservation`、`TestOrganizationQuotaIsScopedAndAdminManaged` |
| N-13 | 多用户组织具备成员管理与组织切换 | 已验收主路径：owner/admin 角色边界、owner 保护、移除成员会话撤销、切换组织旋转 Session；已有账号必须通过邮箱绑定的一次性邀请接受，新账号在接受时设置密码；邀请支持状态查询、撤销和重发，旧 token 立即失效 | `TestMemberManagementAndOrganizationSwitch`、`TestAdminCannotManageOwnerOrGrantAdmin`、`TestOrganizationInvitationAcceptsNewUserOnce`、`TestOrganizationInvitationRequiresExistingUserPassword`、`TestOrganizationInvitationExpires`、`TestOrganizationInvitationCanBeRevokedAndResent` |
| N-14 | 密码变更具备身份校验与会话撤销 | 已验收：校验当前密码、旋转自助会话、旧密码和旧 Session 失效；组织管理员不得修改成员的全局账号凭据 | `TestChangePasswordRotatesSessionsAndInvalidatesOldPassword`、`TestOrganizationAdminCannotResetGlobalMemberPassword` |
| N-16 | 忘记密码具备安全恢复流程 | 已验收核心及 SMTP 投递实现：请求响应不枚举账号；SMTP 支持 587 STARTTLS/465 隐式 TLS；token 只存哈希、30 分钟过期、一次性消费；成功后更新密码并撤销所有 Session；主动改密和组织删除会清理未消费 token。本地 SMTP 契约与邀请邮件投递已测；真实 SMTP 账号 smoke 通过 `TestLiveSmokeSMTPPasswordResetAndInvitation`（需 SMOKE_SMTP_*） | `TestPasswordResetRequestDoesNotEnumerateAccounts`、`TestPasswordResetConsumesTokenAndRevokesSessions`、`TestPasswordResetExpiredTokenRejected`、`TestSMTPPasswordResetSenderRequiresHTTPSAndCredentials` |
| N-15 | 敏感组织操作具备 owner 权限与二次确认 | 已验收：组织导出仅 owner 可用；组织删除要求当前密码及组织 slug，在事务内撤销会话、删除租户数据并写入媒体补偿任务；失败任务可审计和重试 | 组织导出、删除与媒体补偿测试 |
| N-17 | 公开自助注册与平台注册设置 | 已验收：setup 后开放 `POST /auth/register` 与注册 UI；注册创建 user/org/owner 且不认领 legacy 资源；默认开放注册、不强制邮箱校验；`require_email_verification` 第一期仅门禁（未验证不可登录、注册不发邮件）；仅 `is_platform_admin` 可读写平台设置；修改密码入口在设置 → 安全与数据 | `TestAuthRegisterCreatesOwnerWorkspaceAndSession`、`TestAuthRegisterRejectedWhenDisabledOrDuplicate`、`TestAuthRegisterRequiresCompletedSetup`、`TestAuthRegisterVerificationRequiredSkipsSession`、`TestPlatformSettingsOnlyPlatformAdmin`、`TestEmailVerificationGateOnRegisterAndLogin`、`TestChangePasswordRotatesSessionsAndInvalidatesOldPassword`、`frontend/tests/e2e/auth-register.spec.ts`、`frontend/tests/e2e/account-settings-assets.spec.ts` |

## 独立表达红线

1. 不使用“火宝/Huobao”及其 logo、域名风格、宣传语或演示素材。
2. 不逐文件翻译上游实现，不复制其提示词、Skills、测试、CSS、数据库定义或独特文案。
3. UI 以制作效率和本项目信息架构为依据独立设计，不以截图像素相似为目标。
4. 对外宣传使用“同类功能”或“功能对等”，不暗示官方、授权或兼容关系。
5. 商业发布前单独审计依赖、FFmpeg 构建、字体、图标、模型服务条款、声音和肖像授权。

## 下一阶段完成标准

L1 用户可观察功能对等（项目/工作台入口一致）已完成。
L2 中可本机自动完成的部分（SBOM 路径、依赖漏洞、Go 1.26.5、WebKit 项目级 E2E、SQLite/PostgreSQL Compose 冷启动与备份回滚演练）已完成并记入 `docs/COMMERCIAL_RELEASE.md`。

仍依赖真实账号/发布流程的门槛：

- 使用真实 OpenAI、MiniMax、火山、Vidu、阿里账号执行 smoke test，并归档供应商协议版本与测试证据。
- 使用真实 SMTP 账号验收组织邀请和密码恢复投递。
- 恢复 GitHub Actions 账单/额度后，在发布 CI 重跑 `verify`（含 WebKit 与 SBOM artifact 上传）。
- 使用已审核许可证扫描工具生成正式 SPDX/CycloneDX 结论，并完成 FFmpeg 构建、字体、图标、声音、肖像和模型条款的法务归档。
