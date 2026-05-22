import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function HTTPCheckParams() {
  return (
    <>
      <Form.Item
        name={['params', 'url']}
        label="URL地址"
        rules={[{ required: true, message: '请输入URL地址' }]}
      >
        <Input placeholder="https://example.com/health" />
      </Form.Item>

      <Form.Item name={['params', 'method']} label="请求方法" initialValue="GET">
        <Select
          options={[
            { value: 'GET', label: 'GET' },
            { value: 'POST', label: 'POST' },
            { value: 'PUT', label: 'PUT' },
            { value: 'DELETE', label: 'DELETE' },
            { value: 'HEAD', label: 'HEAD' },
          ]}
        />
      </Form.Item>

      <Form.Item label="请求头">
        <Form.List name={['params', 'headers']}>
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name, ...restField }) => (
                <Space
                  key={key}
                  align="baseline"
                  style={{ display: 'flex', marginBottom: 8 }}
                >
                  <Form.Item
                    {...restField}
                    name={[name, 'key']}
                    rules={[{ required: true, message: '请输入请求头名称' }]}
                  >
                    <Input placeholder="请求头名称" />
                  </Form.Item>
                  <Form.Item
                    {...restField}
                    name={[name, 'value']}
                    rules={[{ required: true, message: '请输入值' }]}
                  >
                    <Input placeholder="值" />
                  </Form.Item>
                  <MinusCircleOutlined onClick={() => remove(name)} />
                </Space>
              ))}
              <Button
                type="dashed"
                onClick={() => add()}
                block
                icon={<PlusOutlined />}
              >
                添加请求头
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>

      <Form.Item name={['params', 'body']} label="请求体">
        <Input.TextArea rows={4} placeholder="请求体 (JSON)" />
      </Form.Item>

      <Form.Item name={['params', 'expect_status']} label="期望状态码">
        <Select mode="tags" placeholder="如 200, 201" />
      </Form.Item>

      <Form.Item name={['params', 'expect_body_contains']} label="期望响应包含">
        <Input placeholder="响应体中应包含的文本" />
      </Form.Item>
    </>
  )
}
