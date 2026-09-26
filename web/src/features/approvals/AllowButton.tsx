import { ChevronDownIcon } from 'lucide-react'
import type { AllowScope, Approval } from '@/api/types'
import { ConfirmationAction } from '@/components/ai-elements/confirmation'
import { ButtonGroup } from '@/components/ui/button-group'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useT } from '@/lib/i18n'
import { scopeTerms, type ScopeTerms } from './describe'

export interface AllowButtonProps {
  approval: Approval
  disabled: boolean
  busy: boolean
  onAllow: (scope: AllowScope) => void
}

// AllowButton allows a request: the button lets this one through, and the
// menu beside it lets more through with it (docs/design.md 4.6). The like
// of the request for the rest of the turn, or kept for the member from now
// on, are there only as far as the runtime offered them; whatever else the
// turn asks for always is.
export function AllowButton({ approval, disabled, busy, onAllow }: AllowButtonProps) {
  const t = useT()
  const similar = scopeTerms(approval, 'similar')
  const always = scopeTerms(approval, 'always')
  return (
    <ButtonGroup>
      <ConfirmationAction size="sm" onClick={() => onAllow('once')} disabled={disabled} aria-busy={busy}>
        {t('common.allow')}
      </ConfirmationAction>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <ConfirmationAction size="sm" className="h-8 border-l border-primary-foreground/20 px-1.5" aria-label={t('approval.scope.menu')} disabled={disabled}>
            <ChevronDownIcon />
          </ConfirmationAction>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64" aria-label={t('approval.scope.menu')}>
          {similar ? <ScopeItem label={t('approval.scope.similar')} terms={similar} onSelect={() => onAllow('similar')} /> : null}
          {always ? <ScopeItem label={t('approval.scope.always')} terms={always} onSelect={() => onAllow('always')} /> : null}
          {similar || always ? <DropdownMenuSeparator /> : null}
          <DropdownMenuItem onSelect={() => onAllow('turn')}>{t('approval.scope.turn')}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </ButtonGroup>
  )
}

// ScopeItem is one scope in the menu, with what it takes in: the command
// pattern where it is commands, in words otherwise.
function ScopeItem({ label, terms, onSelect }: { label: string; terms: ScopeTerms; onSelect: () => void }) {
  return (
    <DropdownMenuItem onSelect={onSelect} className="gap-2.5" aria-label={`${label} ${terms.code ?? terms.words}`}>
      <span className="flex-none">{label}</span>
      <span className="grow" />
      {terms.code ? (
        <code className="min-w-0 truncate rounded-[0.3125rem] border bg-muted px-1.5 py-px font-mono text-[0.71875rem] text-muted-foreground" translate="no">
          {terms.code}
        </code>
      ) : (
        <span className="min-w-0 truncate text-xs text-subtle">{terms.words}</span>
      )}
    </DropdownMenuItem>
  )
}
