import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { ErrorBoundary } from './ErrorBoundary'

function Boom(): never {
  throw new Error('render exploded')
}

describe('ErrorBoundary', () => {
  it('shows the error in place of the page', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    renderWithProviders(
      <ErrorBoundary resetKey="/a">
        <Boom />
      </ErrorBoundary>,
    )
    expect(screen.getByText('这一页渲染时出了错。')).toBeInTheDocument()
    expect(screen.getByText('render exploded')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument()
    spy.mockRestore()
  })
})
