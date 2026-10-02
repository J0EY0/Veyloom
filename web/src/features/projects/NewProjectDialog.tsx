import { useId, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { SearchIcon, XIcon } from 'lucide-react'
import { Link, useNavigate } from 'react-router'
import { useAgents, useMachines } from '@/api/agents'
import { useCreateProject } from '@/api/projects'
import { useRuntimeTraits } from '@/api/runtimes'
import type { Agent } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Item, ItemContent, ItemDescription, ItemTitle } from '@/components/ui/item'
import { Spinner } from '@/components/ui/spinner'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { errorText } from '@/api/errorText'
import { byLeader } from './MaintainerFields'
import { keeperFor, NewProjectMaintainer } from './NewProjectMaintainer'

export interface NewProjectDialogProps {
  open: boolean
  onClose: () => void
}

// Creates a project the way a group chat is started: with the agents that
// will work in it, at least one, since an empty project does nothing. They
// join as members working in the project's local checkout. With no agent
// set up yet the dialog points to the Agents page instead.
export function NewProjectDialog({ open, onClose }: NewProjectDialogProps) {
  const navigate = useNavigate()
  const create = useCreateProject()
  const agents = useAgents()
  const nameRef = useRef<HTMLInputElement>(null)
  const [nameError, setNameError] = useState<string>()
  const [error, setError] = useState<string>()
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set())
  const [upkeep, setUpkeep] = useState(false)
  const [maintainer, setMaintainer] = useState(byLeader)
  const traits = useRuntimeTraits()
  const id = useId()
  const t = useT()
  // In the order they were picked, which is the order they join: the first
  // leads the project (docs/design.md 5.21).
  const joining = pickedAgents(agents.data, picked)
  // One taken off the list keeps the wiki no more, nor one who cannot
  // write it; with nobody who can, it is not kept.
  const keeper = keeperFor(joining, picked.has(maintainer) ? maintainer : byLeader, traits.data)

  function toggle(agentId: string, on: boolean) {
    setPicked((current) => {
      const next = new Set(current)
      if (on) next.add(agentId)
      else next.delete(agentId)
      return next
    })
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const name = String(data.get('name') ?? '').trim()
    if (name === '') {
      setNameError(t('project.nameRequired'))
      nameRef.current?.focus()
      return
    }
    setNameError(undefined)
    setError(undefined)
    create.mutate(
      {
        name,
        repo_path: String(data.get('repo_path') ?? '').trim(),
        agent_ids: joining.map((agent) => agent.id),
        ...(upkeep && keeper ? { wiki_upkeep: true, ...(keeper !== byLeader ? { wiki_maintainer_agent_id: keeper } : {}) } : {}),
      },
      {
        onSuccess: ({ rooms }) => {
          onClose()
          void navigate(`/rooms/${rooms[0].id}`)
        },
        onError: (err) => setError(errorText(err)),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto" aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('projects.new')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field data-invalid={nameError ? true : undefined}>
              <FieldLabel htmlFor={`${id}-name`}>{t('project.name')}</FieldLabel>
              <Input
                id={`${id}-name`}
                ref={nameRef}
                name="name"
                autoFocus
                autoComplete="off"
                spellCheck={false}
                placeholder="Veyloom"
                aria-invalid={nameError ? true : undefined}
              />
              {nameError ? <FieldError>{nameError}</FieldError> : null}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-path`}>{t('project.repoPath')}</FieldLabel>
              <Input id={`${id}-path`} name="repo_path" autoComplete="off" spellCheck={false} placeholder="/Users/me/project" />
            </Field>
            {/* A fieldset is as wide as its widest line by default; min-w-0 lets the long names truncate instead. */}
            <FieldSet className="min-w-0 gap-0">
              <FieldLegend variant="label" className="flex w-full items-baseline justify-between gap-3">
                {t('project.agents')}
                {picked.size > 0 ? <span className="text-xs font-normal text-subtle tabular-nums">{t('project.picked', { n: picked.size })}</span> : null}
              </FieldLegend>
              <AgentPicker id={id} agents={agents} picked={picked} onToggle={toggle} onLeave={onClose} />
            </FieldSet>
            {joining.length > 0 ? (
              <NewProjectMaintainer id={id} agents={joining} upkeep={upkeep} value={keeper} onUpkeep={setUpkeep} onChange={setMaintainer} />
            ) : null}
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={create.isPending || picked.size === 0} aria-busy={create.isPending}>
              {create.isPending ? t('project.creating') : t('project.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// pickedAgents are the agents picked, in the order they were.
function pickedAgents(agents: Agent[] | undefined, picked: ReadonlySet<string>): Agent[] {
  const byId = new Map((agents ?? []).map((agent) => [agent.id, agent]))
  return [...picked].flatMap((agentId) => byId.get(agentId) ?? [])
}

interface AgentPickerProps {
  id: string
  agents: ReturnType<typeof useAgents>
  picked: ReadonlySet<string>
  onToggle: (agentId: string, on: boolean) => void
  // Leaving for the Agents page closes the dialog.
  onLeave: () => void
}

// Picks the agents the way an IM starts a group chat. The search box holds
// the ones picked as chips, in the order they were picked; a chip click
// takes one out. Under it every agent is a row: a round check, its
// runtime's mark for an avatar, its name, and the machine and runtime it
// would run on. The search narrows the rows by any of those; Enter picks
// the first match and Backspace in an empty box drops the last chip. A
// machine that is not connected says so; the agent can still join and runs
// once the machine is back.
function AgentPicker({ id, agents, picked, onToggle, onLeave }: AgentPickerProps) {
  const machines = useMachines()
  const [query, setQuery] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)
  const t = useT()

  if (agents.isPending) {
    return (
      <p role="status" className="flex items-center gap-2 text-xs text-subtle">
        <Spinner className="size-3" />
        {t('common.loading')}
      </p>
    )
  }
  if (agents.isError) return <FieldError>{t('project.agentsFailed')}</FieldError>
  if (agents.data.length === 0) {
    return (
      <Empty className="gap-3 p-4 md:p-5">
        <EmptyHeader>
          <EmptyTitle className="text-[0.8125rem] font-normal tracking-normal text-subtle">{t('project.noAgents')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button asChild size="sm" variant="outline">
            <Link to="/agents" onClick={onLeave}>
              {t('project.newAgent')}
            </Link>
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const online = machines.data ? new Set(machines.data.map((machine) => machine.id)) : undefined
  const where = (agent: Agent) => (online && !online.has(agent.machine_id) ? t('agent.machineOffline', { name: agent.machine_name }) : agent.machine_name)
  const chips = pickedAgents(agents.data, picked)
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  const shown = agents.data.filter((agent) => {
    const text = `${agent.name} ${agent.machine_name} ${runtimeName(agent.runtime)}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })

  function onSearchKey(event: KeyboardEvent<HTMLInputElement>) {
    // Keys that confirm a word in an input method are not ours.
    if (event.nativeEvent.isComposing) return
    if (event.key === 'Enter') {
      // The box sits in the dialog's form: Enter here picks, it never creates.
      event.preventDefault()
      if (words.length === 0 || shown.length === 0) return
      if (!picked.has(shown[0].id)) onToggle(shown[0].id, true)
      setQuery('')
    } else if (event.key === 'Backspace' && query === '' && chips.length > 0) {
      onToggle(chips[chips.length - 1].id, false)
    }
  }

  return (
    <div className="overflow-hidden rounded-lg border has-[[data-agent-search]:focus-visible]:border-ring has-[[data-agent-search]:focus-visible]:ring-[3px] has-[[data-agent-search]:focus-visible]:ring-ring/50">
      {/* A click in the box's blank space goes to the search, as in a token field. */}
      <div className="flex cursor-text flex-wrap items-center gap-1.5 border-b px-2.5 py-1" onClick={() => searchRef.current?.focus()}>
        <SearchIcon aria-hidden="true" className="size-4 flex-none text-subtle" />
        {chips.map((agent, index) => (
          <Badge key={agent.id} asChild variant="secondary" className="h-6 max-w-48 cursor-pointer gap-1 pr-1.5 pl-1 font-normal hover:bg-secondary/80">
            <button type="button" aria-label={t('project.unpick', { name: agent.name })} onClick={() => onToggle(agent.id, false)}>
              <AgentAvatar look={agent} name={agent.name} size="xs" />
              <span className="min-w-0 truncate">{agent.name}</span>
              {/* The first picked joins first, and so leads (docs/design.md 5.21). */}
              {index === 0 ? <span className="flex-none text-[0.625rem] text-muted-foreground">{t('member.leader')}</span> : null}
              <XIcon className="text-subtle" />
            </button>
          </Badge>
        ))}
        <Input
          ref={searchRef}
          data-agent-search
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={onSearchKey}
          placeholder={chips.length > 0 ? t('common.search') : t('agents.search')}
          aria-label={t('agents.search')}
          autoComplete="off"
          spellCheck={false}
          className="h-7 min-w-12 flex-1 rounded-none border-0 bg-transparent px-1 shadow-none focus-visible:ring-0 dark:bg-transparent [&::-webkit-search-cancel-button]:appearance-none"
        />
      </div>
      <div className="max-h-64 overflow-y-auto p-1">
        {shown.length === 0 ? (
          <p className="px-2 py-6 text-center text-xs text-subtle">{t('agents.noMatch')}</p>
        ) : (
          shown.map((agent) => (
            <Item key={agent.id} asChild size="sm" className="cursor-pointer flex-nowrap gap-2.5 px-2 py-1.5 hover:bg-muted/60">
              <label htmlFor={`${id}-agent-${agent.id}`}>
                <Checkbox
                  id={`${id}-agent-${agent.id}`}
                  checked={picked.has(agent.id)}
                  onCheckedChange={(checked) => onToggle(agent.id, checked === true)}
                  className="rounded-full"
                />
                <AgentAvatar look={agent} name={agent.name} size="sm" />
                {/* One line: the name, then where it runs in the muted tone; both give way when long. */}
                <ItemContent className="min-w-0 flex-row items-baseline gap-2">
                  <ItemTitle className="block max-w-[60%] shrink-0 truncate text-[0.8125rem]">{agent.name}</ItemTitle>
                  <ItemDescription className="min-w-0 truncate text-xs text-nowrap text-subtle">
                    {where(agent)} · {runtimeName(agent.runtime)}
                  </ItemDescription>
                </ItemContent>
              </label>
            </Item>
          ))
        )}
      </div>
    </div>
  )
}
