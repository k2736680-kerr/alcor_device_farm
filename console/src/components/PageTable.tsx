import { Table } from 'antd'
import type { TableProps } from 'antd'

export interface PageTableProps<T> extends Omit<TableProps<T>, 'pagination' | 'rowKey'> {
  total: number
  page: number
  pageSize: number
  onPageChange: (page: number, pageSize: number) => void
}

/** Server-paginated Ant Design table. */
export function PageTable<T extends object>({
  total,
  page,
  pageSize,
  onPageChange,
  ...rest
}: PageTableProps<T>) {
  return (
    <Table<T>
      {...rest}
      rowKey={(record) => String((record as { id?: unknown }).id ?? JSON.stringify(record))}
      scroll={{ x: 'max-content' }}
      pagination={{
        current: page,
        pageSize,
        total,
        showSizeChanger: true,
        showQuickJumper: true,
        showTotal: (count) => `共 ${count} 条`,
        onChange: onPageChange,
      }}
    />
  )
}
