import { Form, Input } from 'antd'

export default function ProcessCheckParams() {
  return (
    <Form.Item
      name={['params', 'name']}
      label="Process Name"
      rules={[{ required: true, message: 'Process name is required' }]}
    >
      <Input placeholder="process name" />
    </Form.Item>
  )
}
