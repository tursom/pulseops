import { Form, Input } from 'antd'

export default function TCPCheckParams() {
  return (
    <Form.Item
      name={['params', 'address']}
      label="Address"
      rules={[{ required: true, message: 'Address is required' }]}
    >
      <Input placeholder="host:port" />
    </Form.Item>
  )
}
