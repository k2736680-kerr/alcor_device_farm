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
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/console">
        <ConfigProvider
          locale={zhCN}
          theme={{
            token: {
              colorPrimary: '#2563eb',
              colorInfo: '#2563eb',
              colorSuccess: '#16a34a',
              colorWarning: '#f59e0b',
              colorError: '#dc2626',
              borderRadius: 10,
              fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", sans-serif',
            },
            components: {
              Layout: { headerBg: '#ffffff', bodyBg: '#f3f6fb', siderBg: '#07182c' },
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
