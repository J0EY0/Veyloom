import { deflateSync, crc32 } from 'node:zlib'
import { expect, test, type APIRequestContext } from '@playwright/test'

// Attachments as a person meets them (docs/webui.md 4.21): files sent in
// the chat drawn where they were sent, a picture by its smaller copy and a
// PDF by its first page (pdf.js, as built); the viewer stepping through
// them; the Attachments tab narrowing, packing some into a zip and going
// back to the message in the chat; and the search finding one.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

// png is a picture of the given size, a gradient, big enough for the hub
// to make a smaller copy of it.
function png(width: number, height: number): Buffer {
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4)
    length.writeUInt32BE(data.length)
    const body = Buffer.concat([Buffer.from(type, 'latin1'), data])
    const sum = Buffer.alloc(4)
    sum.writeUInt32BE(crc32(body))
    return Buffer.concat([length, body, sum])
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header.set([8, 2, 0, 0, 0], 8)
  const rows = Buffer.alloc((width * 3 + 1) * height)
  for (let y = 0; y < height; y++) {
    const at = y * (width * 3 + 1)
    for (let x = 0; x < width; x++) rows.set([(x * 255) / width, (y * 255) / height, 160], at + 1 + x * 3)
  }
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(rows)),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

// pdf is a PDF with a line of text on each of its pages.
function pdf(lines: string[]): Buffer {
  const objects: string[] = ['<< /Type /Catalog /Pages 2 0 R >>', '', '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
  const kids: string[] = []
  for (const line of lines) {
    const stream = `BT /F1 28 Tf 72 700 Td (${line}) Tj ET`
    objects.push(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`)
    objects.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents ${objects.length} 0 R /Resources << /Font << /F1 3 0 R >> >> >>`)
    kids.push(`${objects.length} 0 R`)
  }
  objects[1] = `<< /Type /Pages /Kids [${kids.join(' ')}] /Count ${lines.length} >>`
  let out = '%PDF-1.4\n'
  const offsets = objects.map((body, i) => {
    const at = out.length
    out += `${i + 1} 0 obj\n${body}\nendobj\n`
    return at
  })
  const xref = out.length
  out += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets.map((o) => `${String(o).padStart(10, '0')} 00000 n \n`).join('')}`
  out += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Buffer.from(out, 'latin1')
}

test('attachments in the chat, the viewer, the tab and the search', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  const { user } = await call<{ user: { id: string } }>(request, 'get', '/auth/status')
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', { name: `files ${stamp}`, repo_path: '', agent_ids: [] })
  const roomId = rooms[0].id

  const upload = async (name: string, mimeType: string, buffer: Buffer) => {
    const res = await request.post(`${api}/rooms/${roomId}/attachments`, { multipart: { file: { name, mimeType, buffer } } })
    expect(res.ok(), `upload ${name} -> ${res.status()}`).toBeTruthy()
    return ((await res.json()) as { attachment: { id: string; thumbnail?: boolean } }).attachment
  }
  const say = (body: string, ...ids: string[]) => call(request, 'post', `/rooms/${roomId}/messages`, { user_id: user.id, body, attachment_ids: ids })

  const code = Array.from({ length: 14 }, (_, i) => `line ${i + 1}`).join('\n') + '\n'
  const shot = await upload('wide-shot.png', 'image/png', png(1600, 1000))
  expect(shot.thumbnail).toBe(true)
  await say('截图在这。', shot.id)
  await say('测试文件在这。', (await upload('tags_test.go', 'text/plain', Buffer.from(code))).id)
  await say('需求文档。', (await upload('spec-v2.pdf', 'application/pdf', pdf(['Tags v2', 'Page two']))).id)
  const zip = await upload('release.zip', 'application/zip', Buffer.from('PK\u0003\u0004 not really a zip'))
  await say('打包好的。', zip.id)

  // In the chat: the picture by its smaller copy, the code's first lines,
  // the PDF's first page drawn by pdf.js, the zip as a card.
  await page.goto(`/rooms/${roomId}`)
  const picture = page.getByRole('button', { name: '预览 wide-shot.png' })
  await expect(picture.locator('img')).toHaveAttribute('src', `${api}/attachments/${shot.id}/thumbnail`)
  await expect(page.getByText(`14 行 · ${code.length} B`)).toBeVisible()
  await expect(page.getByRole('button', { name: '展开其余 5 行' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'spec-v2.pdf 的第一页' })).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText('2 页 · ')).toBeVisible()
  await expect(page.getByRole('link', { name: '下载 release.zip' })).toHaveAttribute('href', `${api}/attachments/${zip.id}`)

  // The viewer: the picture whole, then the newer ones to the right.
  await picture.click()
  const viewer = page.getByRole('dialog', { name: 'wide-shot.png' })
  await expect(viewer.getByText('1 / 4')).toBeVisible()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('dialog', { name: 'tags_test.go' }).getByText('line 14').filter({ visible: true })).toBeVisible()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('dialog', { name: 'spec-v2.pdf' }).getByRole('img', { name: '第 2 页' })).toBeVisible({ timeout: 15_000 })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)

  // The Attachments tab: every file as a card, today's under one heading.
  await page.getByRole('link', { name: '附件' }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}/attachments$`))
  const todays = page.getByRole('region', { name: '今天' })
  await expect(todays.getByText('4 个附件')).toBeVisible()
  await expect(todays.getByRole('article')).toHaveCount(4)

  // Documents only, then the words of a message.
  await page.getByRole('tab', { name: '文档' }).click()
  await expect(todays.getByRole('article')).toHaveCount(2)
  await page.getByRole('searchbox', { name: '搜索附件' }).fill('需求')
  await expect(todays.getByRole('article')).toHaveCount(1)
  await expect(todays.getByRole('article')).toContainText('spec-v2.pdf')
  await page.getByRole('tab', { name: '全部' }).click()
  await page.getByRole('searchbox', { name: '搜索附件' }).fill('')
  await expect(todays.getByRole('article')).toHaveCount(4)

  // Two picked download as one zip.
  await page.getByRole('button', { name: '选择', exact: true }).click()
  await page.getByRole('button', { name: '选择 wide-shot.png' }).click()
  await page.getByRole('button', { name: '选择 release.zip' }).click()
  await expect(page.getByRole('status').filter({ hasText: '已选 2 个' })).toBeVisible()
  const download = page.waitForEvent('download')
  await page.getByRole('link', { name: '打包下载' }).click()
  // Named for the day where the hub is, this machine: its date, not UTC's.
  const day = new Date()
  const today = [day.getFullYear(), day.getMonth() + 1, day.getDate()].map((n) => String(n).padStart(2, '0')).join('-')
  expect((await download).suggestedFilename()).toBe(`files ${stamp} ${today}.zip`)
  await page.getByRole('button', { name: '取消', exact: true }).click()

  // A card opens the viewer; from there, back to the message in the chat.
  await page.getByRole('button', { name: '预览 tags_test.go' }).click()
  await page.getByRole('dialog', { name: 'tags_test.go' }).getByRole('button', { name: '在聊天中查看' }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}$`))
  await expect(page.locator('li.bg-selection')).toContainText('测试文件在这。')

  // The search finds a file of the chat and opens it.
  await page.keyboard.press('ControlOrMeta+k')
  await page.getByPlaceholder('搜索项目、页面…').fill('release')
  await expect(page.getByText(`附件 · files ${stamp}`)).toBeVisible()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: 'release.zip' }).getByText('这种文件在这里看不了，下载后打开。')).toBeVisible()
})
