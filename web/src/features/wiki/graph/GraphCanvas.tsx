import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { ControlButton, useNodesState, useReactFlow, useStore, useStoreApi, type OnNodeDrag, type OnNodesChange } from '@xyflow/react'
import { MaximizeIcon } from 'lucide-react'
import type { WikiGraph } from '@/api/types'
import type { WikiSpace } from '@/api/wiki'
import { Canvas } from '@/components/ai-elements/canvas'
import { Controls } from '@/components/ai-elements/controls'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { useT } from '@/lib/i18n'
import { rootRem } from '@/lib/rem'
import { useResolvedScheme } from '@/lib/theme'
import { useEscape } from '@/lib/useEscape'
import { cn } from '@/lib/utils'
import type { OpenTopic } from '../CommitRow'
import { arrange, toNodes } from './arrange'
import { FocusCard, label } from './FocusCard'
import { GraphViewContext, type GraphView } from './focus'
import { edgeSentences, edgeTypes, toFlowEdge } from './GraphEdges'
import { nodeTypes, type GraphFlowNode } from './GraphNodes'
import { GraphToolbar } from './GraphToolbar'
import { designRem, dotRadius, glyphOf, regionName } from './glyphs'
import { bigGraph, placeLabels, type Mark, type Region } from './labels'
import { generalCluster } from './layout'
import { steps, type GraphFilters } from './model'
import { boundsOf, focusArea, focusView, halfName, nameArea, openView, useNarrowCanvas, viewArea, wholeView, type Fit } from './reveal'
import { useBlocked, useSettledView } from './useCanvasView'

// The relation graph's canvas (docs/design.md 5.17, webui.md 4.14): the
// dots and their names, the toolbar over it and the card about the node in
// focus beside it, and where the view goes.

// Only a project's wiki has a graph (docs/design.md 5.17).
export type GraphSpace = Extract<WikiSpace, { kind: 'project' }>

export interface CanvasProps {
  space: GraphSpace
  graph: WikiGraph
  paths: Map<string, string[]>
  placed: WikiGraph
  shown: WikiGraph
  filters: GraphFilters
  onFilters: (filters: GraphFilters) => void
  focus: string
  onFocus: (id: string) => void
  onOpenThread: OpenTopic
}

// How far out and how near the view goes. Names keep their size, so
// zooming out only crowds the dots, and in only spreads them.
const minZoom = 0.1
const maxZoom = 4

export function GraphCanvas({ space, graph, paths, placed, shown, filters, onFilters, focus, onFocus, onOpenThread }: CanvasProps) {
  const t = useT()
  const scheme = useResolvedScheme()
  const flow = useReactFlow<GraphFlowNode>()
  const store = useStoreApi<GraphFlowNode>()
  const narrow = useNarrowCanvas()
  const width = useStore((state) => state.width)
  const height = useStore((state) => state.height)
  const frame = useRef<HTMLDivElement>(null)
  const toolbar = useRef<HTMLDivElement>(null)
  const rem = rootRem()
  // Each set of nodes the filters let through is laid out once, as it
  // first shows: after that, nodes stay where the layout or a person's
  // dragging put them.
  const signature = useMemo(() => placed.nodes.map((node) => node.id).join('\n'), [placed])
  const [arranged, setArranged] = useState(() => arrange(placed, signature, rem))
  const [initial] = useState(() => toNodes(arranged, placed, t))
  const [nodes, setNodes, onNodesChange] = useNodesState(initial)
  // Where the dots are for their names: where they were laid out, and
  // where a drag left them, once it ends.
  const [spots, setSpots] = useState(arranged.points)
  if (arranged.signature !== signature) {
    const next = arrange(placed, signature, rem)
    setArranged(next)
    setNodes(toNodes(next, placed, t))
    setSpots(next.points)
  }
  const onNodeDragStop = useCallback<OnNodeDrag<GraphFlowNode>>(
    (_, __, dragged) =>
      setSpots((prev) => {
        const next = new Map(prev)
        for (const node of dragged) next.set(node.id, node.position)
        return next
      }),
    [],
  )

  // The view as it last stopped, the zoom scaled back out of what is drawn.
  const [view, settled] = useSettledView(frame)

  // Where the graph opens (openView): whole, or on a canvas too small for
  // that, its middle at the spacing a desktop gives it. Its zoom is the
  // base the names' rules and focusing go by.
  const fit = useMemo<Fit | undefined>(() => {
    const bounds = boundsOf(arranged.points.values())
    const titles = placed.nodes.flatMap((node) => (node.page ? [node.page.title] : []))
    return bounds && { bounds, count: arranged.points.size, half: halfName(titles, rem) }
  }, [arranged, placed, rem])
  const opening = useMemo(() => (fit && width > 0 && height > 0 ? openView(fit, width, height, rem) : undefined), [fit, width, height, rem])
  const base = opening?.base ?? 1
  // The first view of a layout, once the canvas is measured: on the node a
  // link asked to focus on, else as it opens; again as the canvas settles
  // into its size, until a person moves the view. The canvas shows once it
  // is set, its clusters fading in.
  const viewed = useRef({ signature: '', size: '' })
  const touched = useRef(false)
  const [shownView, setShownView] = useState(false)
  useEffect(() => {
    if (!opening) return
    const size = `${width}x${height}`
    const fresh = viewed.current.signature !== arranged.signature
    if (!fresh && (touched.current || viewed.current.size === size)) return
    const first = viewed.current.signature === ''
    viewed.current = { signature: arranged.signature, size }
    if (fresh) touched.current = false
    const point = focus ? arranged.points.get(focus) : undefined
    const next = point ? focusView(point, opening.view.zoom, opening.base, focusArea(width, height, rem), maxZoom) : opening.view
    const duration = first || !fresh ? 0 : 200
    void flow.setViewport(next, { duration })
    // A view set at once has its names at once, not a beat after the dots.
    if (duration === 0) settled(next)
    setShownView(true)
  }, [opening, arranged, focus, flow, width, height, rem, settled])

  // The toolbar and the zoom, which the names keep clear of.
  const blocked = useBlocked(frame, toolbar, `${narrow} ${focus !== ''}`)

  // What is lit: the node in focus, or else the one hovered, and those
  // near it; a node hovered while another is in focus shows its name. A
  // touch screen has no hover: a tap would leave its node lit.
  const [hover, setHover] = useState('')
  const [canHover] = useState(() => window.matchMedia?.('(hover: hover)')?.matches ?? true)
  const hovered = shown.nodes.some((node) => node.id === hover) ? hover : ''
  const lead = focus || hovered
  const near = useMemo(() => (lead ? steps(shown, lead, focus ? filters.depth : 1) : new Map<string, number>()), [shown, lead, focus, filters.depth])
  const extra = focus && hovered !== focus ? hovered : ''

  // The names that have room (placeLabels), for the view as it last
  // stopped, clear of the toolbar and the card.
  const marks = useMemo<Mark[]>(
    () =>
      placed.nodes.map((node) => {
        const at = spots.get(node.id) ?? { x: 0, y: 0 }
        const glyph = glyphOf(node, arranged.clustering.of.get(node.id) === node.id)
        const degree = arranged.degree.get(node.id) ?? 0
        return {
          id: node.id,
          x: at.x * view.zoom + view.x,
          y: at.y * view.zoom + view.y,
          r: (dotRadius(glyph, degree) * rem) / designRem,
          glyph,
          degree,
          name: node.page ? node.page.title : node.topic ? t('wiki.graph.topic', { n: node.topic.number }) : label(t, node),
        }
      }),
    [placed, spots, arranged, view, rem, t],
  )
  const regions = useMemo<Region[]>(() => {
    const { clusters } = arranged.clustering
    // One cluster of pages about no module: nothing to tell apart.
    if (clusters.every((cluster) => cluster.id === generalCluster)) return []
    const titles = new Map(placed.nodes.map((node) => [node.id, node.page?.title ?? '']))
    return clusters.map((cluster) => ({
      id: cluster.id,
      name: cluster.id === generalCluster ? t('wiki.graph.general') : regionName(titles.get(cluster.id) ?? ''),
      hub: cluster.id,
      members: cluster.members,
    }))
  }, [arranged, placed, t])
  const labels = useMemo(
    () =>
      placeLabels(marks, regions, {
        area: width > 0 && height > 0 ? nameArea(width, height, focus !== '', rem) : undefined,
        blocked,
        lead,
        near,
        hover: extra,
        zoom: view.zoom / base,
        big: placed.nodes.length > bigGraph,
        scale: rem / designRem,
      }),
    [marks, regions, width, height, focus, rem, blocked, lead, near, extra, view, base, placed],
  )
  const graphView = useMemo<GraphView>(() => ({ focus, lead, near, hover: extra, labels, shown: shownView }), [focus, lead, near, extra, labels, shownView])

  // A page renamed or checked since shows as it is now, without moving.
  const byId = useMemo(() => new Map(placed.nodes.map((node) => [node.id, node])), [placed])
  const current = useMemo(
    () =>
      nodes.map((node) => {
        const fresh = byId.get(node.id)
        return fresh && fresh !== node.data.node ? { ...node, data: { ...node.data, node: fresh } } : node
      }),
    [nodes, byId],
  )
  // An edge is lit when it leads a step out from the node lit, within
  // the reach the filters set.
  const edges = useMemo(() => {
    const names = new Map(shown.nodes.map((node) => [node.id, label(t, node)]))
    const { of } = arranged.clustering
    return shown.edges.map((edge) => {
      const from = near.get(edge.from)
      const to = near.get(edge.to)
      const lit = from !== undefined && to !== undefined && Math.abs(from - to) === 1
      const said = t(edgeSentences[edge.kind], { from: names.get(edge.from) ?? edge.from, to: names.get(edge.to) ?? edge.to })
      return toFlowEdge(edge, of.get(edge.from) === of.get(edge.to), lit, lead !== '' && !lit, said)
    })
  }, [shown, arranged, near, lead, t])
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
  // Focusing moves the node into the middle of the room the card leaves,
  // near enough for the names round it (focusView).
  const pick = useCallback(
    (id: string) => {
      onFocus(id)
      const node = flow.getNode(id)
      const { width, height, transform } = store.getState()
      if (!node || !opening) return
      touched.current = true
      void flow.setViewport(focusView(node.position, transform[2], opening.base, focusArea(width, height, rem), maxZoom), { duration: 300 })
    },
    [onFocus, flow, store, opening, rem],
  )
  // Closing the card hands the keyboard back to the node it was about.
  const close = useCallback(() => {
    const id = focus
    onFocus('')
    requestAnimationFrame(() => {
      const nodes = frame.current?.querySelectorAll<HTMLElement>('.react-flow__node') ?? []
      ;[...nodes].find((node) => node.dataset.id === id)?.focus()
    })
  }, [focus, onFocus])
  useEscape(close, focus !== '')
  const unfocus = useCallback(() => onFocus(''), [onFocus])
  // The fit button takes in the whole graph, as it is now, clear of the card.
  const fitAll = () => {
    const bounds = boundsOf(flow.getNodes().map((node) => node.position))
    if (!bounds || !fit) return
    touched.current = true
    const { width, height } = store.getState()
    void flow.setViewport(wholeView({ ...fit, bounds }, viewArea(width, height, focus !== '', rem), rem), { duration: 300 })
  }
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' || !(event.target instanceof Element)) return
    const id = event.target.closest('.react-flow__node')?.getAttribute('data-id')
    if (id) pick(id)
  }

  return (
    <div ref={frame} className="relative size-full">
      <div ref={toolbar} className="absolute top-3 left-3 z-10">
        <GraphToolbar space={space} graph={graph} shown={shown.nodes} paths={paths} filters={filters} onFilters={onFilters} onFind={pick} />
      </div>
      {focus ? (
        <div
          className={cn(
            'absolute z-10 flex flex-col overflow-hidden rounded-md border bg-card shadow-pop',
            narrow
              ? 'inset-x-3 bottom-3 max-h-[45%] motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2'
              : 'top-3 right-3 max-h-[calc(100%-1.5rem)] w-80',
          )}
        >
          <FocusCard space={space} graph={shown} id={focus} paths={paths.get(focus)} onFocus={pick} onOpenThread={onOpenThread} onClose={close} />
        </div>
      ) : null}
      <GraphViewContext value={graphView}>
        <Canvas
          nodes={current}
          edges={edges}
          // AI Elements' canvas takes React Flow's plain nodes; these are its.
          onNodesChange={onNodesChange as OnNodesChange}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          nodeOrigin={[0.5, 0.5]}
          colorMode={scheme}
          fitView={false}
          minZoom={minZoom}
          maxZoom={maxZoom}
          panOnDrag
          panOnScroll={false}
          selectionOnDrag={false}
          deleteKeyCode={null}
          nodesConnectable={false}
          elementsSelectable={false}
          edgesFocusable={false}
          onlyRenderVisibleElements={placed.nodes.length > 200}
          onNodeClick={(_, node) => pick(node.id)}
          onNodeMouseEnter={(_, node) => canHover && setHover(node.id)}
          onNodeMouseLeave={() => setHover('')}
          onNodeDragStop={onNodeDragStop as OnNodeDrag}
          onMoveStart={(event) => {
            // A person's own move, not one of the view's.
            if (event) touched.current = true
          }}
          onPaneClick={unfocus}
          onKeyDown={onKeyDown}
          aria-label={t('wiki.graph.canvas')}
          ariaLabelConfig={ariaLabels}
          // A plain ground, the cards' colour, without React Flow's dots.
          className={cn('graph-canvas', !shownView && 'opacity-0')}
        >
          {/* Under the card on a narrow canvas, the zoom goes up out of its way. */}
          <Controls showInteractive={false} showFitView={false} position={narrow && focus ? 'top-right' : 'bottom-left'}>
            <ControlButton onClick={fitAll} title={t('wiki.graph.fitView')} aria-label={t('wiki.graph.fitView')}>
              <MaximizeIcon />
            </ControlButton>
          </Controls>
        </Canvas>
      </GraphViewContext>
      {/* Over the empty canvas, in its middle; the filters above stay in reach. */}
      {shown.nodes.length === 0 ? (
        <Empty className="pointer-events-none absolute inset-0 z-5">
          <EmptyHeader>
            <EmptyTitle>{t('wiki.graph.filteredOut')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : null}
    </div>
  )
}
