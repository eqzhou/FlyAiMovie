# 认证注册与密码管理设计

日期：2026-07-27  
状态：已实现（Tasks 1–6 完成；邮箱验证邮件仍为后续）  
范围：公开自助注册、平台级注册设置、修改密码能力延续

## 问题

FlyAiMovie 现已支持：

- 首次初始化 `/auth/setup`
- 登录
- 忘记密码 / 密码重置
- 已登录修改密码
- 组织邀请与管理员直接添加成员

但**没有**公开自助注册。首个 owner 创建完成后，新的独立用户无法在登录页自行创建账号与组织。修改密码能力已存在，缺的是注册产品面。

## 目标

1. 在登录/注册 UI 提供公开自助注册。
2. 注册成功时创建用户、新组织，并赋予 owner 成员关系。
3. 提供平台级注册设置：
   - 开启/关闭公开注册
   - 是否要求邮箱校验
4. 仅平台管理员（首次 setup 的 owner）可修改注册设置。
5. 保持现有修改密码行为可用、可达。
6. 默认低摩擦：开放注册、不强制邮箱校验、注册后立即登录。

## 非目标

- OAuth / 第三方登录
- 任意组织 admin 修改全站注册策略
- 公开注册时自动加入已有组织（仍走邀请）
- 将完整邮箱验证邮件流作为第一期硬依赖
- 改变组织邀请语义

## 当前基线

### 后端

- `POST /auth/setup` 创建首个用户 + 组织 + owner 成员关系与会话
- `POST /auth/login` 校验凭据并创建会话 cookie
- `POST /auth/change-password` 校验当前密码、更新哈希、撤销全部会话/重置 token、重建会话
- 密码重置与邀请已存在
- 用户有 `status`，尚无 `email_verified_at` 或平台管理员标记
- 尚无平台设置表

### 前端

- `AuthView` 仅支持 setup、登录、忘记密码
- 设置 → 安全 已有修改密码 UI
- 设置页以组织范围为主，尚无平台设置分区

## 推荐方案

方案 A：公开注册 API + 平台设置模型 + 平台管理员设置 UI。

已否决：

- 仅配置文件/环境变量开关：更快，但不满足“系统设置内可改”
- 第一期同时做完整验证邮件流：范围过大

## 架构

```text
浏览器 AuthView
  |  status（注册开关）
  |  register / login / change-password
  v
Auth HTTP API
  |  平台设置读写（仅平台管理员）
  |  用户 + 组织 + 成员关系事务
  v
数据库
  users
  organizations
  memberships
  platform_settings
  sessions
  （二期可选）email_verification_tokens
```

### 平台管理员规则

- 增加 `users.is_platform_admin` 布尔字段
- `/auth/setup` 将首个 owner 设为 `is_platform_admin=true`
- 公开注册用户一律 `is_platform_admin=false`
- 仅平台管理员可通过鉴权接口读写平台注册设置
- 仅有组织 owner/admin 角色不够

### 注册默认值

启动时确保存在单例平台设置：

| 字段 | 默认 | 含义 |
|---|---|---|
| `registration_enabled` | `true` | setup 完成后开放 `/auth/register` 与注册 UI |
| `require_email_verification` | `false` | 为 false 时注册用户立即视为已验证并登录 |

## 数据模型

### `users` 新增

- `email_verified_at *string`：RFC3339；空表示未验证
- `is_platform_admin bool`：默认 false；仅 setup 首个 owner 初始为 true

### `platform_settings`

单例记录：

- `id`（固定为 1）
- `registration_enabled bool`
- `require_email_verification bool`
- `updated_at string`
- `updated_by *uint`（用户 id，可空）

迁移/启动必须确保单例存在且带默认值。

### 邮箱验证后续预留

第一期用 `email_verified_at` 做门禁；验证 token 表放到邮件投递实现时再补。

## API 设计

### `GET /auth/status`

扩展返回：

```json
{
  "enabled": true,
  "setup_required": false,
  "registration_enabled": true,
  "require_email_verification": false
}
```

说明：

- 公开接口
- `setup_required=true` 时仍只走 setup
- auth 关闭时沿用现有行为，注册功能不生效

### `POST /auth/register`

请求：

```json
{
  "organization_name": "My Studio",
  "display_name": "Alice",
  "email": "alice@example.com",
  "password": "twelve+ chars"
}
```

校验：

- auth 已启用
- setup 已完成（用户数 > 0）
- `registration_enabled=true`
- 邮箱规范化后合法且唯一
- 组织名必填/去空白，长度与 setup 对齐
- 显示名可选，空则回退邮箱
- 密码 12–72 字节

**不要求邮箱校验时：**

1. 创建用户：`email_verified_at=now`、`status=active`、`is_platform_admin=false`
2. 创建组织与唯一 slug
3. 创建 owner 成员关系
4. 种子组织默认配置（与 setup 相同 helper，但不认领 legacy 资源）
5. 创建 session + CSRF cookie
6. 返回 `201` 与标准 actor 载荷（`role=owner`）

**要求邮箱校验时：**

1. 创建用户：`email_verified_at=null`
2. 创建组织 + owner + 组织默认配置
3. **不**创建 session
4. 返回 `201`：

```json
{
  "verification_required": true,
  "email": "alice@example.com"
}
```

错误：

| 条件 | 状态 | 文案 |
|---|---|---|
| auth 关闭 | 400 | authentication is disabled |
| 尚未 setup | 409 或 400 | setup required / use setup |
| 注册关闭 | 403 | registration disabled |
| 参数/密码/组织非法 | 400 | 校验文案 |
| 邮箱已存在 | 409 | email already registered |

安全：

- 不返回 password hash；已登录 actor 仅按现有约定返回 CSRF
- 若 login/setup 有限流，注册挂同一限流
- 注册不得认领 `organization_id=0` 的 legacy 资源（仅 setup 可）

### `POST /auth/login` 调整

凭据校验后：

- 若 `require_email_verification=true` 且 `email_verified_at` 为空：
  - 返回 `403 email verification required`
  - 不发 session

关闭校验期间创建、已有 `email_verified_at` 的用户可继续登录。

### 平台设置

#### `GET /auth/platform-settings`

- 需要 session
- 需要 `is_platform_admin=true`
- 返回当前开关

#### `PUT /auth/platform-settings`

请求：

```json
{
  "registration_enabled": true,
  "require_email_verification": false
}
```

- 需要 session + CSRF + 平台管理员
- 更新单例，记录 `updated_by` / `updated_at`
- 返回更新后设置

非管理员 => `403`

### 修改密码

保持现有：

- `POST /auth/change-password`
- 设置页弹窗

除回归测试与入口可达性外，不做行为重设计。

## 前端设计

### auth 状态

`authStore` 需跟踪：

- `registrationEnabled`
- `requireEmailVerification`

### AuthView / 路由

- 复用 `AuthView` 的 register 模式，和/或增加 `/register` 路由
- 仅当 auth 启用、非 setup、且 `registration_enabled` 时显示「注册」
- 注册表单：空间名称、显示名、邮箱、密码、确认密码
- 前端校验确认密码与 12–72 字节
- 无需验证时成功后进首页
- 需要验证时展示等待态，不进入已登录壳层

### 设置页

在「安全与数据」中：

- 仅平台管理员可见「注册设置」
- 开关：开放公开注册、要求邮箱校验
- 通过 platform-settings API 保存
- 文案说明：关闭注册会隐藏公开入口；开启校验会阻止未验证登录；完整验证邮件若未实现不得声称已发送

`/auth/me`、login、setup、register 的 actor 需暴露 `user.is_platform_admin`。

## 本阶段邮箱校验策略

必须：

1. 持久化 `require_email_verification`
2. 在注册/登录门禁中生效
3. 存储 `email_verified_at`

可延后：

- 验证邮件 SMTP 模板
- consume-token 接口
- 重发验证 UI

若在邮件流完成前打开校验：用户可被创建，但在后续验证能力出现前无法登录。UI 不得在未真实发信时声称“已发送邮件”。

二期：

- 哈希验证 token
- HTTPS 邮件链接
- consume 标记 `email_verified_at`，可选自动登录
- 限流重发
- 不额外枚举账号

## 错误处理与安全

- 复用 bcrypt 与密码字节限制
- 邮箱规范化与 setup/login 一致
- 邮箱唯一索引作为冲突真相源
- Session cookie：HttpOnly，Secure 跟随配置，SameSite=Lax
- 已登录变更（平台设置、改密）必须 CSRF
- 可复用审计则记录平台设置变更；非阻塞
- 永不记录明文密码或原始 token

## 测试计划

### 后端

- 关闭校验时注册成功：建 user/org/owner 并发 session
- 注册关闭被拒绝
- 尚未 setup 时注册被拒绝
- 邮箱冲突 409
- 仅平台管理员可读写平台设置
- 普通组织 owner 不能改平台设置
- 开启校验：注册返回 verification_required 且无 session；未验证登录被拒
- setup 首个用户为平台管理员且已验证
- 改密仍旋转 session 并使旧密码失效

### 前端

- 仅 status 允许时显示注册入口
- 注册表单校验与提交
- 需验证时的成功等待态
- 仅平台管理员看到注册设置
- 修改密码弹窗仍可用

## 迁移 / 发布

1. 迁移用户字段与平台设置单例
2. 回填已有用户：
   - 活跃用户 `email_verified_at=created_at` 或 now
   - 平台管理员：最早组织上最低 user id 的 owner（实现时写清）
3. 部署 API + UI
4. 默认仍为开放注册、不强制校验

## 实现分期

### 第一期（本功能）

- 模型 + 迁移/回填
- 注册 API
- status 标志
- 平台设置 API
- AuthView 注册模式
- 平台管理员设置开关
- 未验证登录门禁
- 上述测试
- 改密回归

### 第二期

- 验证 token 表
- SMTP 验证邮件
- 公开验证页
- 重发验证

## 成功标准

- 校验关闭时，访客可注册、获得 owner 工作区并开始使用
- 平台管理员可关闭注册，公开入口与 API 同时失效
- 平台管理员可要求邮箱校验；未验证用户不能登录
- 现有修改密码继续可用
- 非平台组织 owner 不能改全站注册策略

## 明确后续项（不算第一期完成）

- 真实验证邮件投递与 token 消费 UX
- 管理员手动标记已验证
- 注册滥用指标/限流看板
