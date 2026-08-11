import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ImagesPage } from './ImagesPage'
import { renderWithProviders } from '../test/renderWithProviders'

describe('ImagesPage', () => {
  it('shows only verified usable images by default', async () => {
    renderWithProviders(<ImagesPage role="admin" />)

    expect(await screen.findByText('android-14')).toBeInTheDocument()
    expect(screen.queryByText('android-15')).not.toBeInTheDocument()
    expect(screen.queryByText('android-13-old')).not.toBeInTheDocument()
    expect(screen.getByText('共 1 条')).toBeInTheDocument()
    // 状态标签使用面向用户的中文，不直接暴露内部枚举值。
    expect(screen.getByText('可用')).toBeInTheDocument()
    expect(screen.getByText('default-android 默认')).toBeInTheDocument()
    expect(await screen.findByText('Android 官方系统目录')).toBeInTheDocument()
    expect(screen.getByText('Android 16 / API 36')).toBeInTheDocument()
    expect(screen.getByText('默认候选')).toBeInTheDocument()
    expect(screen.getByText('已缓存可用')).toBeInTheDocument()
    expect(screen.getAllByText('共享镜像层').length).toBeGreaterThan(0)
    expect(screen.getAllByText('设备数据卷').length).toBeGreaterThan(0)
    expect(screen.getByText('7.7 GiB')).toBeInTheDocument()
    expect(screen.getByText('4 GiB')).toBeInTheDocument()
  }, 10_000)

  it('selects a ready image for a pool and exposes retired images separately', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ImagesPage role="admin" />)

    expect(await screen.findByText('android-14')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '选择使用' }))
    expect(await screen.findByText(/不会重装或清空当前已有设备/)).toBeInTheDocument()
    await user.click(screen.getByRole('combobox', { name: '设备池' }))
    await user.click(await screen.findByText('default-android（当前默认）'))
    await user.type(screen.getByRole('textbox', { name: '选择原因（写入审计）' }), '后续设备使用该镜像')
    await user.click(screen.getByRole('button', { name: '设为默认镜像' }))
    expect(await screen.findByText(/已设为设备池默认镜像/)).toBeInTheDocument()

    await user.click(screen.getByText('已停用归档'))
    expect(await screen.findByText('android-13-old')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择使用' })).not.toBeInTheDocument()
  }, 10_000)

  it('requires an audited reason before retiring an image', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ImagesPage role="admin" />)

    expect(await screen.findByText('android-14')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /停\s*用/ }))
    expect(await screen.findByText(/历史设备和审计记录仍会保留/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /确认停用/ }))
    expect(await screen.findByText('请填写停用原因')).toBeInTheDocument()
  }, 10_000)

  it('hides catalogue mutations and image validation from viewers', async () => {
    renderWithProviders(<ImagesPage role="viewer" />)

    expect(await screen.findByText('Android 官方系统目录')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '同步官方目录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重新准备' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '验证' })).not.toBeInTheDocument()
  }, 10_000)
})
