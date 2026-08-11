import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ImagesPage } from './ImagesPage'
import { renderWithProviders } from '../test/renderWithProviders'

describe('ImagesPage', () => {
  it('renders the mocked image rows with the total count', async () => {
    renderWithProviders(<ImagesPage role="admin" />)

    expect(await screen.findByText('android-14')).toBeInTheDocument()
    expect(screen.getByText('android-15')).toBeInTheDocument()
    expect(screen.getByText('共 2 条')).toBeInTheDocument()
    // 状态标签使用面向用户的中文，不直接暴露内部枚举值。
    expect(screen.getByText('可用')).toBeInTheDocument()
    expect(screen.getByText('验证失败')).toBeInTheDocument()
    expect(await screen.findByText('Android 官方系统目录')).toBeInTheDocument()
    expect(screen.getByText('Android 16 / API 36')).toBeInTheDocument()
    expect(screen.getByText('默认候选')).toBeInTheDocument()
    expect(screen.getByText('已缓存可用')).toBeInTheDocument()
    expect(screen.getAllByText('共享镜像层').length).toBeGreaterThan(0)
    expect(screen.getAllByText('设备数据卷').length).toBeGreaterThan(0)
    expect(screen.getByText('7.7 GiB')).toBeInTheDocument()
    expect(screen.getByText('4 GiB')).toBeInTheDocument()
  })

  it('hides catalogue mutations and image validation from viewers', async () => {
    renderWithProviders(<ImagesPage role="viewer" />)

    expect(await screen.findByText('Android 官方系统目录')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '同步官方目录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重新准备' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '验证' })).not.toBeInTheDocument()
  })
})
