import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { DevicePoolImageInput } from '../api/generated/models'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { PoolsPage } from './PoolsPage'

function targetResponse(data: DevicePoolImageInput) {
  return HttpResponse.json({
    request_id: 'req_target',
    data: {
      pool_id: 'pool_000000000000001',
      image_id: 'image_00000000000001',
      ...data,
      created_at: '2026-08-06T00:00:00Z',
      updated_at: '2026-08-07T00:00:00Z',
    },
    error: null,
  })
}

async function openTargetEditor() {
  const user = userEvent.setup()
  renderWithProviders(<PoolsPage />)
  expect(await screen.findByText('default-android')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /配\s*置/ }))
  expect(await screen.findByRole('button', { name: /编\s*辑/ })).toBeInTheDocument()
  expect(screen.getByRole('spinbutton', { name: '最大并发' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: /编\s*辑/ }))
  expect(await screen.findByText('设置目标设备数')).toBeInTheDocument()
  return user
}

describe('PoolsPage fixed target', () => {
  it('maps one expansion target to min_ready and max_instances without requiring a reason', async () => {
    let submitted: DevicePoolImageInput | undefined
    server.use(http.put('/api/v1/device-pools/:id/images/:imageId', async ({ request }) => {
      submitted = await request.json() as DevicePoolImageInput
      return targetResponse(submitted)
    }))
    const user = await openTargetEditor()
    const target = screen.getByRole('spinbutton', { name: '目标设备数' })
    fireEvent.change(target, { target: { value: '3' } })
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))

    await waitFor(() => expect(submitted).toEqual({ min_ready: 3, max_instances: 3, enabled: true, reason: '' }))
  })

  it('requires a reason and a confirmation before shrinking', async () => {
    let submitted: DevicePoolImageInput | undefined
    server.use(http.put('/api/v1/device-pools/:id/images/:imageId', async ({ request }) => {
      submitted = await request.json() as DevicePoolImageInput
      return targetResponse(submitted)
    }))
    const user = await openTargetEditor()
    fireEvent.change(screen.getByRole('spinbutton', { name: '目标设备数' }), { target: { value: '1' } })
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('缩容时请填写至少 3 个字的调整原因')).toBeInTheDocument()
    expect(submitted).toBeUndefined()

    await user.type(screen.getByRole('textbox', { name: '调整原因（缩容时必填并写入审计）' }), '减少测试资源')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect((await screen.findAllByText('确认缩容到 1 台？')).length).toBeGreaterThan(0)
    expect(submitted).toBeUndefined()
    await user.click(screen.getByRole('button', { name: /确认缩容/ }))

    await waitFor(() => expect(submitted).toEqual({
      min_ready: 1,
      max_instances: 1,
      enabled: true,
      reason: '减少测试资源',
    }))
  })
})
