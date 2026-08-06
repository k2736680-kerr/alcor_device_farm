import { App as AntApp, Button, Card, Form, Input, Typography } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
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
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card style={{ width: 360 }}>
        <Typography.Title level={4} style={{ textAlign: 'center', marginTop: 0 }}>
          设备农场控制台登录
        </Typography.Title>
        <Form<LoginValues> layout="vertical" onFinish={onSubmit}>
          <Form.Item name="user_id" label="用户 ID" rules={[{ required: true, message: '请输入用户 ID' }]}>
            <Input prefix={<UserOutlined />} autoComplete="username" placeholder="alice" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" placeholder="••••••••" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={login.isPending}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  )
}
