import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { Attachment } from '@/api/types'
import { AttachmentList } from './attachment-list'

const image: Attachment = { id: 'at1', room_id: 'r1', filename: 'shot.png', media_type: 'image/png', size: 2048, created_at: '2026-09-14T02:00:00Z' }
const doc: Attachment = {
  id: 'at2',
  room_id: 'r1',
  filename: 'notes.pdf',
  media_type: 'application/pdf',
  size: 3 * 1024 * 1024,
  created_at: '2026-09-14T02:00:00Z',
}

describe('AttachmentList', () => {
  it('shows images as thumbnails that open, and files as chips that download', () => {
    render(<AttachmentList attachments={[image, doc]} />)
    const thumb = screen.getByRole('link', { name: '打开附件 shot.png' })
    expect(thumb).toHaveAttribute('href', '/api/v1/attachments/at1')
    expect(thumb).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('img', { name: 'shot.png' })).toHaveAttribute('src', '/api/v1/attachments/at1')

    const chip = screen.getByRole('link', { name: '打开附件 notes.pdf' })
    expect(chip).toHaveAttribute('download', 'notes.pdf')
    expect(chip).toHaveTextContent('notes.pdf')
    expect(chip).toHaveTextContent('3.0 MB')
  })

  it('renders nothing without attachments', () => {
    const { container } = render(<AttachmentList attachments={null} />)
    expect(container).toBeEmptyDOMElement()
  })
})
