import { tmpdir } from 'node:os'
import { expect, test, type APIRequestContext } from '@playwright/test'

// The whole loop, as a person sees it: ask an agent in the room, watch it
// work, grant the permission it asks for, read its report, find the @ in
// the inbox. The fake runtime plays the agent; everything else is real.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

interface Machine {
  id: string
}

test('ask, approve, read the report, find it in the inbox', async ({ page }) => {
  // page.request shares the browser's cookie jar, so signing in here signs
  // the page in too.
  const request = page.request
  const stamp = Date.now().toString(36)

  // Create the account on a fresh database, sign in on a reused one.
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  const { user } = await call<{ user: { id: string; name: string } }>(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)

  // The local machine registers once its runtime discovery finishes.
  await expect.poll(async () => (await call<{ machines: Machine[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 }).toBeGreaterThan(0)
  const { machines } = await call<{ machines: Machine[] }>(request, 'get', '/machines')

  // Set up on the local machine; as a member of the project it runs there
  // too. Its avatar is a picture uploaded first, which the agent names.
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=', 'base64')
  const uploaded = await request.post(`${api}/avatars`, { data: png, headers: { 'Content-Type': 'image/png' } })
  expect(uploaded.status()).toBe(201)
  const { avatar } = (await uploaded.json()) as { avatar: string }
  const careful = `Careful ${stamp}`
  await call(request, 'post', '/agents', {
    name: careful,
    avatar,
    machine_id: machines[0].id,
    runtime: 'fake',
    role_card: 'Ask first.',
    permission_preset: 'edit_with_approval',
    runtime_options: { approval: true, reply: '构建好了，服务起来了。' },
  })

  // Start the project the way a person does: a name, where the code is
  // checked out, and the agent that works in it. It opens on its chat.
  const project = { name: `e2e ${stamp}` }
  await page.goto('/inbox')
  await page.getByRole('button', { name: '新建项目' }).first().click()
  const dialog = page.getByRole('dialog', { name: '新建项目' })
  await dialog.getByLabel('名称').fill(project.name)
  await dialog.getByLabel('本地路径').fill(tmpdir())
  await dialog.getByRole('checkbox', { name: new RegExp(careful) }).check()
  await dialog.getByRole('button', { name: '创建项目' }).click()
  await expect(page).toHaveURL(/\/rooms\/[^/?]+$/)
  const roomId = new URL(page.url()).pathname.split('/').pop() ?? ''
  await expect(page).toHaveTitle(`${project.name} · Veyloom`)

  // The page is signed in as the account.
  await expect(page.getByRole('button', { name: `账号 ${user.name}` })).toBeVisible()

  // With one member the chat is a conversation with it: the box says a
  // message without an @ goes to it. The picker still offers it.
  await expect(page.getByText(`不带 @ 时由 ${careful} 回复`)).toBeVisible()
  const box = page.getByLabel('消息')
  await box.fill('@Ca')
  await expect(page.getByRole('listbox', { name: '提及成员' })).toContainText(careful)
  await page.keyboard.press('Enter')
  await expect(box).toHaveValue(`@${careful} `)
  await box.fill('')

  // Ask without an @, a file along: picked into the composer, uploaded on
  // send, shown under the message as a picture that opens in the viewer.
  await page.setInputFiles('input[type=file]', { name: 'shot.png', mimeType: 'image/png', buffer: png })
  await expect(page.getByText('shot.png')).toBeVisible()
  await box.fill('起个服务做冒烟')
  await page.keyboard.press('Enter')
  // In the chat; the sidebar lists the topic it opens by the same words.
  await expect(page.getByRole('main').getByText('起个服务做冒烟')).toBeVisible()
  const shot = page.getByRole('button', { name: '预览 shot.png' })
  await expect(shot).toBeVisible()
  const served = await request.get((await shot.locator('img').getAttribute('src')) ?? '')
  expect(served.ok()).toBeTruthy()
  expect(served.headers()['content-type']).toBe('image/png')

  // The agent asks permission: the island, the title and the footer say so.
  const island = page.getByRole('status', { name: '成员状态' })
  await expect(island).toContainText('在等你审批 · make test', { timeout: 20_000 })
  await expect(page).toHaveTitle(`(1) ${project.name} · Veyloom`)
  await expect(page.getByRole('button', { name: /在等你审批/ })).toBeVisible()

  // Allow it from the island; the turn finishes and reports back once, at
  // the head of its topic, addressed to the person with an @.
  await island.getByRole('button', { name: '允许' }).click()
  const answer = page.getByRole('listitem').filter({ hasText: '构建好了，服务起来了。' })
  await expect(answer).toHaveCount(1, { timeout: 20_000 })
  await expect(answer).toContainText(`${user.name} 构建好了，服务起来了。`)
  // Its reports lead with its picture, which only shows once the image has
  // loaded; the row of the person asking leads with an initial.
  const face = page.getByRole('listitem').filter({ hasText: '构建好了，服务起来了。' }).locator(`[data-avatar] img[src$="/avatars/${avatar}"]`)
  await expect(face.first()).toBeVisible()
  await expect(island).toContainText('空闲')
  await expect(page).toHaveTitle(`${project.name} · Veyloom`)

  // The topic is named by what the person asked and reads as a chat: the
  // agent under its name, the settled request and one line of activity.
  await page.getByRole('button', { name: /^已完成/ }).click()
  const topic = page.getByRole('complementary', { name: '话题' })
  await expect(topic.getByRole('heading', { name: '起个服务做冒烟' })).toBeVisible()
  await expect(topic).toContainText(careful)
  await expect(topic).toContainText('已运行')
  await expect(topic).toContainText('make test')
  await expect(topic).toContainText('1 次审批')
  await expect(page).toHaveURL(/thread=/)

  // Escape closes it; the inbox has the report, and reads its topic in place.
  await page.keyboard.press('Escape')
  await expect(topic).toHaveCount(0)
  await page.goto('/inbox')
  // Within the page itself: the sidebar has links naming the agent too.
  const forMe = page.getByRole('main')
  const report = forMe.getByRole('link').filter({ hasText: '构建好了，服务起来了。' }).first()
  await expect(report).toBeVisible()
  await expect(forMe.getByRole('link').filter({ hasText: careful }).first()).toContainText(project.name)
  await report.click()
  await expect(page).toHaveURL(/\/inbox\?item=/)
  const picked = forMe.getByRole('complementary', { name: '话题' })
  await expect(picked.getByRole('heading', { name: '起个服务做冒烟' })).toBeVisible()
  await expect(picked).toContainText('1 次审批')
  await expect(picked.getByRole('link', { name: '在群聊里打开' })).toHaveAttribute('href', new RegExp(`^/rooms/${roomId}\\?thread=`))
})
