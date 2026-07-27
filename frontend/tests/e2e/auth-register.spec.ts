import { expect, test } from '@playwright/test'

test('desktop: login page shows register entry and can submit registration', async ({ page }) => {
  let registered = false
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let data: unknown = { items: [] }
    let status = 200

    if (path === '/api/v1/auth/status') {
      data = {
        enabled: true,
        setup_required: false,
        registration_enabled: true,
        require_email_verification: false,
      }
    } else if (path === '/api/v1/auth/register' && request.method() === 'POST') {
      registered = true
      status = 201
      data = {
        user: { id: 2, email: 'new@example.com', display_name: 'New', is_platform_admin: false },
        organization: { id: 2, name: 'New Studio', slug: 'new-studio' },
        role: 'owner',
        csrf_token: 'csrf',
      }
    } else if (path === '/api/v1/auth/me') {
      if (!registered) {
        return route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({ code: 401, message: 'unauthorized' }),
        })
      }
      data = {
        user: { id: 2, email: 'new@example.com', display_name: 'New', is_platform_admin: false },
        organization: { id: 2, name: 'New Studio', slug: 'new-studio' },
        role: 'owner',
        csrf_token: 'csrf',
      }
    } else if (path === '/api/v1/auth/organizations') {
      data = registered
        ? [{ id: 2, name: 'New Studio', slug: 'new-studio', role: 'owner', current: true }]
        : []
    } else if (path === '/api/v1/dramas') {
      data = { items: [] }
    }

    return route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify({ code: status, data, message: status < 400 ? 'success' : 'error' }),
    })
  })

  await page.goto('/login')
  await page.getByRole('button', { name: '注册' }).click()
  await expect(page).toHaveURL(/\/register/)
  await page.getByLabel('空间名称').fill('New Studio')
  await page.getByLabel('显示名称').fill('New')
  await page.getByLabel('邮箱').fill('new@example.com')
  await page.getByLabel('密码', { exact: true }).fill('a-secure-password')
  await page.getByLabel('确认密码').fill('a-secure-password')
  await page.getByRole('button', { name: '创建账号' }).click()
  await expect(page).toHaveURL(/\/$/)
})

test('desktop: registration waiting state does not claim email was sent', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let data: unknown = { items: [] }
    let status = 200

    if (path === '/api/v1/auth/status') {
      data = {
        enabled: true,
        setup_required: false,
        registration_enabled: true,
        require_email_verification: true,
      }
    } else if (path === '/api/v1/auth/register' && request.method() === 'POST') {
      status = 201
      data = { verification_required: true, email: 'wait@example.com' }
    } else if (path === '/api/v1/auth/me') {
      return route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ code: 401, message: 'unauthorized' }),
      })
    } else if (path === '/api/v1/auth/organizations') {
      data = []
    }

    return route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify({ code: status, data, message: status < 400 ? 'success' : 'error' }),
    })
  })

  await page.goto('/register')
  await page.getByLabel('空间名称').fill('Wait Studio')
  await page.getByLabel('显示名称').fill('Waiter')
  await page.getByLabel('邮箱').fill('wait@example.com')
  await page.getByLabel('密码', { exact: true }).fill('a-secure-password')
  await page.getByLabel('确认密码').fill('a-secure-password')
  await page.getByRole('button', { name: '创建账号' }).click()
  await expect(page.getByRole('status')).toContainText('账号已创建')
  await expect(page.getByRole('status')).toContainText('wait@example.com')
  await expect(page.getByText('已发送邮件')).toHaveCount(0)
  await expect(page).toHaveURL(/\/register/)
})
