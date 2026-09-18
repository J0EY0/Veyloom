import { AgentAvatar } from '@/components/shared/agent-avatar'
import { UserAvatar } from '@/components/shared/user-avatar'
import { PromptInputCommand, PromptInputCommandGroup, PromptInputCommandItem, PromptInputCommandList } from '@/components/ai-elements/prompt-input'
import type { MentionTarget } from './useMentionTargets'
import { useT } from '@/lib/i18n'

export interface MentionPickerProps {
  items: MentionTarget[]
  active: number
  onSelect: (item: MentionTarget) => void
  // The listbox id, for the textarea's aria-controls.
  id: string
}

export function mentionKey(item: MentionTarget): string {
  return `${item.mention.kind}:${item.mention.id}`
}

// The popover above the composer listing who can be mentioned. The
// textarea keeps focus and drives the highlight; this is display plus
// mouse selection, on AI Elements' prompt-input command list.
export function MentionPicker({ items, active, onSelect, id }: MentionPickerProps) {
  const t = useT()
  const activeKey = items[active] ? mentionKey(items[active]) : ''
  return (
    <PromptInputCommand
      shouldFilter={false}
      value={activeKey}
      className="absolute bottom-full left-0 z-20 mb-2 h-auto w-65 rounded-[10px] border bg-popover p-1 shadow-pop"
    >
      <PromptInputCommandList id={id} label={t('mention.title')} className="max-h-60">
        <PromptInputCommandGroup heading={t('mention.title')}>
          {items.map((item) => (
            <PromptInputCommandItem
              key={mentionKey(item)}
              value={mentionKey(item)}
              onSelect={() => onSelect(item)}
              onMouseDown={(event) => {
                // Before the textarea loses focus, so the caret is where it was.
                event.preventDefault()
              }}
              className="h-8 gap-2.5"
            >
              {item.look ? <AgentAvatar look={item.look} size="sm" /> : <UserAvatar name={item.name} size="sm" />}
              <span className="min-w-0 truncate">{item.name}</span>
            </PromptInputCommandItem>
          ))}
        </PromptInputCommandGroup>
      </PromptInputCommandList>
    </PromptInputCommand>
  )
}
