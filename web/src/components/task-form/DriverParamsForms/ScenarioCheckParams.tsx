import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function ScenarioCheckParams() {
  return (
    <>
      <Form.Item
        name={['params', 'source', 'url']}
        label="源数据URL"
        rules={[{ required: true, message: '请输入源数据URL' }]}
      >
        <Input placeholder="https://api.example.com/sample" />
      </Form.Item>

      <Form.Item
        name={['params', 'source', 'method']}
        label="请求方法"
        initialValue="GET"
      >
        <Select
          options={[
            { value: 'GET', label: 'GET' },
            { value: 'POST', label: 'POST' },
            { value: 'PUT', label: 'PUT' },
            { value: 'DELETE', label: 'DELETE' },
          ]}
        />
      </Form.Item>

      <Form.Item label="请求头">
        <Form.List name={['params', 'source', 'headers']}>
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
                添加请求头
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>

      <Form.Item name={['params', 'sample']} label="采样">
        <Input.TextArea rows={3} placeholder="JSON 格式" />
      </Form.Item>

      <Form.Item name={['params', 'fanout', 'url']} label="分发URL">
        <Input placeholder="https://api.example.com/resolve" />
      </Form.Item>

      <Form.Item
        name={['params', 'fanout', 'method']}
        label="请求方法"
        initialValue="GET"
      >
        <Select
          options={[
            { value: 'GET', label: 'GET' },
            { value: 'POST', label: 'POST' },
            { value: 'PUT', label: 'PUT' },
            { value: 'DELETE', label: 'DELETE' },
          ]}
        />
      </Form.Item>

      <Form.Item name={['params', 'evaluator', 'name']} label="评估器名称">
        <Input placeholder="如 equality_check" />
      </Form.Item>

      <Form.Item name={['params', 'thresholds']} label="阈值">
        <Input.TextArea rows={3} placeholder="JSON 格式" />
      </Form.Item>
    </>
  )
}
