import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { ReactFlowProvider, useNodesState, type OnNodesChange } from '@xyflow/react'
import { BookXIcon, NetworkIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import type { WikiGraph } from '@/api/types'
import { errorText } from '@/api/errorText'
import { useWikiGraph, type WikiSpace } from '@/api/wiki'
import { Canvas } from '@/components/ai-elements/canvas'
import { Controls } from '@/components/ai-elements/controls'
import { Panel } from '@/components/ai-elements/panel'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { useLocale, useT } from '@/lib/i18n'
import { rootRem } from '@/lib/rem'
import { useResolvedScheme } from '@/lib/theme'
import { useEscape } from '@/lib/useEscape'
import { cn } from '@/lib/utils'
import type { OpenTopic } from '../CommitRow'
import { FocusCard, label } from './FocusCard'
import { GraphFocusContext } from './focus'
import { edgeSentences, edgeTypes, toFlowEdge } from './GraphEdges'
import { nodeRadius, nodeTypes, type GraphFlowNode } from './GraphNodes'
import { GraphToolbar } from './GraphToolbar'
import { fitOptions, useFocusView, useNarrowCanvas } from './reveal'
import { defaultFilters, filtersShowing, layout, neighborhood, visibleGraph, type GraphFilters } from './model'

// Only a project's wiki has a graph (docs/design.md 5.17).
type GraphSpace = Extract<WikiSpace, { kind: 'project' }>

export interface WikiGraphViewProps {
  space: GraphSpace
  onOpenThread: OpenTopic
}

// WikiGraphView is the relation graph of a wiki (docs/design.md 5.17,
// webui.md 4.14): its pages, and the paths and topics they name, drawn
// where a force layout puts them. Picking a node focuses on it, and ?focus=
// in the address says which, so a page can link to itself on the graph.
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
  const asked = params.get('focus') ?? ''
  // The filters start where they show the node a link asked to focus on.
  const [filters, setFilters] = useState(() => filtersShowing(graph, defaultFilters, asked))
  const { hiddenTypes, deprecated, files, topics, external, hiddenKinds, depth } = filters
  // What the layout places: the nodes the filters let through, with every
  // edge between them, so that leaving a kind of relation out hides its
  // lines and leaves the nodes where they are.
  const placed = useMemo(
    () => visibleGraph(graph, { ...defaultFilters, hiddenTypes, deprecated, files, topics, external }),
    [graph, hiddenTypes, deprecated, files, topics, external],
  )
  const shown = useMemo(() => ({ nodes: placed.nodes, edges: placed.edges.filter((edge) => !hiddenKinds.includes(edge.kind)) }), [placed, hiddenKinds])
  const focus = shown.nodes.some((node) => node.id === asked) ? asked : ''
  const near = useMemo(() => (focus ? neighborhood(shown, focus, depth) : new Set<string>()), [shown, focus, depth])
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
  const unfocus = useCallback(() => setFocus(''), [setFocus])
  // The canvas says its edges and controls in the interface's language,
  // worked out once: a new language draws it anew.
  const locale = useLocale()
  useEscape(unfocus, focus !== '')
  const focusState = useMemo(() => ({ focus, near }), [focus, near])

  return (
    <div className="relative size-full min-h-0">
      <ReactFlowProvider>
        <GraphFocusContext value={focusState}>
          <GraphCanvas
            key={locale}
            space={space}
            graph={graph}
            placed={placed}
            shown={shown}
            filters={filters}
            onFilters={setFilters}
            focus={focus}
            near={near}
            onFocus={setFocus}
            onUnfocus={unfocus}
            onOpenThread={onOpenThread}
          />
        </GraphFocusContext>
      </ReactFlowProvider>
    </div>
  )
}

interface CanvasProps {
  space: GraphSpace
  graph: WikiGraph
  placed: WikiGraph
  shown: WikiGraph
  filters: GraphFilters
  onFilters: (filters: GraphFilters) => void
  focus: string
  near: ReadonlySet<string>
  onFocus: (id: string) => void
  onUnfocus: () => void
  onOpenThread: OpenTopic
}

function GraphCanvas({ space, graph, placed, shown, filters, onFilters, focus, near, onFocus, onUnfocus, onOpenThread }: CanvasProps) {
  const t = useT()
  const scheme = useResolvedScheme()
  const narrow = useNarrowCanvas()
  const { fit, reveal } = useFocusView()
  // Each set of nodes the filters let through is laid out once, as it
  // first shows: after that, nodes stay where the layout or a person's
  // dragging put them.
  const signature = useMemo(() => placed.nodes.map((node) => node.id).join('\n'), [placed])
  const [laidOut, setLaidOut] = useState(signature)
  const [initial] = useState(() => placeNodes(placed))
  const [nodes, setNodes, onNodesChange] = useNodesState(initial)
  if (laidOut !== signature) {
    setLaidOut(signature)
    setNodes(placeNodes(placed))
  }
  // The first view of a layout takes in the node in focus and those near
  // it, clear of the card, or else everything. React Flow fits the first
  // as the nodes are measured, by the options as they are by then: the
  // card's place waits on the canvas's width, measured just before.
  const [opening] = useState(() => (focus ? [...near] : undefined))
  const fitViewOptions = useMemo(() => fitOptions(opening, narrow), [opening, narrow])
  const fitted = useRef(signature)
  useEffect(() => {
    if (fitted.current === signature) return
    fitted.current = signature
    fit(focus ? [...near] : undefined, 200)
  }, [signature, fit, focus, near])
  // A page renamed or checked since shows as it is now, without moving.
  const byId = useMemo(() => new Map(placed.nodes.map((node) => [node.id, node])), [placed])
  const current = useMemo(
    () =>
      nodes.map((node) => {
        const fresh = byId.get(node.id)
        return fresh && fresh !== node.data.node ? { ...node, data: { node: fresh } } : node
      }),
    [nodes, byId],
  )
  const edges = useMemo(() => {
    const names = new Map(shown.nodes.map((node) => [node.id, label(t, node)]))
    return shown.edges.map((edge) => {
      const lit = focus !== '' && near.has(edge.from) && near.has(edge.to)
      const said = t(edgeSentences[edge.kind], { from: names.get(edge.from) ?? edge.from, to: names.get(edge.to) ?? edge.to })
      return toFlowEdge(edge, lit, focus !== '' && !lit, said)
    })
  }, [shown, focus, near, t])
  // React Flow's own words, in the interface's language.
  const ariaLabels = useMemo(
    () => ({
      'node.a11yDescription.default': t('wiki.graph.nodeHint'),
      'node.a11yDescription.keyboardDisabled': t('wiki.graph.nodeHint'),
      'controls.ariaLabel': t('wiki.graph.controls'),
      'controls.zoomIn.ariaLabel': t('wiki.graph.zoomIn'),
      'controls.zoomOut.ariaLabel': t('wiki.graph.zoomOut'),
      'controls.fitView.ariaLabel': t('wiki.graph.fitView'),
    }),
    [t],
  )
  // Focusing brings the node and those near it into view where the card
  // leaves room; finding one by name also zooms in to where it reads.
  const pick = useCallback(
    (id: string, zoomIn = false) => {
      onFocus(id)
      reveal([...neighborhood(shown, id, filters.depth)], zoomIn)
    },
    [onFocus, reveal, shown, filters.depth],
  )
  const find = useCallback((id: string) => pick(id, true), [pick])
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' || !(event.target instanceof Element)) return
    const id = event.target.closest('.react-flow__node')?.getAttribute('data-id')
    if (id) pick(id)
  }

  return (
    <Canvas
      nodes={current}
      edges={edges}
      // AI Elements' canvas takes React Flow's plain nodes; these are its.
      onNodesChange={onNodesChange as OnNodesChange}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      nodeOrigin={[0.5, 0.5]}
      colorMode={scheme}
      fitViewOptions={fitViewOptions}
      minZoom={0.1}
      maxZoom={2}
      panOnDrag
      panOnScroll={false}
      selectionOnDrag={false}
      deleteKeyCode={null}
      nodesConnectable={false}
      elementsSelectable={false}
      edgesFocusable={false}
      onlyRenderVisibleElements={placed.nodes.length > 200}
      onNodeClick={(_, node) => pick(node.id)}
      onPaneClick={onUnfocus}
      onKeyDown={onKeyDown}
      aria-label={t('wiki.graph.canvas')}
      ariaLabelConfig={ariaLabels}
      className="[--xy-edge-label-background-color:var(--sidebar)] [--xy-edge-label-color:var(--muted-foreground)]"
    >
      <Panel position="top-left" className="m-3 overflow-visible border-none bg-transparent p-0">
        <GraphToolbar space={space} graph={graph} shown={shown.nodes} filters={filters} onFilters={onFilters} onFind={find} />
      </Panel>
      {/* Over the empty canvas, in its middle; the filters above stay in reach. */}
      {shown.nodes.length === 0 ? (
        <Empty className="pointer-events-none absolute inset-0 z-5">
          <EmptyHeader>
            <EmptyTitle>{t('wiki.graph.filteredOut')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : null}
      {focus ? (
        <Panel
          position={narrow ? 'bottom-center' : 'top-right'}
          className={cn('m-3 flex flex-col p-0', narrow ? 'max-h-[45%] w-[calc(100%-1.5rem)]' : 'max-h-[calc(100%-1.5rem)] w-80')}
        >
          <FocusCard space={space} graph={shown} id={focus} onFocus={pick} onOpenThread={onOpenThread} onClose={onUnfocus} />
        </Panel>
      ) : null}
      {/* Under the card on a narrow canvas, the zoom goes up out of its way. */}
      <Controls showInteractive={false} position={narrow && focus ? 'top-right' : 'bottom-left'} />
    </Canvas>
  )
}

// placeNodes lays the graph out as React Flow nodes, each at its centre.
// The layout counts in pixels, and the nodes are sized in rem.
function placeNodes(graph: WikiGraph): GraphFlowNode[] {
  const rem = rootRem()
  const points = layout(graph, (node) => nodeRadius(node) * rem)
  return graph.nodes.map((node) => ({
    id: node.id,
    type: node.kind,
    position: points.get(node.id) ?? { x: 0, y: 0 },
    data: { node },
  }))
}
