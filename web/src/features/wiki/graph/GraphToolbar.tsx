import { useId, useMemo, useState } from 'react'
import { ChevronLeftIcon, SearchIcon, SlidersHorizontalIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { GraphEdgeKind, GraphNode, WikiGraph } from '@/api/types'
import type { WikiSpace } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { MessageKey } from '@/i18n/zh-CN'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { pageHref } from '../links'
import { typeName } from '../names'
import { KindIcon, label } from './FocusCard'
import { defaultFilters, type FileNodes, type GraphFilters } from './model'

// The graph's tools, over its top left corner (docs/design.md 5.17):
// finding a node by title or path, and the filters. In a narrow page, a
// way back to the list of pages. They come before the canvas, so the
// keyboard reaches them first.

export interface GraphToolbarProps {
  space: WikiSpace
  // The whole graph, for the filters to offer what it holds.
  graph: WikiGraph
  // What is on screen, to find in, and the paths named in each directory.
  shown: GraphNode[]
  paths: Map<string, string[]>
  filters: GraphFilters
  onFilters: (filters: GraphFilters) => void
  onFind: (id: string) => void
}

export function GraphToolbar({ space, graph, shown, paths, filters, onFilters, onFind }: GraphToolbarProps) {
  const t = useT()
  return (
    <div className="flex items-center gap-1.5">
      <Button asChild variant="outline" size="icon-sm" className="@split/panel:hidden">
        <Link to={pageHref(space)} aria-label={t('wiki.pages')}>
          <ChevronLeftIcon />
        </Link>
      </Button>
      <FindNode nodes={shown} paths={paths} onFind={onFind} />
      <FiltersButton graph={graph} filters={filters} onFilters={onFilters} />
    </div>
  )
}

// The list shows the first so many nodes that match: a big wiki has
// hundreds, and a few letters narrow them down.
const findCap = 50

function FindNode({ nodes, paths, onFind }: { nodes: GraphNode[]; paths: Map<string, string[]>; onFind: (id: string) => void }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  // A directory is found by the paths in it too.
  const matches = useMemo(() => {
    const words = query.trim().toLowerCase()
    const found = words
      ? nodes.filter((node) =>
          [label(t, node), node.page?.path ?? '', ...(paths.get(node.id) ?? []).map((path) => `${node.file ?? ''}${path}`)].some((text) =>
            text.toLowerCase().includes(words),
          ),
        )
      : nodes
    return found.slice(0, findCap)
  }, [nodes, paths, query, t])
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setQuery('')
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="bg-card">
          <SearchIcon />
          {t('wiki.graph.find')}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(20rem,calc(100vw-2rem))] p-0">
        <Command shouldFilter={false}>
          <CommandInput placeholder={t('wiki.graph.findPlaceholder')} value={query} onValueChange={setQuery} />
          <CommandList>
            <CommandEmpty>{t('wiki.graph.noMatch')}</CommandEmpty>
            {matches.map((node) => (
              <CommandItem
                key={node.id}
                value={node.id}
                onSelect={() => {
                  setOpen(false)
                  setQuery('')
                  onFind(node.id)
                }}
                className="items-start"
              >
                <KindIcon node={node} className="mt-0.5" />
                <span className="min-w-0 flex-1">
                  <span className={cn('block break-words', node.file && 'font-mono text-xs break-all')} translate={node.file ? 'no' : undefined}>
                    {label(t, node)}
                  </span>
                  {node.page || paths.has(node.id) ? (
                    <span className="line-clamp-2 block font-mono text-[0.6875rem] break-all text-subtle" translate="no">
                      {node.page ? node.page.path : paths.get(node.id)?.join(', ')}
                    </span>
                  ) : null}
                </span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

// The kinds of relation a person can leave out. They are drawn alike; the
// card says which is which.
const kinds: { kind: GraphEdgeKind; key: MessageKey }[] = [
  { kind: 'link', key: 'wiki.graph.kind.link' },
  { kind: 'supersedes', key: 'wiki.graph.kind.supersedes' },
  { kind: 'source', key: 'wiki.graph.kind.source' },
]

const fileChoices: { value: FileNodes; key: MessageKey }[] = [
  { value: 'shared', key: 'wiki.graph.files.shared' },
  { value: 'all', key: 'wiki.graph.files.all' },
  { value: 'none', key: 'wiki.graph.files.none' },
]

// The wiki's own types first, in its order; any other after, by name.
const typeOrder = ['Decision', 'Convention', 'Fact', 'Pitfall', 'Module', 'Topic', 'Pattern', 'Skill']

function FiltersButton({ graph, filters, onFilters }: { graph: WikiGraph; filters: GraphFilters; onFilters: (filters: GraphFilters) => void }) {
  const t = useT()
  const id = useId()
  const has = (kind: GraphNode['kind']) => graph.nodes.some((node) => node.kind === kind)
  const types = [...new Set(graph.nodes.flatMap((node) => (node.page ? [node.page.type] : [])))].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
  const toggle = <V,>(list: V[], value: V, on: boolean) => (on ? list.filter((known) => known !== value) : [...list, value])
  const set = (patch: Partial<GraphFilters>) => onFilters({ ...filters, ...patch })
  const changed = JSON.stringify(filters) !== JSON.stringify(defaultFilters)
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="bg-card">
          <SlidersHorizontalIcon />
          {t('wiki.graph.filters')}
          {changed ? (
            <>
              <span aria-hidden="true" className="size-1.5 rounded-full bg-foreground" />
              <span className="sr-only">{t('wiki.graph.filtersChanged')}</span>
            </>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="flex max-h-[min(36rem,calc(100vh-10rem))] w-[min(18rem,calc(100vw-2rem))] flex-col gap-4 overflow-y-auto">
        {types.length > 0 ? (
          <FieldSet className="gap-2">
            <FieldLegend variant="label" className="mb-0 text-xs text-subtle">
              {t('wiki.graph.types')}
            </FieldLegend>
            {types.map((type) => (
              <Field key={type} orientation="horizontal" className="gap-2">
                <Checkbox
                  id={`${id}-type-${type}`}
                  checked={!filters.hiddenTypes.includes(type)}
                  onCheckedChange={(checked) => set({ hiddenTypes: toggle(filters.hiddenTypes, type, checked === true) })}
                />
                <FieldLabel htmlFor={`${id}-type-${type}`} className="text-[0.8125rem] font-normal">
                  {typeName(t, type)}
                </FieldLabel>
              </Field>
            ))}
          </FieldSet>
        ) : null}
        <FieldSet className="gap-2">
          <FieldLegend variant="label" className="mb-0 text-xs text-subtle">
            {t('wiki.graph.kinds')}
          </FieldLegend>
          {kinds.map(({ kind, key }) => (
            <Field key={kind} orientation="horizontal" className="gap-2">
              <Checkbox
                id={`${id}-kind-${kind}`}
                checked={!filters.hiddenKinds.includes(kind)}
                onCheckedChange={(checked) => set({ hiddenKinds: toggle(filters.hiddenKinds, kind, checked === true) })}
              />
              <FieldLabel htmlFor={`${id}-kind-${kind}`} className="flex-1 text-[0.8125rem] font-normal">
                {t(key)}
              </FieldLabel>
            </Field>
          ))}
        </FieldSet>
        <FieldSet className="gap-2.5">
          <FieldLegend variant="label" className="mb-0 text-xs text-subtle">
            {t('wiki.graph.nodes')}
          </FieldLegend>
          <Toggle id={`${id}-deprecated`} label={t('wiki.graph.deprecated')} checked={filters.deprecated} onChange={(deprecated) => set({ deprecated })} />
          {has('external') ? (
            <Toggle id={`${id}-external`} label={t('wiki.graph.external')} checked={filters.external} onChange={(external) => set({ external })} />
          ) : null}
          {has('topic') ? <Toggle id={`${id}-topics`} label={t('wiki.graph.topics')} checked={filters.topics} onChange={(topics) => set({ topics })} /> : null}
          {has('file') ? (
            <Field className="gap-1.5">
              <FieldLabel htmlFor={`${id}-files`} className="text-[0.8125rem] font-normal">
                {t('wiki.graph.files')}
              </FieldLabel>
              <Select value={filters.files} onValueChange={(files) => set({ files: files as FileNodes })}>
                <SelectTrigger id={`${id}-files`} size="sm" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {fileChoices.map((choice) => (
                    <SelectItem key={choice.value} value={choice.value}>
                      {t(choice.key)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          <Field orientation="horizontal" className="gap-2">
            <FieldLabel htmlFor={`${id}-depth`} className="flex-1 text-[0.8125rem] font-normal">
              {t('wiki.graph.depth')}
            </FieldLabel>
            <Select value={String(filters.depth)} onValueChange={(depth) => set({ depth: depth === '2' ? 2 : 1 })}>
              <SelectTrigger id={`${id}-depth`} size="sm" className="w-auto">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="1">{t('wiki.graph.depth.1')}</SelectItem>
                <SelectItem value="2">{t('wiki.graph.depth.2')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </FieldSet>
        <Button variant="ghost" size="sm" className="self-start" disabled={!changed} onClick={() => onFilters(defaultFilters)}>
          {t('wiki.graph.reset')}
        </Button>
      </PopoverContent>
    </Popover>
  )
}

function Toggle({ id, label, checked, onChange }: { id: string; label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <Field orientation="horizontal" className="gap-2">
      <FieldLabel htmlFor={id} className="flex-1 text-[0.8125rem] font-normal">
        {label}
      </FieldLabel>
      <Switch id={id} size="sm" checked={checked} onCheckedChange={onChange} />
    </Field>
  )
}

function rank(type: string): number {
  const at = typeOrder.indexOf(type)
  return at === -1 ? typeOrder.length : at
}
