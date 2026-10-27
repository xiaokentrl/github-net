import { Component, type ErrorInfo, type ReactNode } from 'react'
import { t } from '../i18n'

type Props = { children: ReactNode }
type State = { error: Error | null }

// 渲染崩溃时至少留下一条能看懂的失败信息和重试入口，不能留白屏。
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('界面渲染失败', error, info.componentStack)
  }

  render(): ReactNode {
    if (!this.state.error) return this.props.children
    return (
      <main className="app-error">
        <h1>{t('error.boundary.title')}</h1>
        <p role="alert">{t('error.boundary.detail', { reason: this.state.error.message || t('error.unknown') })}</p>
        <button type="button" onClick={() => this.setState({ error: null })}>
          {t('error.boundary.action')}
        </button>
      </main>
    )
  }
}

export default ErrorBoundary
