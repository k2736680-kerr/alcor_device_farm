import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import path from 'node:path'

const userID = process.env.DEVICE_FARM_E2E_USER_ID ?? 'admin'
const password = process.env.DEVICE_FARM_E2E_PASSWORD ?? 'admin-password'
const e3Enabled = process.env.DEVICE_FARM_E3 === '1'
const e3DeviceID = process.env.DEVICE_FARM_E3_DEVICE_ID ?? ''
const e3EvidenceDir = process.env.DEVICE_FARM_E3_EVIDENCE_DIR ?? ''

function shortID(value: string): string {
  return value.length > 20 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value
}

function evidencePath(defaultPath: string, name: string): string {
  if (!e3EvidenceDir) {
    return defaultPath
  }
  mkdirSync(e3EvidenceDir, { recursive: true })
  return path.join(e3EvidenceDir, name)
}

async function login(page: Page) {
  await page.goto('/console/')
  await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  await page.getByLabel('用户账号').fill(userID)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page.getByRole('link', { name: '设备', exact: true }).first()).toBeVisible()
  await expect(page.getByRole('link', { name: 'Android 镜像' })).toHaveCount(0)
}

test.describe('DF-028 Web 安全与部署基线', () => {
  test('未认证、CSRF 缺省和撤销会话均被拒绝，API 不缓存', async ({ page, playwright }) => {
    const anon = await playwright.request.newContext({ baseURL: test.info().project.use.baseURL as string })
    const unauth = await anon.get('/api/v1/device-images')
    expect(unauth.status()).toBe(401)
    expect(unauth.headers()['cache-control']).toBe('no-store')
    expect(unauth.headers().pragma).toBe('no-cache')
    const unauthBody = (await unauth.json()) as { error?: { code?: string } }
    expect(unauthBody.error?.code).toBe('UNAUTHORIZED')
    await anon.dispose()

    await page.goto('/console/')
    await page.getByLabel('用户账号').fill(userID)
    await page.getByLabel('密码').fill(`${password}-wrong`)
    await page.getByRole('button', { name: /登\s*录/ }).click()
    await expect(page.getByText(/用户账号或密码错误/)).toBeVisible()

    await login(page)

    // 使用真实写接口验证 CSRF；资源不存在也必须先被 CSRF 中间件拒绝。
    const noCSRF = await page.request.post('/api/v1/devices/df028-csrf-probe/quarantines', {
      data: { reason: 'DF-028 CSRF probe' },
    })
    expect(noCSRF.status()).toBe(403)
    const csrfBody = (await noCSRF.json()) as { error?: { code?: string } }
    expect(csrfBody.error?.code).toBe('CSRF_VALIDATION_FAILED')
    expect(noCSRF.headers()['cache-control']).toBe('no-store')

    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
    const revoked = await page.request.get('/console/api/v1/me')
    expect(revoked.status()).toBe(401)
  })

  test('Console 静态响应使用安全头、入口防缓存和哈希资源长缓存', async ({ request }) => {
    const entry = await request.get('/console/')
    expect(entry.status()).toBe(200)
    expect(entry.headers()['cache-control']).toBe('no-store')
    expect(entry.headers()['content-security-policy']).toContain("default-src 'self'")
    expect(entry.headers()['content-security-policy']).toContain("frame-ancestors 'none'")
    expect(entry.headers()['x-frame-options']).toBe('DENY')
    expect(entry.headers()['x-content-type-options']).toBe('nosniff')
    expect(entry.headers()['referrer-policy']).toBe('no-referrer')

    const html = await entry.text()
    const asset = html.match(/assets\/index-[^"' ]+\.js/)?.[0]
    expect(asset).toBeTruthy()
    const assetResponse = await request.get(`/console/${asset}`)
    expect(assetResponse.status()).toBe(200)
    expect(assetResponse.headers()['cache-control']).toBe('public, max-age=31536000, immutable')

    const bundle = await assetResponse.text()
    expect(`${html}\n${bundle}`).not.toMatch(
      /DEVICE_FARM_(?:SECURITY_(?:SERVICE|AGENT)_TOKEN|STF_API_TOKEN)|postgres(?:ql)?:\/\/|Bearer\s+[A-Za-z0-9._~-]{20,}/,
    )
  })
})

test.describe('DF-028 E3 真实设备 Web 验收', () => {
  test.describe.configure({ timeout: 600_000 })
  test.skip(!e3Enabled, '设置 DEVICE_FARM_E3=1 后才执行真实 Linux/STF/Appium 验收')

  test('真实登录并展示设备运行总览', async ({ page }, testInfo) => {
    await page.goto('/console/')
    await page.screenshot({ path: evidencePath(testInfo.outputPath('login.png'), 'login.png'), fullPage: true })
    await login(page)
    await page.screenshot({ path: evidencePath(testInfo.outputPath('dashboard.png'), 'dashboard.png'), fullPage: true })
    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })

  test('浏览真实资源并完成人工预约、续租、释放和审计', async ({ page }, testInfo) => {
    await login(page)
    await page.getByRole('link', { name: '预约' }).first().click()
    await page.getByRole('button', { name: '创建人工预约' }).click()
    await page.locator('.ant-modal').getByLabel('设备池').click()
    await page.locator('.ant-select-dropdown .ant-select-item-option:not(.ant-select-item-option-disabled)').first().click()
    await page.locator('.ant-modal').getByRole('button', { name: /创\s*建/ }).click()
    await expect(page.getByText(/预约已创建.*请求编号：req_/)).toBeVisible()

    const activeRow = page.getByRole('row', { name: /active/ }).first()
    await expect(activeRow).toBeVisible({ timeout: 30_000 })
    await expect(activeRow).toContainText(userID)
    await expect(activeRow.getByRole('button', { name: 'STF 远控' })).toHaveCount(0)
    await page.screenshot({ path: evidencePath(testInfo.outputPath('reservation-active.png'), 'reservation-active.png'), fullPage: true })

    await activeRow.getByRole('button', { name: /续\s*租/ }).click()
    await page.getByLabel('续租时长（秒）').fill('600')
    await page.locator('.ant-modal').getByRole('button', { name: /续\s*租/ }).click()
    await expect(page.getByText(/租期窗口已延长.*请求编号：req_/)).toBeVisible()

    await page.getByRole('row', { name: /active/ }).first().getByRole('button', { name: /释\s*放/ }).click()
    await page.getByLabel('操作原因（必填，将写入审计）').fill('DF-028 真实浏览器预约释放验收')
    await page.getByRole('button', { name: '确认执行' }).click()
    await expect(page.getByText(/预约已释放.*请求编号：req_/)).toBeVisible()

    await page.getByRole('link', { name: '审计' }).first().click()
    await expect(page.getByText('DF-028 真实浏览器预约释放验收').first()).toBeVisible()
    await page.screenshot({ path: evidencePath(testInfo.outputPath('audit.png'), 'audit.png'), fullPage: true })

    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })

  test('隔离/解除隔离真实 Android 设备并从 Server 状态收敛', async ({ page }, testInfo) => {
    expect(e3DeviceID, 'DEVICE_FARM_E3_DEVICE_ID is required').not.toBe('')
    const deviceLabel = shortID(e3DeviceID)
    await login(page)
    await page.getByRole('link', { name: '设备', exact: true }).first().click()

    const deviceRow = page.getByRole('row', { name: new RegExp(deviceLabel) }).first()
    await expect.poll(async () => {
      await page.reload()
      return page.getByRole('row', { name: new RegExp(deviceLabel) }).first().innerText()
    }, { timeout: 360_000 }).toContain('ready')

    await deviceRow.getByRole('button', { name: /更\s*多/ }).click()
    await page.getByRole('menuitem', { name: /隔\s*离/ }).click()
    await page.getByLabel('操作原因（必填，将写入审计）').fill('DF-028 真实环境隔离验证')
    await page.getByRole('button', { name: '确认执行' }).click()
    await expect(page.getByText(/操作已受理.*请求编号：req_/)).toBeVisible()

    await page.reload()
    const quarantinedRow = page.getByRole('row', { name: new RegExp(deviceLabel) }).first()
    await expect(quarantinedRow).toContainText('quarantined')
    await page.screenshot({ path: evidencePath(testInfo.outputPath('device-quarantined.png'), 'device-quarantined.png'), fullPage: true })

    await quarantinedRow.getByRole('button', { name: /更\s*多/ }).click()
    await page.getByRole('menuitem', { name: /解除\s*隔离/ }).click()
    await page.getByLabel('操作原因（必填，将写入审计）').fill('DF-028 真实环境恢复验证')
    await page.getByRole('button', { name: '确认执行' }).click()
    await expect(page.getByText(/操作已受理.*请求编号：req_/)).toBeVisible()

    await expect.poll(async () => {
      await page.reload()
      return page.getByRole('row', { name: new RegExp(deviceLabel) }).first().innerText()
    }, { timeout: 360_000 }).toContain('ready')

    await page.screenshot({ path: evidencePath(testInfo.outputPath('device-ready.png'), 'device-ready.png'), fullPage: true })

    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })
})
