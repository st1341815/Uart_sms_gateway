import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider } from 'next-themes'
import './index.css'
import App from './App.tsx'

// 主题策略：
//   默认 dark（13 叔定的），enableSystem 保留「跟随系统」这一档
//   attribute="class" → html 上加 .dark / .light，CSS 里两套变量按类覆盖
//   disableTransitionOnChange → 切换瞬间不跑过渡，避免整页元素一起淡入淡出
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider
      attribute="class"
      defaultTheme="dark"
      enableSystem
      disableTransitionOnChange
      themes={['light', 'dark']}
      storageKey="uartsms-theme"
    >
      <App />
    </ThemeProvider>
  </StrictMode>,
)