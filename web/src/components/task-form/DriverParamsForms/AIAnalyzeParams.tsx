import { Form, Input, Select } from 'antd'

export default function AIAnalyzeParams() {
  return (
    <>
      <Form.Item
        name={['params', 'analysis_type']}
        label="Analysis Type"
        rules={[{ required: true, message: 'Analysis type is required' }]}
      >
        <Select
          options={[
            { value: 'diagnose', label: 'Diagnose' },
            { value: 'trend', label: 'Trend' },
            { value: 'validate', label: 'Validate' },
          ]}
        />
      </Form.Item>

      <Form.Item name={['params', 'data_sources']} label="Data Sources">
        <Select mode="tags" placeholder="e.g. prometheus, logs" />
      </Form.Item>

      <Form.Item name={['params', 'prompt']} label="Prompt">
        <Input.TextArea rows={6} placeholder="AI analysis prompt" />
      </Form.Item>

      <Form.Item name={['params', 'outputs']} label="Outputs">
        <Select mode="tags" placeholder="Output field names" />
      </Form.Item>
    </>
  )
}
