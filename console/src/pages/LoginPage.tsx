import { App as AntApp, Button, Card, Form, Input, Typography } from 'antd'
import { CloudServerOutlined, LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import { getGetConsoleSessionQueryKey, useCreateConsoleSession } from '../api/generated/device-farm'

interface LoginValues {
  user_id: string
  password: string
}

export function LoginPage() {
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const login = useCreateConsoleSession({
    mutation: {
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: getGetConsoleSessionQueryKey() })
      },
      onError: (error) => {
        message.error(error instanceof Error ? error.message : '登录失败')
      },
    },
  })

  const onSubmit = (values: LoginValues) => {
    login.mutate({ data: values })
  }

  return (
    <div className="login-shell">
      <section className="login-hero">
        <div className="login-brand"><CloudServerOutlined /> ALCOR DEVICE FARM</div>
        <div className="login-hero-copy">
          <Typography.Title>统一管理每一台<br />测试设备</Typography.Title>
          <Typography.Paragraph>
            查看宿主机容量、设备健康与预约状态，集中完成设备域日常运维与调度。
          </Typography.Paragraph>
          <div className="login-feature-list">
            <span><i /> 固定暖池与自动回收</span>
            <span><i /> 设备状态与审计可追踪</span>
            <span><i /> 浏览器不接触内部 Token</span>
          </div>
        </div>
        <div className="login-hero-foot"><SafetyCertificateOutlined /> 设备域独立控制面</div>
      </section>
      <section className="login-panel">
        <Card className="login-card" variant="borderless">
          <div className="login-card-kicker">WELCOME BACK</div>
          <Typography.Title level={2}>设备农场控制台登录</Typography.Title>
          <Typography.Paragraph type="secondary">使用管理员账户进入设备资源控制台</Typography.Paragraph>
        <Form<LoginValues> layout="vertical" onFinish={onSubmit}>
          <Form.Item name="user_id" label="用户 ID" rules={[{ required: true, message: '请输入用户 ID' }]}>
            <Input size="large" prefix={<UserOutlined />} autoComplete="username" placeholder="请输入用户 ID" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password size="large" prefix={<LockOutlined />} autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Button className="login-submit" type="primary" size="large" htmlType="submit" block loading={login.isPending}>
            登录
          </Button>
        </Form>
          <div className="login-security-note"><SafetyCertificateOutlined /> 登录会话受 CSRF、过期和空闲超时保护</div>
        </Card>
      </section>
    </div>
  )
}
