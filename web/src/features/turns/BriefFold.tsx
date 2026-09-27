import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useT } from '@/lib/i18n'

const labels = {
  brief: { shut: 'event.brief', open: 'event.briefFold' },
  system: { shut: 'event.systemPrompt', open: 'event.systemPromptFold' },
  steer: { shut: 'event.steerText', open: 'event.steerTextFold' },
  ask: { shut: 'event.askText', open: 'event.askTextFold' },
} as const

// What a run of the turn began with, as the agent got it: the brief the hub
// composed for it (docs/design.md 5.2), or its system prompt, the role card
// and the standing instructions (5.23.1); or what it was passed as it ran
// (5.23.2), or asked when it said nothing in the chat (5.24). Folded until asked for, being long and much the same from turn
// to turn. When an agent seems not to know something, this says whether it
// was told.
export function BriefFold({ prompt, kind = 'brief' }: { prompt: string; kind?: keyof typeof labels }) {
  const [open, setOpen] = useState(false)
  const t = useT()
  const text = prompt.trimEnd()
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <Button variant="ghost" size="xs" className="-ml-1.5 h-5 px-1.5 text-xs text-subtle hover:text-foreground">
          {open ? t(labels[kind].open) : t(labels[kind].shut, { n: text.split('\n').length })}
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div
          className="mt-1 rounded-md bg-secondary px-2.5 py-2 font-mono text-[0.71875rem] break-words whitespace-pre-wrap text-muted-foreground"
          translate="no"
        >
          {text}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
