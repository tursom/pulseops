import { Form, Input, Select, Button, Space, Tooltip } from 'antd'
import { MinusCircleOutlined, PlusOutlined, QuestionCircleOutlined } from '@ant-design/icons'
import type { FormInstance } from 'antd'

const DATA_SOURCE_TYPE_OPTIONS = [
  { value: 'upstream_output', label: '上游任务输出' },
  { value: 'run_context', label: '触发上下文' },
  { value: 'run_history', label: '运行历史' },
  { value: 'previous_analysis', label: '历史分析' },
  { value: 'http_call', label: 'HTTP 调用' },
]

export default function AIAnalyzeParams(_props: { form?: FormInstance }) {
  const promptHint = `可用模板变量: {{.DataSources.<别名>.<字段>}}
辅助函数: {{json .}}, {{table . "col1" "col2"}}, {{len .}}, {{avg . "field"}}, {{failures .}}`

  return (
    <>
      {/* 分析类型 */}
      <Form.Item
        name={['params', 'analysis_type']}
        label="分析类型"
        rules={[{ required: true, message: '请选择分析类型' }]}
      >
        <Select
          options={[
            { value: 'diagnose', label: '诊断分析' },
            { value: 'trend', label: '趋势分析' },
            { value: 'validate', label: '数据校验' },
          ]}
        />
      </Form.Item>

      {/* 数据源列表 */}
      <Form.Item label="数据源">
        <Form.List name={['params', 'data_sources']}>
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name, ...restField }) => (
                <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                  <Form.Item
                    {...restField}
                    name={[name, 'type']}
                    rules={[{ required: true, message: '请选择类型' }]}
                    style={{ marginBottom: 0 }}
                  >
                    <Select
                      placeholder="类型"
                      style={{ width: 140 }}
                      options={DATA_SOURCE_TYPE_OPTIONS}
                    />
                  </Form.Item>

                  <Form.Item
                    {...restField}
                    name={[name, 'alias']}
                    style={{ marginBottom: 0 }}
                  >
                    <Input placeholder="别名（如 upstream）" style={{ width: 160 }} />
                  </Form.Item>

                  <Form.Item noStyle shouldUpdate={(prev, cur) => {
                    const prevType = prev.params?.data_sources?.[name]?.type
                    const curType = cur.params?.data_sources?.[name]?.type
                    return prevType !== curType
                  }}>
                    {({ getFieldValue }) => {
                      const dsType = getFieldValue(['params', 'data_sources', name, 'type'])
                      
                      if (dsType === 'run_history' || dsType === 'previous_analysis') {
                        return (
                          <Form.Item
                            {...restField}
                            name={[name, 'config', 'task_id']}
                            rules={[{ required: true, message: '需填 task_id' }]}
                            style={{ marginBottom: 0 }}
                          >
                            <Input placeholder="task_id" style={{ width: 140 }} />
                          </Form.Item>
                        )
                      }
                      
                      if (dsType === 'http_call') {
                        return (
                          <>
                            <Form.Item
                              {...restField}
                              name={[name, 'config', 'url']}
                              rules={[{ required: true, message: '需填 url' }]}
                              style={{ marginBottom: 0 }}
                            >
                              <Input placeholder="https://..." style={{ width: 180 }} />
                            </Form.Item>
                            <Form.Item
                              {...restField}
                              name={[name, 'config', 'method']}
                              initialValue="GET"
                              style={{ marginBottom: 0 }}
                            >
                              <Select style={{ width: 80 }} options={[
                                { value: 'GET', label: 'GET' },
                                { value: 'POST', label: 'POST' },
                              ]} />
                            </Form.Item>
                          </>
                        )
                      }
                      
                      return null
                    }}
                  </Form.Item>

                  <MinusCircleOutlined onClick={() => remove(name)} />
                </Space>
              ))}

              <Form.Item style={{ marginBottom: 0 }}>
                <Button type="dashed" onClick={() => add({ on_error: 'fail' })} block icon={<PlusOutlined />}>
                  添加数据源
                </Button>
              </Form.Item>
            </>
          )}
        </Form.List>
      </Form.Item>

      {/* 提示词 */}
      <Form.Item
        name={['params', 'prompt']}
        label={
          <Space size={4}>
            <span>提示词模板</span>
            <Tooltip title={<pre style={{ margin: 0, fontSize: 12 }}>{promptHint}</pre>}>
              <QuestionCircleOutlined style={{ color: '#999' }} />
            </Tooltip>
          </Space>
        }
      >
        <Input.TextArea
          rows={6}
          placeholder={`分析上游任务输出:\n{{json .DataSources.upstream}}\n\n判断是否存在异常。`}
        />
      </Form.Item>

      {/* 输出配置 */}
      <Form.Item name={['params', 'outputs']} label="输出配置">
        <Select mode="tags" placeholder="输出字段名" />
      </Form.Item>
    </>
  )
}
