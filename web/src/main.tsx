import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './app/App'
import { applyLocale } from './lib/i18n'
import { applyTheme, watchSystemTheme } from './lib/theme'
import { applyUiSize } from './lib/uiSize'
import './styles/theme.css'

applyTheme()
watchSystemTheme()
applyLocale()
applyUiSize()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
