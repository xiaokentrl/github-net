import React from 'react'
import ReactDOM from 'react-dom/client'
import './styles/tokens.css'
import './styles/themes.css'
import App from './app/App'
import ErrorBoundary from './app/ErrorBoundary'

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </React.StrictMode>,
)
