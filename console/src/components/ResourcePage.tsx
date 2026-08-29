import { Alert, Button, Descriptions, Drawer, Space, Typography } from 'antd'
import type { DescriptionsProps, DrawerProps } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ReactNode } from 'react'
import { apiErrorText } from '../api/presentation'

interface ResourcePageHeaderProps {
  title: string
  description: string
  actions?: ReactNode
  dataUpdatedAt?: number
  isFetching?: boolean
  onRefresh?: () => void
  autoRefreshText?: string
}

export function ResourcePageHeader({
  title,
  description,
  actions,
  dataUpdatedAt = 0,
  isFetching,
  onRefresh,
  autoRefreshText,
}: ResourcePageHeaderProps) {
  return (
    <section className="resource-page-header">
      <div className="resource-page-copy">
        <Typography.Title level={3}>{title}</Typography.Title>
        <Typography.Paragraph>{description}</Typography.Paragraph>
        {(autoRefreshText || dataUpdatedAt > 0) && (
          <Typography.Text type="secondary" className="resource-page-updated">
            {autoRefreshText ? `${autoRefreshText} · ` : ''}
            最近更新：{dataUpdatedAt > 0
              ? new Date(dataUpdatedAt).toLocaleTimeString('zh-CN', { hour12: false })
              : '正在加载'}
          </Typography.Text>
        )}
      </div>
      <Space wrap className="resource-page-actions">
        {onRefresh && (
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={onRefresh}>
            刷新
          </Button>
        )}
        {actions}
      </Space>
    </section>
  )
}

export function PageQueryError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <Alert
      type="error"
      showIcon
      message="页面数据加载失败"
      description={apiErrorText(error)}
      action={onRetry ? <Button size="small" onClick={onRetry}>重试</Button> : undefined}
    />
  )
}

interface ResourceDetailDrawerProps {
  open: boolean
  title: string
  items: DescriptionsProps['items']
  onClose: () => void
  width?: DrawerProps['width']
  extra?: ReactNode
}

export function ResourceDetailDrawer({ open, title, items, onClose, width = 'min(560px, 100vw)', extra }: ResourceDetailDrawerProps) {
  return (
    <Drawer open={open} title={title} width={width} onClose={onClose} extra={extra}>
      <Descriptions bordered size="small" column={1} items={items} />
    </Drawer>
  )
}
