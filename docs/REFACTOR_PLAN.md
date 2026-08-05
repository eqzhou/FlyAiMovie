# FlyAiMovie 重构计划

> 生成日期：2026-08-04
> 依据：当前代码现状（`git HEAD = d40cd3f`）+ 全量结构扫描
> 目标：在不改变外部行为的前提下，降低大文件复杂度、消除重复代码、收敛 `any`
> 状态：**规划中** — 本文档只做分析与排期，不含实现

## 1. 原则与约束

- **行为等价**：所有重构以「零行为变更」为前提，每步以 `go build ./...`、`go vet ./...`、`go test -race ./...` 与前端 `vue-tsc` 验证。
- **小步提交**：一个 PR/提交只做一类重构，便于回滚与审查。
- **测试先行**：抽取纯函数时，为被抽取逻辑补单测再迁移，作为等价性护栏。
- **不扩大范围**：不顺手改功能、不引入新依赖、不动 clean-room 边界。
- **文件规模目标**：单文件 800 行为硬上限，400 行为常态目标（见 coding-style）。

## 2. 现状体检

### 2.1 超限大文件（> 800 行硬上限）

| 文件 | 行数 | 主要职责 | 问题 |
|------|------|----------|------|
| `services/agents/runner.go` | 1376 | Agent 编排 + 工具执行 + 离线兜底 + 解析 | 单文件承担 5 类职责 |
| `httpapi/media.go` | 1276 | 图片/视频/上传/合成/合并/宫格 6 个域 | 6 个 register* 聚在一处 |
| `httpapi/resources.go` | 1056 | 角色/场景/分镜 3 类资源 CRUD | 可按资源拆分 |
| `httpapi/settings.go` | 968 | AI 配置/Agent 配置/技能/语音 | 接近上限，多域混杂 |
| `services/production/service.go` | 885 | 制作状态机 | 接近上限 |
| `services/jobs/service.go` | 838 | 任务调度 | 接近上限 |

### 2.2 前端超限视图

| 文件 | 行数 | ref | computed | function | `any` |
|------|------|-----|----------|----------|-------|
| `views/WorkbenchView.vue` | 2233 | 65 | 28 | 79 | 81 |
| `views/SettingsView.vue` | 1254 | 69 | 8 | 57 | 29 |
| `views/DramaView.vue` | 844 | — | — | — | 24 |

WorkbenchView 的 script 段 1535 行、模板 695 行，是全项目复杂度最高点。

### 2.3 重复代码（已核实逐字/近似重复）

| 重复项 | 位置 | 状态 |
|--------|------|------|
| `asString` / `asInt` / `asUint` | `runner.go:1342-1374`、`resources.go:1030-1056` | **逐字相同** |
| `firstNonEmpty` 家族 | `resources.go`、`runner.go`(`firstNonEmptyLocal`)、`production/service.go`、`adapters/vidu_video.go`(`firstNonEmptyStr`)、`prompttemplate/apply.go` | 5 处近似 |
| `uniquePositiveIDs` vs `uniqueAgentIDs` | `resources.go:981`、`runner.go:1083` | 近似 |
| JSON `Unmarshal` 解析样板 | 全后端 ~20 处（bundle/tags/payload/grid 等） | 分散 |
| 归属查询样板 `Where(...).First(...)` | `resources.go`×46、`media.go`×21、`pipeline.go`×26… | 高频样板 |

### 2.4 前端重复模式

| 模式 | 分布 | 可抽取为 |
|------|------|----------|
| 轮询 `setInterval` + 可见性守卫 + `onUnmounted` 清理 | WorkbenchView、JobsView×2 | `usePolling` composable |
| `loading` / `error` / `busy` 三件套 ref | 10 个视图 | `useAsyncState` composable |
| 状态中文标签内联映射 | WorkbenchView(已抽)、DramaView、SkillRegistryEditor | 复用 labels 模块 |

### 2.5 已有的良好基础（不需重构）

- `response.*` 统一响应封装已被广泛使用（Success 145、BadRequest 363…），保持。
- `s.orgID(c)` 已是统一的组织提取入口（`context.go`），保持。
- `frontend/src/api/types.ts` 已建立领域实体类型，可继续外扩。
- `views/workbench/labels.ts` 已示范纯函数抽取模式，作为后续拆分模板。

## 3. 执行阶段

### Phase 1 — 后端公共工具去重（低风险，高收益）

**目标**：消除逐字重复的类型转换与字符串辅助。

1. 新建 `backend/internal/internalutil/`（或 `services/shared/`）包：
   - `conv.go`：`AsString/AsInt/AsUint`（合并 runner + resources 版本，逐字相同，直接提升）
   - `strutil.go`：`FirstNonEmpty(...string) string`（统一 5 处变体）
   - `idset.go`：`UniquePositiveIDs(any) []uint`（合并 uniqueXxxIDs）
2. 替换各调用点，删除本地副本。
3. 为 3 个工具补齐单测（含 float64/string/nil 分支）。

**验证**：`go test -race ./...` 全绿；`go vet` 无新增。
**风险**：极低（纯函数、逐字迁移）。

### Phase 2 — 后端 JSON 解析样板收敛

**目标**：把散落的 `json.Unmarshal([]byte(x), &y)` 收敛为少量带错误语义的 helper。

1. 在工具包加 `jsonutil.go`：
   - `DecodeStringSlice(raw string) []string`（合并 `decodeGridStrings`）
   - `DecodeUintSlice(raw string) []uint`（合并 `decodeGridIDs`）
   - `DecodeTags(raw string) []string`（dramas.go×2）
2. 仅替换「解析失败即返回空/忽略」的安全场景；对有业务错误分支的解析（bundle 凭据校验等）**不动**。

**验证**：同上。
**风险**：低（限定在幂等安全解析）。

### Phase 3 — 后端大文件按域拆分（中风险）

**目标**：把 4 个超限/临界文件拆为 <800 行的域文件，**不改函数签名与路由**。

1. `media.go`(1276) → 按 register 域拆：
   - `media_images.go` / `media_videos.go` / `media_upload.go` / `media_compose.go` / `media_merge.go` / `media_grid.go`
   - 共用校验（`validateGenerationOwnership`、`validateGridOwnership`）留 `media_shared.go`。
2. `resources.go`(1056) → `resource_characters.go` / `resource_scenes.go` / `resource_storyboards.go`，共用校验入 `resource_shared.go`。
3. `runner.go`(1376) → 按职责拆（同包多文件）：
   - `runner_core.go`（Run/RunObserved/编排）
   - `runner_tools.go`（execTool/saveDedup*/saveStoryboards）
   - `runner_prompt.go`（resolveSystemPrompt*/loadSkill/defaultPrompt/toolCatalog）
   - `runner_offline.go`（offlineFallback/extractOffline*）
4. `settings.go`(968) → `settings_ai_configs.go` / `settings_agent_configs.go` / `settings_voices.go`。

> 说明：同包拆分（同 `package httpapi` / `package agents`），仅移动函数、不改可见性，`git` diff 以「移动」为主，最易审查。

**验证**：拆分前后 `go build` + 全量测试对比；每个文件拆分单独提交。
**风险**：中（移动量大，靠编译器与测试兜底）。

### Phase 4 — 前端复用层（中风险）

**目标**：抽取轮询与异步状态两个 composable，收敛重复。

1. `composables/usePolling.ts`：封装 `setInterval` + `document.visibilityState` 守卫 + `onUnmounted` 清理 + 条件谓词。替换 WorkbenchView / JobsView 三处定时器。
2. `composables/useAsyncState.ts`：封装 `loading/error` + `run(fn)` 包装（try/catch/finally）。渐进接入视图，不强制一次替换全部。
3. `DramaView`、`SkillRegistryEditor` 的内联状态标签映射迁到 `labels` 模块（复用 Phase 已建模式）。

**验证**：`vue-tsc --noEmit`；Playwright 关键路径（首页/工作台加载）冒烟。
**风险**：中（触及运行时定时器逻辑，需保证清理语义不变）。

### Phase 5 — WorkbenchView 拆分（较高风险，压轴）

**目标**：把 2233 行的工作台按阶段拆为子组件，主视图降到 <500 行。

1. 按 5 个阶段抽子组件（props 向下、emit 向上）：
   - `workbench/StageScript.vue` / `StageCast.vue` / `StageGrid.vue` / `StageBoards.vue` / `StageExport.vue`
2. 共享状态用 `provide/inject` 或一个 `useWorkbench(episodeId)` composable 承载数据加载与轮询。
3. `any` 收敛：把 65 个 ref 中的核心实体 ref 换成 `types.ts` 类型。

**验证**：Playwright 工作台全流程（脚本→分镜→导出）回归；视觉快照对比。
**风险**：较高（状态耦合最深）。**建议放最后**，前面阶段稳定后再动。

## 4. 建议执行顺序

```
Phase 1（去重工具）→ Phase 2（JSON 收敛）→ Phase 3（后端拆分）
→ Phase 4（前端 composable）→ Phase 5（Workbench 拆分）
```

低风险纯函数先行建立信心与工具底座；后端拆分与前端 composable 相互独立可并行；WorkbenchView 压轴，依赖前置 composable 与类型。

## 5. 不做清单（明确排除）

- 不改任何 HTTP 路由、请求/响应契约、DB schema。
- 不动 clean-room 边界与业务语义。
- 不重写 `response.*`、`s.orgID`、鉴权中间件等已收敛的良好基础。
- 不为「未来可能用到」引入抽象（YAGNI）。
- 不在重构中混入功能变更或性能优化（另开任务）。

## 6. 风险与回滚

| 阶段 | 风险点 | 缓解 |
|------|--------|------|
| 1-2 | 合并版本行为细微差异 | 迁移前补单测覆盖各类型分支 |
| 3 | 大量函数移动引入编译/可见性错误 | 每文件单独提交，编译器 + 全测兜底 |
| 4 | 定时器清理语义变化导致泄漏 | composable 内统一 `onUnmounted`，冒烟验证 |
| 5 | 工作台状态耦合断裂 | Playwright 全流程回归 + 视觉快照 |

回滚策略：每阶段独立提交，出问题按提交粒度 `git revert`。

## 7. 验收标准

- [ ] 无文件超过 800 行（`runner.go`/`media.go`/`resources.go`/`WorkbenchView.vue` 达标）
- [ ] `asString`/`firstNonEmpty`/`uniqueXxxIDs` 全局单一实现
- [ ] 前端轮询与异步状态经由 composable，无散落 `setInterval`
- [ ] `go build ./...` + `go vet ./...` + `go test -race ./...` 全绿
- [ ] `vue-tsc --noEmit` 无错误；关键路径 Playwright 通过
- [ ] 全程零外部行为变更（路由/契约/schema 不变）
