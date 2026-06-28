import { useEffect, useState } from 'react'
import { Form, Input, Select, Button, Space } from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { fetchPluginCapabilities } from '../../../api/client'
import type { PluginCapability, PluginSchemaField } from '../../../api/types'

export default function ScenarioCheckParams() {
  const [evaluatorOptions, setEvaluatorOptions] = useState<Array<{ value: string; label: string }>>([
    { value: 'steam_game_price_consistency', label: 'Steam 游戏价格一致性' },
    { value: 'ai_evaluator', label: 'AI Evaluator' },
  ])
  const [capabilitiesByName, setCapabilitiesByName] = useState<Record<string, PluginCapability>>({})

  useEffect(() => {
    fetchPluginCapabilities('evaluator')
      .then((caps) => {
        const enabled = caps.filter((cap) => cap.enabled)
        const byName: Record<string, PluginCapability> = {}
        const next = enabled.map((cap) => {
          byName[cap.name] = cap
          return { value: cap.name, label: cap.title || cap.name }
        })
        if (next.length > 0) setEvaluatorOptions(next)
        setCapabilitiesByName(byName)
      })
      .catch(() => {})
  }, [])

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
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="选择 evaluator"
          options={evaluatorOptions}
        />
      </Form.Item>

      <Form.Item noStyle shouldUpdate={(prev, cur) => {
        return prev.params?.evaluator?.name !== cur.params?.evaluator?.name
      }}>
        {({ getFieldValue }) => {
          const evaluatorName = getFieldValue(['params', 'evaluator', 'name'])
          const capability = capabilitiesByName[evaluatorName]
          if (!capability?.schema || Object.keys(capability.schema).length === 0) return null
          return (
            <Space wrap align="baseline">
              {Object.entries(capability.schema).map(([field, schema]) => (
                <EvaluatorSchemaField key={field} field={field} schema={schema} />
              ))}
            </Space>
          )
        }}
      </Form.Item>

      <Form.Item name={['params', 'thresholds']} label="阈值">
        <Input.TextArea rows={3} placeholder="JSON 格式" />
      </Form.Item>
    </>
  )
}

function EvaluatorSchemaField({ field, schema }: { field: string; schema: PluginSchemaField }) {
  const rules = schema.required ? [{ required: true, message: `需填 ${field}` }] : undefined
  if (schema.type === 'object' || schema.type === 'array') {
    return (
      <Form.Item
        name={['params', 'evaluator', 'params', field]}
        label={field}
        rules={rules}
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
        <Input.TextArea rows={1} placeholder={schema.description || field} style={{ width: 240 }} />
      </Form.Item>
    )
  }
  return (
    <Form.Item name={['params', 'evaluator', 'params', field]} label={field} rules={rules}>
      <Input placeholder={schema.description || field} style={{ width: 220 }} />
    </Form.Item>
  )
}
