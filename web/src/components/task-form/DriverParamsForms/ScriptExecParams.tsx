import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function ScriptExecParams() {
  return (
    <>
      <Form.Item
        name={['params', 'command']}
        label="Command"
        rules={[{ required: true, message: 'Command is required' }]}
      >
        <Input placeholder="/usr/local/bin/check.sh" />
      </Form.Item>

      <Form.Item name={['params', 'args']} label="Args">
        <Select mode="tags" placeholder="Command arguments" />
      </Form.Item>

      <Form.Item name={['params', 'work_dir']} label="Work Dir">
        <Input placeholder="/opt/app" />
      </Form.Item>

      <Form.Item label="Env">
        <Form.List name={['params', 'env']}>
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
                    rules={[{ required: true, message: 'Key required' }]}
                  >
                    <Input placeholder="VAR_NAME" />
                  </Form.Item>
                  <Form.Item
                    {...restField}
                    name={[name, 'value']}
                    rules={[{ required: true, message: 'Value required' }]}
                  >
                    <Input placeholder="value" />
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
                Add Env Var
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>
    </>
  )
}
