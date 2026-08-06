import { expect, test } from '@playwright/test'

test.describe('设备农场控制台预约流程 E2E', () => {
  test('创建人工预约 → 等待 active → 续租 → 释放 → 审计留痕', async ({ page }) => {
    // 登录
    await page.goto('/console/')
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
    await page.getByLabel('用户 ID').fill('admin')
    await page.getByLabel('密码').fill('admin-password')
    await page.getByRole('button', { name: /登\s*录/ }).click()
    await expect(page.getByText('控制台管理员')).toBeVisible()

    // 进入预约页
    await page.getByRole('link', { name: '预约' }).first().click()
    await expect(page.getByRole('button', { name: '创建人工预约' })).toBeVisible()

    // 创建人工预约
    await page.getByRole('button', { name: '创建人工预约' }).click()
    await page.locator('.ant-modal').getByLabel('设备池').click()
    await page.locator('.ant-select-dropdown').getByText(/e2e-pool/).click()
    await expect(page.getByLabel('预约所有者')).toBeDisabled()
    await page.locator('.ant-modal').getByRole('button', { name: /创\s*建/ }).click()
    await expect(page.getByText(/预约已创建.*request_id: req_/)).toBeVisible()

    // 列表出现 pending，并在轮询下变为 active（调度器 provider-free 分配预置设备）
    await expect(page.getByRole('row', { name: /active/ }).first()).toBeVisible({ timeout: 20_000 })

    // 刷新后必须重新读取 Server 真相，不能依靠浏览器内的乐观状态。
    await page.reload()
    const activeRow = page.getByRole('row', { name: /active/ }).first()
    await expect(activeRow).toBeVisible()
    await expect(activeRow).toContainText('admin')

    // remoteConnect 返回的是 TCP ADB 地址，不是浏览器页面；控制台不得把它伪装成 Web 远控入口。
    await expect(activeRow.getByRole('button', { name: 'STF 远控' })).toHaveCount(0)

    // 单设备占用期间第二条预约只能保持 pending，不能突破容量
    await page.getByRole('button', { name: '创建人工预约' }).click()
    await page.locator('.ant-modal').getByLabel('设备池').click()
    await page.locator('.ant-select-dropdown').getByText(/e2e-pool/).click()
    await page.locator('.ant-modal').getByRole('button', { name: /创\s*建/ }).click()
    const pendingRow = page.getByRole('row', { name: /pending/ }).first()
    await expect(pendingRow).toBeVisible()
    await expect(pendingRow.getByRole('button', { name: /续\s*租/ })).toHaveCount(0)
    await pendingRow.getByRole('button', { name: /取\s*消/ }).click()
    await page.getByLabel('操作原因（必填，将写入审计）').fill('e2e 单设备容量验证后取消')
    await page.getByRole('button', { name: '确认执行' }).click()
    await expect(page.getByText(/预约已取消.*request_id: req_/)).toBeVisible()

    // 续租
    await activeRow.getByRole('button', { name: /续\s*租/ }).click()
    await page.getByLabel('续租时长(s)').fill('600')
    await page.locator('.ant-modal').getByRole('button', { name: /续\s*租/ }).click()
    await expect(page.getByText(/租期已续/)).toBeVisible()

    // 释放
    await page.getByRole('row', { name: /active/ }).first().getByRole('button', { name: /释\s*放/ }).click()
    await page.getByLabel('操作原因（必填，将写入审计）').fill('e2e 验证完成')
    await page.getByRole('button', { name: '确认执行' }).click()
    await expect(page.getByText(/预约已释放.*request_id: req_/)).toBeVisible()

    // 审计留痕
    await page.getByRole('link', { name: '审计' }).first().click()
    await expect(page.getByRole('table')).toBeVisible()
    await expect(page.getByText('release_device_reservation').first()).toBeVisible()
    await expect(page.getByText('cancel_pending_device_reservation').first()).toBeVisible()
    await expect(page.getByText('e2e 验证完成').first()).toBeVisible()

    // 验收结束主动撤销浏览器会话，不把技术会话留给清理任务兜底。
    await page.getByRole('button', { name: /退出/ }).click()
    await expect(page.getByText('设备农场控制台登录')).toBeVisible()
  })
})
