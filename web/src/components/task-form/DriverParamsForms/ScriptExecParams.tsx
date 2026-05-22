import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'

export default function ScriptExecParams() {
  return (
    <>
      <Form.Item
        name={['params', 'command']}
        label="命令"
        rules={[{ required: true, message: '请输入命令' }]}
      >
        <Input placeholder="可执行命令路径" />
      </Form.Item>

      <Form.Item name={['params', 'args']} label="参数">
        <Select mode="tags" placeholder="命令行参数" />
      </Form.Item>

      <Form.Item name={['params', 'work_dir']} label="工作目录">
        <Input placeholder="/opt/app" />
      </Form.Item>

      <Form.Item label="环境变量">
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
                添加环境变量
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>
    </>
  )
}
