# Auth Registration and Password Management Design

Date: 2026-07-27  
Status: Approved for implementation planning  
Scope: Public self-service registration, platform registration settings, password change continuity

## Problem

FlyAiMovie already supports:

- first-time bootstrap via `/auth/setup`
- login
- forgot-password / password-reset
- authenticated change-password
- organization invitations and admin-created members

It does **not** expose public self-service registration. After the first owner is created, new independent users cannot create their own account and organization from the login page. Password change already exists in settings, but registration is the missing product surface.

## Goals

1. Allow public self-service registration from the login/register UI.
2. On successful registration, create a user, a new organization, and an owner membership.
3. Provide platform-level registration settings:
   - enable/disable public registration
   - require/skip email verification
4. Restrict registration-setting changes to the platform admin (the first setup owner).
5. Keep existing password-change behavior working and reachable.
6. Default path remains low-friction: registration enabled, email verification off, immediate login after register.

## Non-Goals

- OAuth / third-party identity providers
- Letting arbitrary organization admins change site-wide registration policy
- Auto-joining an existing organization during public registration (invitation flow remains separate)
- Full email-verification mailer UX as a hard dependency of the first implementation slice
- Changing organization invitation semantics

## Current Baseline

### Backend

- `POST /auth/setup` creates the first user + organization + owner membership and session.
- `POST /auth/login` authenticates and creates a session cookie.
- `POST /auth/change-password` requires current password, updates hash, revokes all sessions/reset tokens, creates a fresh session.
- Password reset and invitations already exist.
- Users have `status`, but no `email_verified_at` or platform-admin flag.
- No platform settings table exists today.

### Frontend

- `AuthView` supports setup, login, and forgot-password modes only.
- Settings → Security already has change-password UI.
- Settings tabs are organization-scoped; there is no platform-settings section yet.

## Recommended Approach

Approach A: public registration API + platform settings model + settings UI for platform admin.

Rejected alternatives:

- Config/env-only switches: faster, but does not match the requested in-app registration settings.
- Full verification mailer in the same first slice: desirable later, but expands scope beyond the approved MVP gate.

## Architecture

```text
Browser AuthView
  |  status (registration flags)
  |  register / login / change-password
  v
Auth HTTP API
  |  platform settings read/write (platform admin only)
  |  user + org + membership transaction
  v
DB
  users
  organizations
  memberships
  platform_settings
  sessions
  (optional later) email_verification_tokens
```

### Platform admin rule

- Add `users.is_platform_admin` boolean.
- `/auth/setup` sets the first owner to `is_platform_admin=true`.
- Public registration always creates normal users with `is_platform_admin=false`.
- Only platform admins may read/update platform registration settings via authenticated admin endpoints.
- Organization owner/admin roles alone are insufficient.

### Registration defaults

Seed or ensure a single platform settings row:

| Key | Default | Meaning |
|---|---|---|
| `registration_enabled` | `true` | Public `/auth/register` and register UI are available when setup is complete |
| `require_email_verification` | `false` | When false, registered users are immediately verified and signed in |

## Data Model

### `users` additions

- `email_verified_at *string`  
  RFC3339 timestamp when verified. Null means unverified.
- `is_platform_admin bool`  
  Default false. Only setup-created first owner is true initially.

### `platform_settings`

Single-row or singleton settings record:

- `id` (fixed singleton, e.g. `1`)
- `registration_enabled bool`
- `require_email_verification bool`
- `updated_at string`
- `updated_by *uint` (user id, nullable)

Migration/startup must ensure the singleton exists with defaults.

### Optional first-slice reserve for later verification mailer

Either:

- add `email_verification_tokens` table now, unused until mailer lands, or
- defer token table until verification delivery is implemented

Preferred: reserve fields/status now; implement token table when mail delivery is built. First slice still enforces the gate using `email_verified_at`.

## API Design

### `GET /auth/status`

Extend response:

```json
{
  "enabled": true,
  "setup_required": false,
  "registration_enabled": true,
  "require_email_verification": false
}
```

Notes:

- Public endpoint.
- When `setup_required=true`, registration UI is irrelevant; setup remains the only bootstrap path.
- When auth is disabled, existing behavior remains authoritative; registration features are inactive.

### `POST /auth/register`

Request:

```json
{
  "organization_name": "My Studio",
  "display_name": "Alice",
  "email": "alice@example.com",
  "password": "twelve+ chars"
}
```

Validation:

- auth enabled
- setup already completed (`users` count > 0)
- `registration_enabled=true`
- normalized email valid and unique
- organization name required/trimmed, max length aligned with setup
- display name optional; fallback to email like setup
- password 12–72 bytes

Behavior when email verification is **not** required:

1. Create user with `email_verified_at=now`, `status=active`, `is_platform_admin=false`
2. Create organization + unique slug
3. Create owner membership
4. Seed organization defaults (same helper path as setup, without legacy claim)
5. Create session + CSRF cookie
6. Return `201` with standard auth actor payload (`role=owner`)

Behavior when email verification **is** required:

1. Create user with `email_verified_at=null`
2. Create organization + owner membership + org defaults
3. Do **not** create session
4. Return `201` with:

```json
{
  "verification_required": true,
  "email": "alice@example.com"
}
```

Errors:

| Condition | Status | Message |
|---|---|---|
| auth disabled | 400 | authentication is disabled |
| setup not complete | 409 or 400 | setup required / use setup |
| registration disabled | 403 | registration disabled |
| invalid body/password/org | 400 | validation message |
| email taken | 409 | email already registered |

Security notes:

- Do not return password hashes or session secrets in JSON beyond existing CSRF token convention for authenticated actors.
- Attach the same auth rate limiting used by login/setup if present.
- Registration must not claim legacy `organization_id=0` resources; only first setup does that.

### `POST /auth/login` adjustment

After credential checks:

- If platform setting `require_email_verification=true` and user `email_verified_at` is null:
  - reject with `403 email verification required`
  - do not issue session

Users created while verification was off keep their existing verified timestamp and remain login-capable.

### Platform settings

#### `GET /auth/platform-settings`

- requires session
- requires `is_platform_admin=true`
- returns current flags

#### `PUT /auth/platform-settings`

Request:

```json
{
  "registration_enabled": true,
  "require_email_verification": false
}
```

- requires session + CSRF + platform admin
- updates singleton settings
- records `updated_by` / `updated_at`
- returns updated settings

Non-admin => `403`.

### Password change

Keep existing:

- `POST /auth/change-password`
- Settings modal UI

No behavioral redesign required beyond regression coverage and ensuring the security section remains obvious.

## Frontend Design

### Auth status store

`authStore.initialize` / status handling should track:

- `registrationEnabled`
- `requireEmailVerification`

### AuthView / routing

Options:

- reuse `AuthView` with a `register` mode, and/or
- add `/register` route pointing at the same component

Required UI:

- Login page shows “注册” only when:
  - auth enabled
  - setup not required
  - registration enabled
- Register form fields:
  - organization/space name
  - display name
  - email
  - password
  - confirm password
- Client-side password confirmation and 12–72 byte checks
- Success without verification => navigate home as logged-in owner
- Success with verification required => show success/waiting state, no authenticated app shell

### Settings

In Settings → 安全与数据 (or a clearly labeled adjacent platform block):

- visible only to platform admin
- card title: 注册设置
- toggles:
  - 开放公开注册
  - 要求邮箱校验
- save action through platform-settings API
- helper text explaining:
  - disabled registration hides public signup
  - enabling email verification blocks unverified logins
  - full verification email delivery may be pending if not yet implemented

Actor payload should expose enough for the UI to know platform-admin status, e.g. `user.is_platform_admin` on `/auth/me`, login, setup, and register responses.

## Email Verification Policy for This Slice

Approved product intent: settings switch exists now.

First implementation slice must:

1. Persist `require_email_verification`
2. Honor it on register and login gates
3. Store `email_verified_at`

First implementation slice may defer:

- verification email SMTP templates
- consume-token endpoint
- resend verification UI

If verification is enabled before mailer exists, registered users are created but cannot log in until a later verification path or admin process exists. UI copy must not claim an email was sent unless sending is actually implemented.

Follow-up slice:

- create hashed verification tokens
- email link under public HTTPS base
- consume endpoint marks `email_verified_at` and optionally signs in
- resend with rate limits
- do not enumerate accounts beyond existing password-reset norms

## Error Handling and Security

- Reuse bcrypt cost and password byte limits.
- Normalize emails the same way as setup/login.
- Unique email index remains the source of truth for conflicts.
- Session cookies remain HTTP-only, Secure according to config, SameSite=Lax.
- CSRF required on authenticated mutations, including platform settings and change-password.
- Audit useful platform-setting changes if audit helpers are easy to reuse; not blocking if absent.
- Never log raw passwords or raw tokens.

## Testing Plan

### Backend

- register success creates user/org/owner and session when verification off
- register rejected when registration disabled
- register rejected when setup still required
- duplicate email => 409
- platform settings get/put allowed only for platform admin
- ordinary org owner cannot change platform settings
- verification required:
  - register returns verification_required and no session cookie
  - login rejected for unverified user
- setup first user is platform admin and email verified
- change-password still rotates sessions and invalidates old password

### Frontend

- login shows register CTA only when status allows it
- register form validation and submit wiring
- verification-required success state
- platform settings card visible only to platform admin
- change-password modal still works

## Rollout / Migration

1. Migrate user columns and platform settings singleton.
2. Backfill existing users:
   - `email_verified_at=created_at` or now for active users
   - first/bootstrap platform owner `is_platform_admin=true` (deterministic rule: the original setup user; if multiple historical owners exist, choose lowest user id with an owner membership on the earliest organization, documented in implementation)
3. Deploy API + UI.
4. Default remains open registration without verification.

## Implementation Phases

### Phase 1 (this feature)

- models + migration/backfill
- register API
- status flags
- platform settings API
- AuthView register mode
- settings toggles for platform admin
- login gate for unverified users
- tests above
- password-change regression

### Phase 2 (follow-up)

- verification token table
- SMTP verification mail
- public verify page
- resend verification

## Success Criteria

- A new visitor can register, get an owner workspace, and start using the app when verification is off.
- Platform admin can disable registration from settings and the public CTA/API both stop working.
- Platform admin can require email verification; unverified users cannot log in.
- Existing password change continues to work.
- Non-platform organization owners cannot change site registration policy.

## Open Follow-ups (explicitly out of Phase 1 completion)

- Actual verification email delivery and token consumption UX
- Admin tooling to manually mark a user verified
- Metrics/rate-limit dashboards for registration abuse
