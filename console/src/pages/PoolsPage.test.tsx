import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { DevicePoolInput } from '../api/generated/models'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { PoolsPage } from './PoolsPage'

function poolResponse(data: DevicePoolInput) {
  return HttpResponse.json({
    request_id: 'req_target',
    data: {
      id: 'pool_000000000000001',
      ...data,
      status: 'active',
      created_at: '2026-08-06T00:00:00Z',
      updated_at: '2026-08-07T00:00:00Z',
    },
    error: null,
  })
}

async function openPoolEditor() {
  const user = userEvent.setup()
  renderWithProviders(<PoolsPage />)
  expect(await screen.findByText('default-android')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /配\s*置/ }))
  expect(await screen.findByRole('spinbutton', { name: '总目标数量' })).toHaveValue('2')
  expect(screen.getByRole('spinbutton', { name: '最小预热数量' })).toHaveValue('2')
  expect(screen.getByRole('spinbutton', { name: '最大并发' })).toHaveValue('2')
  return user
}

describe('PoolsPage pool capacity', () => {
  it('updates pool-wide total, warm and concurrency limits without requiring an expansion reason', async () => {
    let submitted: DevicePoolInput | undefined
    server.use(http.put('/api/v1/device-pools/:id', async ({ request }) => {
      submitted = await request.json() as DevicePoolInput
      return poolResponse(submitted)
    }))
    const user = await openPoolEditor()
    fireEvent.change(screen.getByRole('spinbutton', { name: '总目标数量' }), { target: { value: '3' } })
    fireEvent.change(screen.getByRole('spinbutton', { name: '最小预热数量' }), { target: { value: '3' } })
    fireEvent.change(screen.getByRole('spinbutton', { name: '最大并发' }), { target: { value: '3' } })
    await user.click(screen.getByRole('button', { name: '保存基本信息' }))

    await waitFor(() => expect(submitted).toMatchObject({
      name: 'default-android',
      total_target: 3,
      min_ready: 3,
      max_concurrency: 3,
      reason: '',
    }))
  }, 10_000)

  it('requires a reason and a confirmation before shrinking', async () => {
    let submitted: DevicePoolInput | undefined
    server.use(http.put('/api/v1/device-pools/:id', async ({ request }) => {
      submitted = await request.json() as DevicePoolInput
      return poolResponse(submitted)
    }))
    const user = await openPoolEditor()
    fireEvent.change(screen.getByRole('spinbutton', { name: '总目标数量' }), { target: { value: '1' } })
    fireEvent.change(screen.getByRole('spinbutton', { name: '最小预热数量' }), { target: { value: '1' } })
    fireEvent.change(screen.getByRole('spinbutton', { name: '最大并发' }), { target: { value: '1' } })
    await user.click(screen.getByRole('button', { name: '保存基本信息' }))
    expect(await screen.findByText('缩容时请填写至少 3 个字的调整原因')).toBeInTheDocument()
    expect(submitted).toBeUndefined()

    await user.type(screen.getByRole('textbox', { name: '调整原因（缩容时必填并写入审计）' }), '减少测试资源')
    await user.click(screen.getByRole('button', { name: '保存基本信息' }))
    expect((await screen.findAllByText('确认把设备池总目标缩容到 1 台？')).length).toBeGreaterThan(0)
    expect(submitted).toBeUndefined()
    await user.click(screen.getByRole('button', { name: /确认缩容/ }))

    await waitFor(() => expect(submitted).toMatchObject({
      total_target: 1,
      min_ready: 1,
      max_concurrency: 1,
      reason: '减少测试资源',
    }))
  }, 10_000)
})
