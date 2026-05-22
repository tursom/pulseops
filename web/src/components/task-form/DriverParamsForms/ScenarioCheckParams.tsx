import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function ScenarioCheckParams() {
  return (
    <>
      <Form.Item
        name={['params', 'source', 'url']}
        label="Source URL"
        rules={[{ required: true, message: 'Source URL is required' }]}
      >
        <Input placeholder="https://api.example.com/sample" />
      </Form.Item>

      <Form.Item
        name={['params', 'source', 'method']}
        label="Source Method"
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

      <Form.Item label="Source Headers">
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
                Add Header
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>

      <Form.Item name={['params', 'sample']} label="Sample">
        <Input.TextArea rows={3} placeholder="Sample data (JSON)" />
      </Form.Item>

      <Form.Item name={['params', 'fanout', 'url']} label="Fanout URL">
        <Input placeholder="https://api.example.com/resolve" />
      </Form.Item>

      <Form.Item
        name={['params', 'fanout', 'method']}
        label="Fanout Method"
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

      <Form.Item name={['params', 'evaluator', 'name']} label="Evaluator Name">
        <Input placeholder="e.g. equality_check" />
      </Form.Item>

      <Form.Item name={['params', 'thresholds']} label="Thresholds">
        <Input.TextArea rows={3} placeholder="Thresholds (JSON)" />
      </Form.Item>
    </>
  )
}
