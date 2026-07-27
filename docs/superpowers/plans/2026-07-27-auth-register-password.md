# 公开注册与密码管理实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans，按任务逐项实现。步骤使用 checkbox（`- [ ]`）跟踪。

**Goal:** 为 FlyAiMovie 增加公开自助注册（创建账号 + 新组织 + owner），提供平台级注册设置（开放注册 / 要求邮箱校验），并保证现有修改密码能力继续可用。

**Architecture:** 在现有 cookie session 认证上扩展：新增 `platform_settings` 单例与用户字段（`email_verified_at`、`is_platform_admin`）；`POST /auth/register` 复用 setup 的建账号/组织事务但不认领 legacy 资源；`/auth/status` 与设置页暴露/管理平台开关；邮箱校验第一期只做门禁，不发验证邮件。

**Tech Stack:** Go + Gin + GORM，Vue 3 + TypeScript，现有 httptest 后端测试与 Playwright e2e。

## Global Constraints

- 文档与用户可见文案使用中文；API 错误 message 可保持与现有英文风格一致（如 `registration disabled`），前端再做人话提示。
- 密码规则不变：12–72 字节，bcrypt。
- 公开注册不得 `claimLegacyResources`；仅 `/auth/setup` 可认领 `organization_id=0`。
- 仅 `is_platform_admin=true` 可改平台注册设置；组织 owner/admin 不够。
- 默认：`registration_enabled=true`，`require_email_verification=false`。
- 第一期不实现验证邮件发送；开启校验时不得声称“已发送邮件”。
- 每个任务先写失败测试，再写最小实现（TDD）。
- 提交信息使用 conventional commits。

---

## 文件结构

**后端**
- 修改：`backend/internal/models/auth.go` — User 字段、PlatformSettings 模型
- 修改：`backend/internal/db/db.go` — AutoMigrate、单例种子、回填
- 修改：`backend/internal/httpapi/auth.go` — status/register/login/setup/authResponse
- 新增或修改：`backend/internal/httpapi/platform_settings.go` — 平台设置 GET/PUT
- 修改：`backend/internal/httpapi/members.go` — change-password 无需行为变更，但 actor 载荷需带平台标记
- 测试：`backend/internal/httpapi/auth_register_test.go`（新）、`backend/internal/db/db_test.go`、既有 `password_test.go` 回归

**前端**
- 修改：`frontend/src/auth.ts` — status 标志、register、actor 类型
- 修改：`frontend/src/views/AuthView.vue` — 注册模式
- 修改：`frontend/src/router/index.ts` — `/register` 路由
- 修改：`frontend/src/views/SettingsView.vue` — 注册设置卡片
- 修改：`frontend/src/api/index.ts` — platform settings API（如不走 authStore）
- 测试：`frontend/tests/e2e/account-settings-assets.spec.ts` 或新 `auth-register.spec.ts`

---

### Task 1: 数据模型、迁移与平台设置单例

**Files:**
- Modify: `backend/internal/models/auth.go`
- Modify: `backend/internal/db/db.go`
- Test: `backend/internal/db/db_test.go`

**Interfaces:**
- Produces:
  - `models.User` 增加 `EmailVerifiedAt *string`、`IsPlatformAdmin bool`
  - `models.PlatformSettings`：`ID uint`、`RegistrationEnabled bool`、`RequireEmailVerification bool`、`UpdatedAt string`、`UpdatedBy *uint`
  - `db.EnsurePlatformSettings(gdb *gorm.DB) error`
  - `db.BackfillAuthRegistrationFields(gdb *gorm.DB) error`（可内联在 AutoMigrate 后）

- [ ] **Step 1: 写失败测试（迁移后字段与单例存在）**

在 `backend/internal/db/db_test.go` 增加：

```go
func TestAutoMigrateSeedsPlatformSettingsAndUserAuthFields(t *testing.T) {
	database, err := Open(t.TempDir() + "/auth-register-migrate.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if !database.Migrator().HasColumn(&models.User{}, "email_verified_at") {
		t.Fatal("missing email_verified_at")
	}
	if !database.Migrator().HasColumn(&models.User{}, "is_platform_admin") {
		t.Fatal("missing is_platform_admin")
	}
	var settings models.PlatformSettings
	if err := database.First(&settings, 1).Error; err != nil {
		t.Fatalf("platform settings: %v", err)
	}
	if !settings.RegistrationEnabled || settings.RequireEmailVerification {
		t.Fatalf("defaults=%+v", settings)
	}
}

func TestAutoMigrateBackfillsVerifiedAndPlatformAdmin(t *testing.T) {
	database, err := Open(t.TempDir() + "/auth-register-backfill.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := "2026-01-01T00:00:00Z"
	org := models.Organization{Name: "Early", Slug: "early", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{Email: "owner@example.com", PasswordHash: "x", DisplayName: "Owner", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	member := models.Membership{OrganizationID: org.ID, UserID: user.ID, Role: "owner", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillAuthRegistrationFields(database); err != nil {
		t.Fatal(err)
	}
	var got models.User
	if err := database.First(&got, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.EmailVerifiedAt == nil || !got.IsPlatformAdmin {
		t.Fatalf("backfill failed: %+v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run:

```bash
cd backend && go test ./internal/db -run 'TestAutoMigrateSeedsPlatformSettingsAndUserAuthFields|TestAutoMigrateBackfillsVerifiedAndPlatformAdmin' -count=1
```

Expected: FAIL（缺模型/函数/列）

- [ ] **Step 3: 最小实现**

1. `models/auth.go`：

```go
type User struct {
	ID               uint    `gorm:"primaryKey" json:"id"`
	Email            string  `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash     string  `gorm:"not null" json:"-"`
	DisplayName      string  `gorm:"not null" json:"display_name"`
	Status           string  `gorm:"not null;default:active" json:"status"`
	IsPlatformAdmin  bool    `gorm:"not null;default:false" json:"is_platform_admin"`
	EmailVerifiedAt  *string `json:"email_verified_at,omitempty"`
	LastLoginAt      *string `json:"last_login_at,omitempty"`
	CreatedAt        string  `gorm:"not null" json:"created_at"`
	UpdatedAt        string  `gorm:"not null" json:"updated_at"`
}

type PlatformSettings struct {
	ID                        uint   `gorm:"primaryKey" json:"id"`
	RegistrationEnabled       bool   `gorm:"not null;default:true" json:"registration_enabled"`
	RequireEmailVerification  bool   `gorm:"not null;default:false" json:"require_email_verification"`
	UpdatedAt                 string `gorm:"not null" json:"updated_at"`
	UpdatedBy                 *uint  `json:"updated_by,omitempty"`
}
```

2. `db.AutoMigrate` 加入 `&models.PlatformSettings{}`，并在成功后调用：

```go
if err := EnsurePlatformSettings(gdb); err != nil {
	return err
}
return BackfillAuthRegistrationFields(gdb)
```

3. 实现：

```go
func EnsurePlatformSettings(gdb *gorm.DB) error {
	var count int64
	if err := gdb.Model(&models.PlatformSettings{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	row := models.PlatformSettings{
		ID:                       1,
		RegistrationEnabled:      true,
		RequireEmailVerification: false,
		UpdatedAt:                response.Now(),
	}
	return gdb.Create(&row).Error
}

func BackfillAuthRegistrationFields(gdb *gorm.DB) error {
	// 1) 活跃且未验证用户：email_verified_at = created_at（空则 now）
	// 2) 若尚无 platform admin：取最早 organization（id 升序）上 role=owner 的最小 user_id，设 is_platform_admin=true
	// 用原生/Updates 完成，保证幂等
	return nil // 替换为真实实现
}
```

- [ ] **Step 4: 跑测试确认通过**

Run:

```bash
cd backend && go test ./internal/db -run 'TestAutoMigrateSeedsPlatformSettingsAndUserAuthFields|TestAutoMigrateBackfillsVerifiedAndPlatformAdmin' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/auth.go backend/internal/db/db.go backend/internal/db/db_test.go
git commit -m "feat: add registration auth fields and platform settings"
```

---

### Task 2: 扩展 auth status / actor 载荷，并实现公开注册 API

**Files:**
- Modify: `backend/internal/httpapi/auth.go`
- Test: `backend/internal/httpapi/auth_register_test.go`（新建）

**Interfaces:**
- Consumes: `models.PlatformSettings`、`db.SeedOrganizationDefaults`、现有 `normalizeEmail`/`validPassword`/`uniqueSlug`/`createSession`/`authResponse`
- Produces:
  - `GET /auth/status` 增加 `registration_enabled`、`require_email_verification`
  - `POST /auth/register`
  - `authResponse` 的 `user` 增加 `is_platform_admin`
  - setup 创建用户：`IsPlatformAdmin=true`，`EmailVerifiedAt=now`

- [ ] **Step 1: 写失败测试**

新建 `backend/internal/httpapi/auth_register_test.go`：

```go
func TestAuthRegisterCreatesOwnerWorkspaceAndSession(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()

	// 先 setup 平台
	setup := performRequest(router, http.MethodPost, "/api/v1/auth/setup", `{
		"organization_name":"Platform","email":"platform@example.com",
		"display_name":"Platform","password":"correct horse battery staple"
	}`, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}

	status := performRequest(router, http.MethodGet, "/api/v1/auth/status", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"registration_enabled":true`) {
		t.Fatalf("status=%d %s", status.Code, status.Body.String())
	}

	reg := performRequest(router, http.MethodPost, "/api/v1/auth/register", `{
		"organization_name":"Indie Studio","email":"indie@example.com",
		"display_name":"Indie","password":"correct horse battery staple"
	}`, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register=%d %s", reg.Code, reg.Body.String())
	}
	if responseCookie(t, reg, "fly_session") == "" {
		t.Fatal("missing session cookie")
	}
	payload := decodeResponse(t, reg)["data"].(map[string]any)
	user := payload["user"].(map[string]any)
	if payload["role"] != "owner" || user["is_platform_admin"] == true {
		t.Fatalf("payload=%v", payload)
	}

	var orgCount int64
	_ = db.DB.Model(&models.Organization{}).Count(&orgCount)
	if orgCount < 2 {
		t.Fatalf("orgCount=%d", orgCount)
	}
}

func TestAuthRegisterRejectedWhenDisabledOrDuplicate(t *testing.T) {
	// setup 后把 platform_settings.registration_enabled=false，register => 403
	// 再打开后同一 email 第二次 => 409
}

func TestAuthRegisterRequiresCompletedSetup(t *testing.T) {
	// 无用户时 register => 409/400，且提示走 setup
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd backend && go test ./internal/httpapi -run 'TestAuthRegister' -count=1
```

Expected: FAIL（无 `/auth/register`）

- [ ] **Step 3: 最小实现**

在 `registerAuth` 增加：

```go
auth.POST("/register", s.authRegister)
```

实现要点：

```go
func (s *Server) authStatus(c *gin.Context) {
	var count int64
	_ = db.DB.Model(&models.User{}).Count(&count).Error
	settings := loadPlatformSettings() // 读 id=1，缺省则默认 true/false
	response.Success(c, gin.H{
		"enabled": s.Cfg.Auth.Enabled,
		"setup_required": count == 0,
		"registration_enabled": settings.RegistrationEnabled,
		"require_email_verification": settings.RequireEmailVerification,
	})
}

func (s *Server) authRegister(c *gin.Context) {
	// auth disabled => 400
	// users count == 0 => 409 setup required
	// !settings.RegistrationEnabled => 403 registration disabled
	// bind body 同 setupInput
	// normalize email / valid password / org name
	// bcrypt hash
	// tx:
	//   email 已存在 => 返回特殊 err => 409
	//   create user (is_platform_admin=false; email_verified_at=now 除非 require verification)
	//   create org + owner membership
	//   SeedOrganizationDefaults(tx, org.ID)  // 不要 claimLegacyResources
	// if require verification: 201 {verification_required, email} 无 cookie
	// else: createSession + set cookie + 201 authResponse role=owner
}

func authResponse(...) gin.H {
	// user 增加 "is_platform_admin": user.IsPlatformAdmin
}

// createInitialAccount: 首个用户 IsPlatformAdmin=true 且 EmailVerifiedAt=now
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd backend && go test ./internal/httpapi -run 'TestAuthRegister|TestAuthSetupLoginCSRFAndLogout' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/auth.go backend/internal/httpapi/auth_register_test.go
git commit -m "feat: add public auth registration endpoint"
```

---

### Task 3: 登录邮箱校验门禁 + 平台设置 API

**Files:**
- Modify: `backend/internal/httpapi/auth.go`（login 门禁）
- Create: `backend/internal/httpapi/platform_settings.go`
- Test: `backend/internal/httpapi/auth_register_test.go`（追加）或 `platform_settings_test.go`

**Interfaces:**
- Produces:
  - `GET /api/v1/auth/platform-settings`
  - `PUT /api/v1/auth/platform-settings`
  - login：当 `require_email_verification && email_verified_at==nil` => 403

- [ ] **Step 1: 写失败测试**

```go
func TestPlatformSettingsOnlyPlatformAdmin(t *testing.T) {
	// setup => platform admin cookie
	// GET/PUT platform-settings 成功
	// 再 register 普通 owner
	// 普通 owner GET/PUT => 403
}

func TestEmailVerificationGateOnRegisterAndLogin(t *testing.T) {
	// setup 后平台管理员打开 require_email_verification
	// register => 201 verification_required，无 session cookie
	// login 该用户 => 403 email verification required
	// 手动把 email_verified_at 写上后 login 成功
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd backend && go test ./internal/httpapi -run 'TestPlatformSettingsOnlyPlatformAdmin|TestEmailVerificationGateOnRegisterAndLogin' -count=1
```

Expected: FAIL

- [ ] **Step 3: 最小实现**

```go
func (s *Server) registerAuth(api *gin.RouterGroup) {
	// ...
	auth.GET("/platform-settings", s.requireSession(), s.getPlatformSettings)
	auth.PUT("/platform-settings", s.requireSession(), s.putPlatformSettings)
}

func requirePlatformAdmin(c *gin.Context) bool {
	actor, ok := currentAuth(c)
	if !ok || !actor.User.IsPlatformAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "platform admin required"})
		return false
	}
	return true
}

// get/put 读写 models.PlatformSettings id=1
// put 更新 RegistrationEnabled / RequireEmailVerification，写 UpdatedAt/UpdatedBy

// authLogin / verifyLogin 之后：
// if settings.RequireEmailVerification && user.EmailVerifiedAt == nil { 403 }
```

注意：关闭校验时历史未验证用户——按 spec，门禁只在设置开启时生效；开启后未验证不能登录。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd backend && go test ./internal/httpapi -run 'TestPlatformSettings|TestEmailVerification|TestChangePasswordRotatesSessions' -count=1
```

Expected: PASS（含改密回归）

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/auth.go backend/internal/httpapi/platform_settings.go backend/internal/httpapi/*_test.go
git commit -m "feat: add platform registration settings and verification gate"
```

---

### Task 4: 前端 authStore + 注册页

**Files:**
- Modify: `frontend/src/auth.ts`
- Modify: `frontend/src/views/AuthView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/App.vue`（若需放行 register 路由名）
- Test: `frontend/tests/e2e/auth-register.spec.ts`（新建，mock API）

**Interfaces:**
- Consumes: `/auth/status`、`/auth/register`
- Produces:
  - `authStore.state.registrationEnabled` / `requireEmailVerification`
  - `authStore.register(...)`
  - 路由 `name: 'register'`

- [ ] **Step 1: 写失败 e2e（mock）**

```ts
// frontend/tests/e2e/auth-register.spec.ts
test('login page shows register entry and can submit registration', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/auth/status') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        code: 200, data: { enabled: true, setup_required: false, registration_enabled: true, require_email_verification: false }
      })})
    }
    if (path === '/api/v1/auth/register' && route.request().method() === 'POST') {
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
        code: 201, data: {
          user: { id: 2, email: 'new@example.com', display_name: 'New', is_platform_admin: false },
          organization: { id: 2, name: 'New Studio', slug: 'new-studio' },
          role: 'owner', csrf_token: 'csrf'
        }
      })})
    }
    if (path === '/api/v1/auth/me') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        code: 200, data: {
          user: { id: 2, email: 'new@example.com', display_name: 'New', is_platform_admin: false },
          organization: { id: 2, name: 'New Studio', slug: 'new-studio' },
          role: 'owner', csrf_token: 'csrf'
        }
      })})
    }
    if (path === '/api/v1/auth/organizations') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data: [] })})
    }
    // dramas list empty etc.
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, data: { items: [] } })})
  })
  await page.goto('/login')
  await page.getByRole('button', { name: '注册' }).click()
  await page.getByLabel('空间名称').fill('New Studio')
  await page.getByLabel('显示名称').fill('New')
  await page.getByLabel('邮箱').fill('new@example.com')
  await page.getByLabel('密码', { exact: true }).fill('a-secure-password')
  await page.getByLabel('确认密码').fill('a-secure-password')
  await page.getByRole('button', { name: '创建账号' }).click()
  await expect(page).toHaveURL(/\/$|\/home|projects/i)
})
```

（按现有 Home 路由实际 URL 调整断言；参考 `account-settings-assets.spec.ts` mock 风格。）

- [ ] **Step 2: 跑测试确认失败**

```bash
cd frontend && npx playwright test tests/e2e/auth-register.spec.ts --project=chromium
```

Expected: FAIL（无注册按钮/路由）

- [ ] **Step 3: 最小实现**

`auth.ts`：

```ts
type AuthActor = {
  user: { id: number; email: string; display_name: string; is_platform_admin?: boolean }
  organization: { id: number; name: string; slug: string }
  role: 'owner' | 'admin' | 'editor' | 'viewer'
  csrf_token?: string
}

// state 增加 registrationEnabled / requireEmailVerification
// initialize 从 status 读取
// register(data) 调用 POST /auth/register
// 若返回 verification_required，不要 setActor；抛出带 code 的错误或返回标记
```

`router/index.ts`：

```ts
{ path: '/register', name: 'register', component: AuthView }
// guard: register 在未登录且 registrationEnabled 时可进；已登录跳 home
```

`AuthView.vue`：

- `isRegister` from route name 或本地 mode
- 字段与 setup 类似
- 登录态显示「注册账号」按钮（当 `authStore.state.registrationEnabled && !setupRequired`）
- 提交调用 `authStore.register`
- 若 verification required：展示中文等待文案，**不要**写“已发送邮件”（第一期）

`App.vue`：未登录白名单加入 `register`。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd frontend && npx playwright test tests/e2e/auth-register.spec.ts --project=chromium
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/auth.ts frontend/src/views/AuthView.vue frontend/src/router/index.ts frontend/src/App.vue frontend/tests/e2e/auth-register.spec.ts
git commit -m "feat: add public registration UI"
```

---

### Task 5: 设置页注册设置 + 改密入口回归

**Files:**
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/api/index.ts`（可选 platformSettingsAPI）
- Modify: `frontend/tests/e2e/account-settings-assets.spec.ts` 或 `auth-register.spec.ts`

**Interfaces:**
- Consumes: `GET/PUT /auth/platform-settings`、`user.is_platform_admin`
- Produces: 安全与数据分区中的「注册设置」卡片

- [ ] **Step 1: 写失败 e2e**

```ts
test('platform admin can see and save registration settings', async ({ page }) => {
  // mock me.is_platform_admin=true
  // mock GET platform-settings
  // 打开设置安全分区，看到「开放公开注册」「要求邮箱校验」
  // 切换并保存，断言 PUT body
})

test('non-platform owner does not see registration settings', async ({ page }) => {
  // is_platform_admin=false，安全区仍有修改密码，但无注册设置
})
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd frontend && npx playwright test tests/e2e/auth-register.spec.ts tests/e2e/account-settings-assets.spec.ts --project=chromium
```

Expected: 新断言 FAIL

- [ ] **Step 3: 最小实现**

```ts
// api/index.ts
export const platformSettingsAPI = {
  get: () => api.get<{ registration_enabled: boolean; require_email_verification: boolean }>('/auth/platform-settings'),
  update: (data: { registration_enabled: boolean; require_email_verification: boolean }) =>
    api.put('/auth/platform-settings', data),
}
```

SettingsView 安全分区：

```vue
<div v-if="authStore.state.actor?.user?.is_platform_admin" class="settings-command">
  <div>
    <strong>注册设置</strong>
    <p class="muted">控制全站公开注册与邮箱校验门禁。开启邮箱校验后，未验证账号无法登录；第一期不会自动发送验证邮件。</p>
  </div>
  <!-- 两个 checkbox + 保存按钮 -->
</div>
<!-- 现有修改密码区块保持不动 -->
```

加载设置：进入 security section 或 onMounted 时，若是平台管理员则 GET。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd frontend && npx playwright test tests/e2e/auth-register.spec.ts --project=chromium
cd backend && go test ./internal/httpapi -run 'TestChangePasswordRotatesSessionsAndInvalidatesOldPassword|TestAuthRegister|TestPlatformSettings' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/SettingsView.vue frontend/src/api/index.ts frontend/tests/e2e
git commit -m "feat: add platform registration settings UI"
```

---

### Task 6: 文档与总回归

**Files:**
- Modify: `docs/superpowers/specs/2026-07-27-auth-register-password-design.md`（若实现有微调，同步中文 spec）
- Optional: `README.md` 认证小节补一句公开注册说明
- Optional: `docs/FUNCTIONAL_PARITY.md` 增加 N-xx 条目

- [ ] **Step 1: 跑后端相关测试套件**

```bash
cd backend && go test ./internal/db ./internal/httpapi -count=1
```

Expected: PASS

- [ ] **Step 2: 跑前端相关 e2e**

```bash
cd frontend && npx playwright test tests/e2e/auth-register.spec.ts tests/e2e/account-settings-assets.spec.ts --project=chromium
```

Expected: PASS

- [ ] **Step 3: 更新中文说明（若需要）**

在 README 认证段落补充：

- 支持公开自助注册（可在平台注册设置关闭）
- 修改密码入口在设置 → 安全与数据
- 邮箱校验开关第一期仅门禁，验证邮件为后续

- [ ] **Step 4: 最终 commit**

```bash
git add README.md docs || true
git commit -m "docs: document public registration and password settings"
```

---

## 自检（对照 spec）

| Spec 要求 | 对应任务 |
|---|---|
| 公开注册创建 user/org/owner | Task 2 |
| status 返回注册开关 | Task 2 |
| 平台设置模型与默认值 | Task 1 |
| 仅平台管理员改设置 | Task 3 / 5 |
| 邮箱校验门禁（不发邮件） | Task 3 |
| setup 首个用户平台管理员且已验证 | Task 1 回填 + Task 2 setup |
| 修改密码延续 | Task 3 回归 + Task 5 UI |
| 前端注册入口/表单 | Task 4 |
| 设置页注册设置 | Task 5 |
| 中文文档 | 已有中文 spec + Task 6 |

无 TBD/占位步骤；类型名前后一致：`PlatformSettings`、`IsPlatformAdmin`、`EmailVerifiedAt`、`registration_enabled`、`require_email_verification`。

---

## 执行交接

计划已保存到 `docs/superpowers/plans/2026-07-27-auth-register-password.md`。

两种执行方式：

1. **Subagent-Driven（推荐）** — 每个 Task 派一个新子代理，任务间复审，迭代快  
2. **Inline Execution** — 本会话按 executing-plans 批量执行并设检查点  

你选哪一种？
