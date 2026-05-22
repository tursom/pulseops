import { Form, Input } from 'antd'

export default function ProcessCheckParams() {
  return (
    <Form.Item
      name={['params', 'name']}
      label="进程名"
      rules={[{ required: true, message: '请输入进程名' }]}
    >
      <Input placeholder="进程名称" />
    </Form.Item>
  )
}
