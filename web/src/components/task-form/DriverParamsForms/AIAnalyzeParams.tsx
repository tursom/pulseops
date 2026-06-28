import { useEffect, useState } from 'react'
import { Form, Input, Select, Button, Space, Tooltip } from 'antd'
import { MinusCircleOutlined, PlusOutlined, QuestionCircleOutlined } from '@ant-design/icons'
import type { FormInstance } from 'antd'
import { fetchPluginCapabilities } from '../../../api/client'
import type { PluginCapability, PluginSchemaField } from '../../../api/types'

interface DataSourceOption {
  value: string
  label: string
}

const DATA_SOURCE_TYPE_OPTIONS: DataSourceOption[] = [
  { value: 'upstream_output', label: '上游任务输出' },
  { value: 'run_context', label: '触发上下文' },
  { value: 'run_history', label: '运行历史' },
  { value: 'previous_analysis', label: '历史分析' },
  { value: 'http_call', label: 'HTTP 调用' },
]

export default function AIAnalyzeParams(_props: { form?: FormInstance }) {
  const [dataSourceOptions, setDataSourceOptions] = useState(DATA_SOURCE_TYPE_OPTIONS)
  const [capabilitiesByName, setCapabilitiesByName] = useState<Record<string, PluginCapability>>({})
  const [outputWriterOptions, setOutputWriterOptions] = useState([
    { value: 'summary', label: 'Summary' },
    { value: 'findings', label: 'Findings' },
    { value: 'artifact', label: 'Artifact' },
  ])
  const [outputCapabilitiesByName, setOutputCapabilitiesByName] = useState<Record<string, PluginCapability>>({})
  const promptHint = `可用模板变量: {{.DataSources.<别名>.<字段>}}
辅助函数: {{json .}}, {{table . "col1" "col2"}}, {{len .}}, {{avg . "field"}}, {{failures .}}`

  useEffect(() => {
    Promise.all([
      fetchPluginCapabilities('ai_data_source'),
      fetchPluginCapabilities('data_source'),
    ])
      .then(([aiCaps, dataSourceCaps]) => {
        const defaults = new Map(DATA_SOURCE_TYPE_OPTIONS.map((item) => [item.value, item]))
        const seen = new Set<string>()
        const capMap: Record<string, PluginCapability> = {}
        const next = [...aiCaps, ...dataSourceCaps]
          .filter((cap) => cap.enabled)
          .filter((cap) => {
            if (seen.has(cap.name)) return false
            seen.add(cap.name)
            return true
          })
          .map((cap) => ({
            value: cap.name,
            label: cap.title || defaults.get(cap.name)?.label || `${cap.name}${cap.protocol ? ` (${cap.protocol})` : ''}`,
          }))
        for (const cap of [...aiCaps, ...dataSourceCaps]) {
          if (cap.enabled) capMap[cap.name] = cap
        }
        if (next.length > 0) setDataSourceOptions(next)
        setCapabilitiesByName(capMap)
      })
      .catch(() => setDataSourceOptions(DATA_SOURCE_TYPE_OPTIONS))
  }, [])

  useEffect(() => {
    fetchPluginCapabilities('output_writer')
      .then((caps) => {
        const defaults = new Map(outputWriterOptions.map((item) => [item.value, item]))
        const enabled = caps.filter((cap) => cap.enabled)
        const byName: Record<string, PluginCapability> = {}
        const next = enabled.map((cap) => {
          byName[cap.name] = cap
          return { value: cap.name, label: cap.title || defaults.get(cap.name)?.label || cap.name }
        })
        if (next.length > 0) setOutputWriterOptions(next)
        setOutputCapabilitiesByName(byName)
      })
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
            { value: 'evaluate', label: '数据校验' },
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
                      options={dataSourceOptions}
                    />
                  </Form.Item>

                  <Form.Item
                    {...restField}
                    name={[name, 'alias']}
                    rules={[{ required: true, message: '请输入别名' }]}
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

                      const capability = capabilitiesByName[dsType]
                      if (capability?.schema && Object.keys(capability.schema).length > 0) {
                        return (
                          <Space wrap align="baseline">
                            {Object.entries(capability.schema).map(([field, schema]) => (
                              <SchemaConfigField
                                key={field}
                                field={field}
                                schema={schema}
                                name={name}
                                restField={restField}
                              />
                            ))}
                          </Space>
                        )
                      }
                      
                      return null
                    }}
                  </Form.Item>

                  <Form.Item
                    {...restField}
                    name={[name, 'on_error']}
                    initialValue="fail"
                    style={{ marginBottom: 0 }}
                  >
                    <Select
                      style={{ width: 100 }}
                      options={[
                        { value: 'fail', label: '失败' },
                        { value: 'skip', label: '跳过' },
                      ]}
                    />
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
        name={['params', 'prompt', 'text']}
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
      <Form.Item label="输出配置">
        <Form.List name={['params', 'outputs']}>
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name, ...restField }) => (
                <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                  <Form.Item
                    {...restField}
                    name={[name, 'type']}
                    initialValue="summary"
                    rules={[{ required: true, message: '请选择输出类型' }]}
                    style={{ marginBottom: 0 }}
                  >
                    <Select
                      style={{ width: 120 }}
                      options={outputWriterOptions}
                    />
                  </Form.Item>
                  <Form.Item noStyle shouldUpdate={(prev, cur) => {
                    const prevType = prev.params?.outputs?.[name]?.type
                    const curType = cur.params?.outputs?.[name]?.type
                    return prevType !== curType
                  }}>
                    {({ getFieldValue }) => {
                      const outputType = getFieldValue(['params', 'outputs', name, 'type'])
                      const capability = outputCapabilitiesByName[outputType]
                      if (capability?.schema && Object.keys(capability.schema).length > 0) {
                        return (
                          <Space wrap align="baseline">
                            {Object.entries(capability.schema).map(([field, schema]) => (
                              <SchemaConfigField
                                key={field}
                                field={field}
                                schema={schema}
                                name={name}
                                restField={restField}
                                root="outputs"
                              />
                            ))}
                          </Space>
                        )
                      }
                      return (
                        <Form.Item
                          {...restField}
                          name={[name, 'config', 'field']}
                          style={{ marginBottom: 0 }}
                        >
                          <Input placeholder="字段名，如 ai_analysis" style={{ width: 200 }} />
                        </Form.Item>
                      )
                    }}
                  </Form.Item>
                  <MinusCircleOutlined onClick={() => remove(name)} />
                </Space>
              ))}
              <Form.Item style={{ marginBottom: 0 }}>
                <Button type="dashed" onClick={() => add({ type: 'summary', config: { field: 'ai_analysis' } })} block icon={<PlusOutlined />}>
                  添加输出
                </Button>
              </Form.Item>
            </>
          )}
        </Form.List>
      </Form.Item>
    </>
  )
}

function SchemaConfigField({
  field,
  schema,
  name,
  restField,
  root = 'data_sources',
}: {
  field: string
  schema: PluginSchemaField
  name: number
  restField: Record<string, unknown>
  root?: 'data_sources' | 'outputs'
}) {
  const rules = schema.required ? [{ required: true, message: `需填 ${field}` }] : undefined
  const commonProps = {
    ...restField,
    name: root === 'outputs' ? [name, 'config', field] : [name, 'config', field],
    rules,
    style: { marginBottom: 0 },
  }
  if (schema.type === 'object' || schema.type === 'array') {
    return (
      <Form.Item
        {...commonProps}
        getValueFromEvent={(event) => {
          const raw = event.target.value.trim()
          if (!raw) return undefined
          try {
            return JSON.parse(raw)
          } catch {
            return event.target.value
          }
        }}
        getValueProps={(value) => ({
          value: value && typeof value === 'object' ? JSON.stringify(value, null, 2) : value,
        })}
      >
        <Input.TextArea rows={1} placeholder={schema.description || field} style={{ width: 220 }} />
      </Form.Item>
    )
  }
  return (
    <Form.Item {...commonProps}>
      <Input placeholder={schema.description || field} style={{ width: 180 }} />
    </Form.Item>
  )
}
