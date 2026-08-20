import { createHash } from 'node:crypto'
import { mkdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test, type Locator, type Page } from '@playwright/test'

const enabled = process.env.DEVICE_FARM_DF031_E3 === '1'
const userID = process.env.DEVICE_FARM_E2E_USER_ID ?? 'admin'
const password = process.env.DEVICE_FARM_E2E_PASSWORD ?? ''
const evidenceDir = process.env.DEVICE_FARM_E3_EVIDENCE_DIR ?? ''
const hstsSeedURL = process.env.DEVICE_FARM_E3_HSTS_SEED_URL ?? ''
const expectedSTFOrigin = process.env.DEVICE_FARM_E3_EXPECT_STF_ORIGIN ?? ''
const sessionBootstrapOrigin = process.env.DEVICE_FARM_E3_SESSION_BOOTSTRAP_ORIGIN ?? ''

function evidencePath(testOutput: string, name: string): string {
  if (!evidenceDir) return testOutput
  mkdirSync(evidenceDir, { recursive: true })
  return path.join(evidenceDir, name)
}

async function login(page: Page) {
  if (!password) throw new Error('DEVICE_FARM_E2E_PASSWORD is required for DF-031 E3')
  if (hstsSeedURL) await page.goto(hstsSeedURL)
  await page.goto(sessionBootstrapOrigin ? `${sessionBootstrapOrigin}/console/` : '/console/')
  await page.getByLabel('用户账号').fill(userID)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page.getByRole('link', { name: '设备', exact: true }).first()).toBeVisible()
  if (sessionBootstrapOrigin) await page.goto('/console/')
}

async function waitForRemoteButton(page: Page): Promise<Locator> {
  const button = page.getByRole('button', { name: '远程连接' }).first()
  await expect.poll(async () => {
    await page.reload()
    await page.getByRole('link', { name: '设备', exact: true }).first().click()
    return button.isVisible()
  }, { timeout: 360_000, intervals: [2_000, 5_000] }).toBe(true)
  return button
}

function digest(buffer: Buffer): string {
  return createHash('sha256').update(buffer).digest('hex')
}

async function waitForScreenChange(screen: Locator, before: string) {
  await expect.poll(async () => digest(await screen.screenshot()), {
    timeout: 20_000,
    intervals: [1_000],
  }).not.toBe(before)
}

async function screenHasImage(screen: Locator): Promise<boolean> {
  return screen.evaluate((node) => {
    const canvas = node as HTMLCanvasElement
    const context = canvas.getContext('2d')
    if (!context || canvas.width === 0 || canvas.height === 0) return false
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data
    const colors = new Set<string>()
    const step = Math.max(4, Math.floor(pixels.length / 4000 / 4) * 4)
    for (let offset = 0; offset < pixels.length; offset += step) {
      colors.add(`${pixels[offset]}:${pixels[offset + 1]}:${pixels[offset + 2]}`)
      if (colors.size >= 12) return true
    }
    return false
  })
}

test.describe('DF-031 真实 STF 管理员远控', () => {
  test.describe.configure({ timeout: 600_000 })
  test.skip(!enabled, '设置 DEVICE_FARM_DF031_E3=1 后才执行真实 STF 浏览器远控验收')

  test('无感进入指定设备，完成点击、滑动、输入、Home、返回并挂断', async ({ page }, testInfo) => {
    await login(page)
    const remoteButton = await waitForRemoteButton(page)
    const rowText = (await remoteButton.locator('xpath=ancestor::tr').innerText()).replace(/\s+/g, ' ')
    const serial = rowText.match(/\b\d{1,3}(?:\.\d{1,3}){3}:\d+\b/)?.[0]
    expect(serial).toBeTruthy()
    await page.screenshot({ path: evidencePath(testInfo.outputPath('device-ready.png'), 'device-ready.png'), fullPage: true })

    const popupPromise = page.waitForEvent('popup')
    await remoteButton.click()
    const remote = await popupPromise
    await remote.waitForURL((url) => url.port === '7100' && !url.searchParams.has('jwt'), { timeout: 120_000 })
    if (expectedSTFOrigin) expect(new URL(remote.url()).origin).toBe(expectedSTFOrigin)
    await expect(remote).toHaveURL(new RegExp(`#!/control/${serial!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`))
    await expect(remote.getByText('Control', { exact: true }).first()).toBeVisible()

    const screen = remote.locator('canvas.screen')
    await expect(screen).toBeVisible({ timeout: 60_000 })
    await expect.poll(() => screenHasImage(screen), {
      timeout: 120_000,
      intervals: [2_000],
    }).toBe(true)
    await remote.screenshot({ path: evidencePath(testInfo.outputPath('stf-connected.png'), 'stf-connected.png'), fullPage: true })

    const box = await screen.boundingBox()
    expect(box).not.toBeNull()
    const home = await remote.locator('a[title="Home"]').boundingBox()
    const back = await remote.locator('a[title="Back"]').boundingBox()
    expect(home).not.toBeNull()
    expect(back).not.toBeNull()
    // Android Emulator may surface the transient "System UI isn't responding"
    // dialog after a rebuild. Clicking the stable "Wait" position is harmless
    // on the home screen and makes the following device click deterministic.
    await remote.mouse.click(box!.x + 70, box!.y + 335)
    await remote.waitForTimeout(2_000)
    let before = digest(await screen.screenshot())
    // Tap the emulator home-screen search field: unlike an app icon, this
    // remains at a stable location across the warm-pool rebuilds used here.
    await remote.mouse.click(box!.x + 130, box!.y + 542)
    await waitForScreenChange(screen, before)
    await remote.screenshot({ path: evidencePath(testInfo.outputPath('stf-click.png'), 'stf-click.png'), fullPage: true })

    before = digest(await screen.screenshot())
    await remote.mouse.click(home!.x + home!.width / 2, home!.y + home!.height / 2)
    await waitForScreenChange(screen, before)
    before = digest(await screen.screenshot())
    await remote.mouse.move(box!.x + box!.width / 2, box!.y + box!.height * 0.92)
    await remote.mouse.down()
    await remote.mouse.move(box!.x + box!.width / 2, box!.y + box!.height * 0.12, { steps: 20 })
    await remote.mouse.up()
    await waitForScreenChange(screen, before)
    await remote.screenshot({ path: evidencePath(testInfo.outputPath('stf-swipe.png'), 'stf-swipe.png'), fullPage: true })

    before = digest(await screen.screenshot())
    await remote.mouse.click(home!.x + home!.width / 2, home!.y + home!.height / 2)
    await waitForScreenChange(screen, before)
    before = digest(await screen.screenshot())
    await remote.mouse.click(box!.x + 130, box!.y + 542)
    await remote.keyboard.type('DF031 remote input', { delay: 40 })
    await waitForScreenChange(screen, before)
    await remote.screenshot({ path: evidencePath(testInfo.outputPath('stf-input.png'), 'stf-input.png'), fullPage: true })

    before = digest(await screen.screenshot())
    await remote.mouse.click(back!.x + back!.width / 2, back!.y + back!.height / 2)
    await waitForScreenChange(screen, before)
    before = digest(await screen.screenshot())
    await remote.mouse.click(home!.x + home!.width / 2, home!.y + home!.height / 2)
    await waitForScreenChange(screen, before)
    await remote.screenshot({ path: evidencePath(testInfo.outputPath('stf-home-back.png'), 'stf-home-back.png'), fullPage: true })

    await page.bringToFront()
    const hangUp = page.getByRole('button', { name: /挂\s*断/ }).first()
    await expect(hangUp).toBeVisible({ timeout: 20_000 })
    await hangUp.click()
    await expect(page.getByText('远控已挂断，设备正在清理并重建')).toBeVisible()
    await expect.poll(() => remote.isClosed(), { timeout: 30_000 }).toBe(true)
    await page.screenshot({ path: evidencePath(testInfo.outputPath('console-hung-up.png'), 'console-hung-up.png'), fullPage: true })
  })
})
