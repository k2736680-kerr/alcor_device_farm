import { expect, test } from '@playwright/test'

test.describe('设备农场控制台 E2E', () => {
  test('登录 → 仪表盘 → 镜像列表 → 退出', async ({ page }) => {
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

    // 4. 进入镜像列表页，表格与分页信息渲染
    await page.getByRole('link', { name: 'Android 镜像' }).first().click()
    await expect(page.getByRole('table')).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '名称' })).toBeVisible()
    await expect(page.getByText(/共 \d+ 条/)).toBeVisible()

    // 5. 退出登录回到登录页
    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })
})
