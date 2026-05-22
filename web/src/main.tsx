import { StrictMode, Component } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'

class ErrorBoundary extends Component<{ children: React.ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) { return { error } }
  render() {
    if (this.state.error) {
      return (
        <div style={{ padding: 48, color: 'red', fontFamily: 'monospace' }}>
          <h1>应用错误</h1>
          <pre>{this.state.error.message}</pre>
          <pre style={{ fontSize: 12, marginTop: 16 }}>{this.state.error.stack}</pre>
        </div>
      )
    }
    return this.props.children
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <ConfigProvider locale={zhCN}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </ConfigProvider>
    </ErrorBoundary>
  </StrictMode>,
)
