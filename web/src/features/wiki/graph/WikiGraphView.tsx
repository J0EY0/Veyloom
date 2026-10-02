import { useCallback, useMemo, useState } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import { BookXIcon, NetworkIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import type { WikiGraph } from '@/api/types'
import { errorText } from '@/api/errorText'
import { useWikiGraph } from '@/api/wiki'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { useLocale, useT } from '@/lib/i18n'
import type { OpenTopic } from '../CommitRow'
import { foldPaths } from './fold'
import { GraphCanvas, type GraphSpace } from './GraphCanvas'
import { defaultFilters, filtersShowing, visibleGraph } from './model'

export interface WikiGraphViewProps {
  space: GraphSpace
  onOpenThread: OpenTopic
}

// WikiGraphView is the relation graph of a wiki (docs/design.md 5.17,
// webui.md 4.14): its pages, the directories of the paths they name and
// the topics they came from, as dots, the pages about each module drawn
// together round it. Picking a node focuses on it, and ?focus= in the
// address says which, so a page can link to itself on the graph.
export function WikiGraphView({ space, onOpenThread }: WikiGraphViewProps) {
  const t = useT()
  const graph = useWikiGraph(space)
  if (graph.isError) {
    return (
      <Empty className="h-full">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <BookXIcon />
          </EmptyMedia>
          <EmptyTitle>{t('wiki.graph.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(graph.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  if (!graph.data) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner aria-label={t('common.loading')} className="text-subtle" />
      </div>
    )
  }
  if (!graph.data.nodes.some((node) => node.page)) {
    return (
      <Empty className="h-full">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <NetworkIcon />
          </EmptyMedia>
          <EmptyTitle>{t('wiki.graph.empty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return <GraphExplorer space={space} graph={graph.data} onOpenThread={onOpenThread} />
}

interface ExplorerProps {
  space: GraphSpace
  graph: WikiGraph
  onOpenThread: OpenTopic
}

function GraphExplorer({ space, graph, onOpenThread }: ExplorerProps) {
  const [params, setParams] = useSearchParams()
  // The paths pages name fold into their directories; an address focusing
  // on a path focuses on its directory.
  const folded = useMemo(() => foldPaths(graph), [graph])
  const named = params.get('focus') ?? ''
  const asked = folded.into.get(named) ?? named
  // The filters start where they show the node a link asked to focus on.
  const [filters, setFilters] = useState(() => filtersShowing(folded.graph, defaultFilters, asked))
  const { hiddenTypes, deprecated, files, topics, external, hiddenKinds } = filters
  // What the layout places: the nodes the filters let through, with every
  // edge between them, so that leaving a kind of relation out hides its
  // lines and leaves the nodes where they are.
  const placed = useMemo(
    () => visibleGraph(folded.graph, { ...defaultFilters, hiddenTypes, deprecated, files, topics, external }),
    [folded, hiddenTypes, deprecated, files, topics, external],
  )
  const shown = useMemo(() => ({ nodes: placed.nodes, edges: placed.edges.filter((edge) => !hiddenKinds.includes(edge.kind)) }), [placed, hiddenKinds])
  const focus = shown.nodes.some((node) => node.id === asked) ? asked : ''
  const setFocus = useCallback(
    (id: string) =>
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (id) next.set('focus', id)
          else next.delete('focus')
          return next
        },
        { replace: true },
      ),
    [setParams],
  )
  // The canvas says its nodes, edges and controls in the interface's
  // language, worked out once: a new language draws it anew.
  const locale = useLocale()

  return (
    <div className="relative size-full min-h-0">
      <ReactFlowProvider>
        <GraphCanvas
          key={locale}
          space={space}
          graph={folded.graph}
          paths={folded.paths}
          placed={placed}
          shown={shown}
          filters={filters}
          onFilters={setFilters}
          focus={focus}
          onFocus={setFocus}
          onOpenThread={onOpenThread}
        />
      </ReactFlowProvider>
    </div>
  )
}
