import { App as AntApp, Button, Popconfirm, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { getListDeviceImagesQueryKey, useListDeviceImages, useValidateDeviceImage } from '../api/generated/device-farm'
import type { DeviceImage } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { imageStatusLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'

function canValidate(image: DeviceImage): boolean {
  return image.status === 'draft' || image.status === 'failed' || image.status === 'disabled'
}

export function ImagesPage() {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const validate = useValidateDeviceImage()

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceImagesQueryKey() })
  }

  const runValidate = (image: DeviceImage) => {
    validate.mutate(
      { id: image.id },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`镜像验证已受理（request_id: ${requestID}）`)
          invalidate()
        },
        onError: (error) => {
          const err = error as { code?: string; requestId?: string; message?: string }
          message.error(`验证被拒绝（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
        },
      },
    )
  }

  const columns: TableColumnsType<DeviceImage> = [
    { title: '镜像编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 160 },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'ready' ? 'green' : value === 'failed' ? 'red' : 'orange'}>{imageStatusLabel(value)}</Tag> },
    { title: 'API 级别', dataIndex: 'api_level', width: 90 },
    { title: 'ABI', dataIndex: 'abi', width: 90 },
    { title: '分辨率', dataIndex: 'resolution', width: 110 },
    { title: '镜像引用', dataIndex: 'docker_image', ellipsis: true, render: (value?: string) => value ?? '-' },
    { title: '摘要', dataIndex: 'docker_digest', ellipsis: true, render: (value: string) => shortID(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      fixed: 'right',
      render: (_, image) =>
        canValidate(image) ? (
          <Popconfirm
            title="发起镜像验证"
            description="将向宿主代理下发镜像验证命令，验证完成前镜像不可用。"
            okText="确认验证"
            onConfirm={() => runValidate(image)}
          >
            <Button size="small" loading={validate.isPending}>验证</Button>
          </Popconfirm>
        ) : (
          <Typography.Text type="secondary">-</Typography.Text>
        ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isLoading } = useListDeviceImages(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DeviceImage>(data)
  return (
    <PageTable<DeviceImage>
      columns={columns}
      dataSource={result?.items}
      loading={isLoading}
      total={result?.total ?? 0}
      page={result?.page ?? page}
      pageSize={result?.page_size ?? pageSize}
      onPageChange={onPageChange}
    />
  )
}
