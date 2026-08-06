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
    // status tags reflect the image status
    expect(screen.getByText('ready')).toBeInTheDocument()
    expect(screen.getByText('failed')).toBeInTheDocument()
  })
})
