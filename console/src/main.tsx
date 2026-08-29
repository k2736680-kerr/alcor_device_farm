import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App as AntApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import 'antd/dist/reset.css'
import App from './App'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
      staleTime: 10_000,
    },
  },
})

const routerBase = window.location.pathname.startsWith('/api/v2/device-farm/console')
  ? '/api/v2/device-farm/console'
  : '/console'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename={routerBase}>
        <ConfigProvider
          locale={zhCN}
          theme={{
            token: {
              // 与 Alcor 主平台（alcor_console App.tsx ConfigProvider）保持同一套视觉令牌。
              colorPrimary: '#1769d1',
              colorInfo: '#1769d1',
              colorSuccess: '#16a34a',
              colorWarning: '#f59e0b',
              colorError: '#dc2626',
              borderRadius: 8,
              fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
            },
            components: {
              Layout: { headerBg: '#ffffff', bodyBg: '#f8fafc', siderBg: '#07182c' },
              Menu: { darkItemBg: '#07182c', darkSubMenuItemBg: '#07182c', darkItemSelectedBg: '#1d4ed8' },
              Card: { paddingLG: 22 },
            },
          }}
        >
          <AntApp>
            <App />
          </AntApp>
        </ConfigProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
