import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function HTTPCheckParams() {
  return (
    <>
      <Form.Item
        name={['params', 'url']}
        label="URL"
        rules={[{ required: true, message: 'URL is required' }]}
      >
        <Input placeholder="https://example.com/health" />
      </Form.Item>

      <Form.Item name={['params', 'method']} label="Method" initialValue="GET">
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

      <Form.Item label="Headers">
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
                    rules={[{ required: true, message: 'Header name required' }]}
                  >
                    <Input placeholder="Header name" />
                  </Form.Item>
                  <Form.Item
                    {...restField}
                    name={[name, 'value']}
                    rules={[{ required: true, message: 'Value required' }]}
                  >
                    <Input placeholder="Value" />
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
                Add Header
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>

      <Form.Item name={['params', 'body']} label="Body">
        <Input.TextArea rows={4} placeholder="Request body (JSON)" />
      </Form.Item>

      <Form.Item name={['params', 'expect_status']} label="Expect Status">
        <Select mode="tags" placeholder="e.g. 200, 201" />
      </Form.Item>

      <Form.Item name={['params', 'expect_body_contains']} label="Expect Body Contains">
        <Input placeholder="Text expected in response body" />
      </Form.Item>
    </>
  )
}
