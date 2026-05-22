import { Form, Input } from 'antd'

export default function TCPCheckParams() {
  return (
    <Form.Item
      name={['params', 'address']}
      label="地址"
      rules={[{ required: true, message: '请输入地址' }]}
    >
      <Input placeholder="host:port" />
    </Form.Item>
  )
}
