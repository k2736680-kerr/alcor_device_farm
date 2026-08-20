import { expect, test } from '@playwright/test'

test.describe('设备农场控制台 E2E', () => {
  test('登录 → 仪表盘 → 设备新增入口 → 退出', async ({ page }) => {
    // 1. SPA 加载出登录页
    await page.goto('/console/')
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()

    // 2. 错误密码被拒绝并给出提示
    await page.getByLabel('用户账号').fill('admin')
    await page.getByLabel('密码').fill('wrong-password')
    await page.getByRole('button', { name: /登\s*录/ }).click()
    await expect(page.getByText(/用户账号或密码错误/)).toBeVisible()

    // 3. 正确密码登录，进入控制台布局
    await page.getByLabel('密码').fill('admin-password')
    await page.getByRole('button', { name: /登\s*录/ }).click()
    await expect(page.getByText('控制台管理员')).toBeVisible()
    await expect(page.getByText('设备农场控制台')).toBeVisible()

    // 4. Android 系统列表只在设备新增流程中提供，不再占用独立导航页
    await expect(page.getByRole('link', { name: 'Android 镜像' })).toHaveCount(0)
    await page.getByRole('link', { name: '设备', exact: true }).first().click()
    await expect(page.getByRole('table')).toBeVisible()
    await page.getByRole('button', { name: '新增 Android 模拟器' }).click()
    await page.getByRole('button', { name: '下一步' }).click()
    await expect(page.getByLabel('Android 系统版本')).toBeVisible()
    await expect(page.getByText(/共 \d+ 条/)).toBeVisible()

    // 5. 退出登录回到登录页
    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })
})
