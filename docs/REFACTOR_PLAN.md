# FlyAiMovie 重构计划与收口记录

> 计划生成：2026-08-04
> 收口日期：2026-08-08
> 范围：代码结构、公共 helper、前端状态复用与自动化验证
> 状态：**已完成（代码重构范围）**

本次收口以不改变 HTTP 路由、请求/响应契约、数据库 schema 和 clean-room 边界为约束。商业发布仍需真实供应商、SMTP、法务和生产基础设施证据；这些外部门槛不计入本计划的代码未完成项，见
[`docs/COMMERCIAL_RELEASE.md`](./COMMERCIAL_RELEASE.md)。

## 1. 已完成内容

### 1.1 公共 helper 去重

- `backend/internal/textutil/` 统一承载 `AsString`、`AsInt`、`AsUint`、`UniquePositiveIDs`、`FirstNonEmpty`、`FirstNonBlank` 和安全列表解析。
- `httpapi` 与 `services/agents` 保留一行兼容 wrapper，以维持原有包内调用和两种历史解析语义；转换实现不再逐包复制。
- 为 JSON 数值转换、字符串转换、ID 去重和边界分支补充 `backend/internal/textutil/textutil_test.go`。
- `production`、`httpapi` 等调用点已经改用共享字符串 fallback；没有把 whitespace-only 的两种历史语义强行合并。

### 1.2 后端按职责拆分

- Agent、媒体、资源、设置等原有大文件已按职责拆为同包文件，保留函数签名和路由注册方式。
- `backend/internal/services/production/service_terminal.go` 承载终态/取消逻辑。
- `backend/internal/services/jobs/service_state.go` 承载结果缓存、重试和时间状态逻辑。
- 当前生产源文件均不超过 800 行；未为本次结构调整修改 API、持久化模型或外部服务契约。

### 1.3 前端复用与视图拆分

- `frontend/src/composables/usePolling.ts` 继续统一轮询、可见性守卫和卸载清理。
- 新增 `frontend/src/composables/useAsyncState.ts`，并先接入 `JobsView`，统一最新请求保护、loading/error/reset 语义。
- `WorkbenchView` 已拆为 Script、Cast、Grid、Boards、Export 五个阶段组件，并通过 context 与 action composable 共享状态。
- `SettingsView` 已拆为核心状态、组织操作、服务操作和提示词操作模块。
- `DramaView` 已拆为 `useDrama` composable；模板仍由原视图负责，避免引入额外运行时层级。
- 轮询/请求和视图拆分均保留原有路由、事件和 UI 交互契约。

## 2. 收口后的文件规模

| 文件/模块 | 当前行数 | 结果 |
|---|---:|---|
| `frontend/src/views/WorkbenchView.vue` | 272 | 主视图低于 500 行 |
| `frontend/src/views/SettingsView.vue` | 264 | 主视图低于 500 行 |
| `frontend/src/views/DramaView.vue` | 218 | 主视图低于 500 行 |
| `backend/internal/services/production/service.go` | 771 | 低于 800 行 |
| `backend/internal/services/jobs/service.go` | 758 | 低于 800 行 |
| `backend/internal/services/agents/runner.go` | 561 | 低于 800 行 |
| `backend/internal/httpapi/media.go` | 355 | 低于 800 行 |
| `backend/internal/httpapi/resources.go` | 356 | 低于 800 行 |
| `backend/internal/httpapi/settings.go` | 499 | 低于 800 行 |

拆分后的模块单文件也控制在 800 行以内；现有少数大型测试文件属于既有测试资产，本次未进行无关重排。

## 3. 验证结果（2026-08-08）

| 检查 | 结果 |
|---|---|
| `go test ./... -race -count=1` | 通过 |
| `go vet ./...` | 通过 |
| `make coverage` | 通过，总覆盖率 81.2%，门槛 80% |
| `npm run build` | 通过，包含 `vue-tsc -b` 与 Vite 构建 |
| `make supply-chain-test` | 通过 |
| Playwright desktop | 51/51 断言通过 |
| Playwright mobile | 8/8 断言通过 |
| Playwright WebKit | 51/51 断言通过 |

桌面 Chrome 在 51 条断言全部通过后，本机浏览器进程清理阶段仍会挂起，需要人工中断 runner；没有发现断言失败。移动端和 WebKit 项目均正常退出。该问题属于本机 Chrome runner 清理，不是功能验证失败，已在验收文档中单独记录。

## 4. 验收清单

- [x] Workbench 主视图降至 500 行以内，并完成五阶段组件拆分。
- [x] Settings 与 Drama 主视图降至 500 行以内。
- [x] 生产源文件与拆分后的视图/模块不超过 800 行。
- [x] `AsString`/`AsInt`/`AsUint`/ID 去重的实现集中到共享 helper，保留兼容 wrapper 的历史语义。
- [x] `FirstNonEmpty`/`FirstNonBlank` 由共享文本工具提供，调用点不再各自实现。
- [x] 轮询由 `usePolling` 管理；异步状态已通过 `useAsyncState` 渐进接入。
- [x] 前端 `vue-tsc`、构建和三类 Playwright 项目完成验证。
- [x] 后端 race、vet、覆盖率和供应链检查完成。
- [x] 未改变路由、请求/响应契约、数据库 schema 或 clean-room 边界。
- [x] 本计划状态和验收记录已更新，不再标记为“规划中”。

## 5. 有意保留的范围

以下事项仍属于商业发布的外部门槛，不是本次重构代码的遗留项：真实 OpenAI/MiniMax/火山/Vidu/阿里账号 smoke test、真实 SMTP 投递、公开 HTTPS/真实 Safari 人工验收、正式 SPDX/SBOM 与许可证归档、生产密钥/TLS/监控配置，以及声音、肖像、字体和模型条款授权。它们的当前状态以商业发布文档为准。

## 6. 维护约束

- 后续功能优先放入已有领域模块，不重新把逻辑塞回主视图或主 service。
- 新的纯函数先补单测，再迁移调用点。
- 继续保持输入校验、组织归属校验、错误语义和外部请求安全边界不变。
