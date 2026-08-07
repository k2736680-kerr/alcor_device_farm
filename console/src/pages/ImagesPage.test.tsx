import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ImagesPage } from './ImagesPage'
import { renderWithProviders } from '../test/renderWithProviders'

describe('ImagesPage', () => {
  it('renders the mocked image rows with the total count', async () => {
    renderWithProviders(<ImagesPage />)

    expect(await screen.findByText('android-14')).toBeInTheDocument()
    expect(screen.getByText('android-15')).toBeInTheDocument()
    expect(screen.getByText('共 2 条')).toBeInTheDocument()
    // 状态标签使用面向用户的中文，不直接暴露内部枚举值。
    expect(screen.getByText('可用')).toBeInTheDocument()
    expect(screen.getByText('验证失败')).toBeInTheDocument()
  })
})
