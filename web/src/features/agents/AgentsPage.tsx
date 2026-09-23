import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownWideNarrowIcon, BotIcon, ChevronDownIcon, PlusIcon, SearchIcon } from 'lucide-react'
import { useAgents } from '@/api/agents'
import type { Agent } from '@/api/types'
import { PageBody } from '@/components/layout/PageBody'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { Kbd } from '@/components/ui/kbd'
import { runtimeName } from '@/lib/runtimes'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { DeleteAgentDialog } from './DeleteAgentDialog'
import { AgentCard, AgentCardSkeleton } from './AgentCard'
import { AgentDialog } from './AgentDialog'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

type Order = 'added' | 'name' | 'recent'

// The Agents page (docs/webui.md §4.7): every agent as it is set up,
// before it joins a project. One card each — the whole card opens it in a
// dialog, its "…" menu edits or deletes it, the button in the toolbar
// starts a new one.
export function AgentsPage() {
  const t = useT()
  useDocumentTitle(t('agents.title'))
  const agents = useAgents()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Agent>()
  const [deleting, setDeleting] = useState<Agent>()
  const [query, setQuery] = useState('')
  const [runtime, setRuntime] = useState('')
  const [order, setOrder] = useState<Order>('added')
  const search = useRef<HTMLInputElement>(null)

  // ⌘F lands in the page's own search rather than the browser's: on a
  // page of cards, finding an agent is what you actually mean.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key === 'f') {
        event.preventDefault()
        search.current?.focus()
        search.current?.select()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const all = useMemo(() => agents.data ?? [], [agents.data])
  const runtimes = useMemo(() => [...new Set(all.map((item) => item.runtime))].sort((a, b) => runtimeName(a).localeCompare(runtimeName(b))), [all])
  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const ranked = all
      .filter((item) => (runtime ? item.runtime === runtime : true))
      .filter((item) => (needle ? `${item.name} ${item.model} ${item.role_card}`.toLowerCase().includes(needle) : true))
    if (order === 'name') return [...ranked].sort((a, b) => a.name.localeCompare(b.name))
    if (order === 'recent') return [...ranked].sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    return ranked
  }, [all, runtime, query, order])

  // With nothing in the list there is nothing to search or filter, so an
  // empty library keeps only the heading and the way to make the first one.
  const toolbar = (bare = false) => (
    <div className="flex flex-col gap-4 pb-2.5">
      <p className="text-[0.71875rem] text-subtle">{t('nav.manage')}</p>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-[1.75rem] font-bold tracking-[-0.03em] text-foreground">{t('agents.title')}</h2>
        {bare ? null : (
          <>
            <InputGroup className="h-8 w-49 min-w-0">
              <InputGroupAddon align="inline-start">
                <SearchIcon className="size-3.5 text-subtle" />
              </InputGroupAddon>
              <InputGroupInput
                ref={search}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                aria-label={t('agents.search')}
                placeholder={t('agents.search')}
                className="text-xs"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>
                  <Kbd>⌘F</Kbd>
                </InputGroupText>
              </InputGroupAddon>
            </InputGroup>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="font-normal">
                  {t('agents.runtimeIs', { runtime: runtime ? runtimeName(runtime) : t('agents.allRuntimes') })}
                  <ChevronDownIcon data-icon="inline-end" className="text-subtle" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuRadioGroup value={runtime} onValueChange={setRuntime}>
                  <DropdownMenuRadioItem value="">{t('agents.allRuntimes')}</DropdownMenuRadioItem>
                  {runtimes.map((name) => (
                    <DropdownMenuRadioItem key={name} value={name}>
                      {runtimeName(name)}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="font-normal">
                  <ArrowDownWideNarrowIcon data-icon="inline-start" className="text-subtle" />
                  {t('agents.sort')}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuRadioGroup value={order} onValueChange={(value) => setOrder(value as Order)}>
                  <DropdownMenuRadioItem value="added">{t('agents.sortAdded')}</DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="name">{t('agents.sortName')}</DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="recent">{t('agents.sortRecent')}</DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )}
        <Button size="sm" onClick={() => setCreating(true)}>
          <PlusIcon data-icon="inline-start" />
          {t('agents.new')}
        </Button>
      </div>
    </div>
  )

  return (
    <Panel>
      <PanelHeader title={t('agents.title')} />
      {agents.isPending ? (
        <PageBody>
          {toolbar()}
          <Grid>
            {[0, 1, 2, 3].map((n) => (
              <AgentCardSkeleton key={n} label={n === 0 ? t('common.loading') : undefined} />
            ))}
          </Grid>
        </PageBody>
      ) : agents.isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('agents.failed')}</EmptyTitle>
            <EmptyDescription>{errorText(agents.error)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : all.length === 0 ? (
        <PageBody fill>
          {toolbar(true)}
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BotIcon />
              </EmptyMedia>
              <EmptyTitle>{t('agents.empty')}</EmptyTitle>
            </EmptyHeader>
            <EmptyContent>
              <Button size="sm" onClick={() => setCreating(true)}>
                <PlusIcon data-icon="inline-start" />
                {t('agents.new')}
              </Button>
            </EmptyContent>
          </Empty>
        </PageBody>
      ) : (
        <PageBody fill={shown.length === 0}>
          {toolbar()}
          {shown.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>{t('agents.noMatch')}</EmptyTitle>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setQuery('')
                    setRuntime('')
                  }}
                >
                  {t('agents.clearFilters')}
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Grid>
              {shown.map((agent) => (
                <li key={agent.id}>
                  <AgentCard agent={agent} onOpen={setEditing} onDelete={setDeleting} />
                </li>
              ))}
            </Grid>
          )}
        </PageBody>
      )}
      {creating ? <AgentDialog onClose={() => setCreating(false)} /> : null}
      {editing ? <AgentDialog agent={editing} onClose={() => setEditing(undefined)} /> : null}
      {deleting ? <DeleteAgentDialog agent={deleting} onClose={() => setDeleting(undefined)} /> : null}
    </Panel>
  )
}

// Four across on a wide screen, one on a phone; cards in a row share a
// height so the footers line up.
function Grid({ children }: { children: React.ReactNode }) {
  return <ul className="grid grid-cols-[repeat(auto-fill,minmax(264px,1fr))] gap-3">{children}</ul>
}
