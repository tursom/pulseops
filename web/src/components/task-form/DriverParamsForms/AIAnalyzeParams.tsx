import { Form, Input, Select } from 'antd'

export default function AIAnalyzeParams() {
  return (
    <>
      <Form.Item
        name={['params', 'analysis_type']}
        label="分析类型"
        rules={[{ required: true, message: '请选择分析类型' }]}
      >
        <Select
          options={[
            { value: 'diagnose', label: 'Diagnose' },
            { value: 'trend', label: 'Trend' },
            { value: 'validate', label: 'Validate' },
          ]}
        />
      </Form.Item>

      <Form.Item name={['params', 'data_sources']} label="数据源">
        <Select mode="tags" placeholder="如 prometheus, logs" />
      </Form.Item>

      <Form.Item name={['params', 'prompt']} label="提示词">
        <Input.TextArea rows={6} placeholder="AI 分析提示词" />
      </Form.Item>

      <Form.Item name={['params', 'outputs']} label="输出字段">
        <Select mode="tags" placeholder="输出字段名" />
      </Form.Item>
    </>
  )
}
