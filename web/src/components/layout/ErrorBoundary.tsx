import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Panel } from './Panel'
import { PanelHeader } from './PanelHeader'
import { t } from '@/lib/i18n'

interface Props {
  children: ReactNode
  // Changing the key resets the boundary, e.g. on navigation.
  resetKey?: string
}

interface State {
  error?: Error
}

// Keeps a rendering error inside the page it happened in: the sidebar
// stays, and the reader can reload just this part.
export class ErrorBoundary extends Component<Props, State> {
  state: State = {}

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('page crashed', error, info.componentStack)
  }

  componentDidUpdate(prev: Props) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: undefined })
    }
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <Panel>
        <PanelHeader title={t('page.crashed')} />
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('page.crashedTitle')}</EmptyTitle>
            <EmptyDescription>{this.state.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button size="sm" onClick={() => location.reload()}>
              {t('common.reload')}
            </Button>
          </EmptyContent>
        </Empty>
      </Panel>
    )
  }
}
