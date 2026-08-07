import { Form, Input, Modal, Typography } from 'antd'
import { useEffect } from 'react'

interface ReasonActionModalProps {
  open: boolean
  title: string
  description?: string
  danger?: boolean
  confirmLoading?: boolean
  onSubmit: (reason: string) => void
  onCancel: () => void
}

interface ReasonFormValues {
  reason: string
}

/**
 * Danger/audited operation confirm modal: the operator must type a reason,
 * which is sent to the server and recorded in the audit trail.
 */
export function ReasonActionModal({
  open,
  title,
  description,
  danger,
  confirmLoading,
  onSubmit,
  onCancel,
}: ReasonActionModalProps) {
  const [form] = Form.useForm<ReasonFormValues>()

  useEffect(() => {
    if (open) {
      form.resetFields()
    }
  }, [open, form])

  const submit = (values: ReasonFormValues) => {
    onSubmit(values.reason.trim())
  }

  return (
    <Modal
      open={open}
      title={title}
      okText="确认执行"
      okButtonProps={{ danger }}
      cancelText="取消"
      confirmLoading={confirmLoading}
      onCancel={onCancel}
      onOk={() => form.submit()}
      destroyOnHidden
    >
      {description ? <Typography.Paragraph type="secondary">{description}</Typography.Paragraph> : null}
      <Form<ReasonFormValues> form={form} layout="vertical" onFinish={submit}>
        <Form.Item name="reason" label="操作原因（必填，将写入审计）" rules={[{ required: true, whitespace: true, message: '请填写操作原因' }]}>
          <Input.TextArea rows={3} maxLength={200} placeholder="例如：镜像异常，需要重建验证" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
