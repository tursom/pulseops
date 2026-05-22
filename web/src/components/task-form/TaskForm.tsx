import { useState, useEffect } from 'react'
import {
  Form,
  Input,
  Select,
  Switch,
  Card,
  Collapse,
  Button,
  InputNumber,
  Radio,
  Space,
  Spin,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import type { TaskDefinition } from '../../api/types'
import { fetchTaskDefinitions } from '../../api/client'
import { driverForms } from './DriverParamsForms'

const KIND_OPTIONS = [
  { value: 'http_check', label: 'HTTP Check' },
  { value: 'tcp_check', label: 'TCP Check' },
  { value: 'script_exec', label: 'Script Exec' },
  { value: 'process_check', label: 'Process Check' },
  { value: 'scenario_check', label: 'Scenario Check' },
  { value: 'ai_analyze', label: 'AI Analyze' },
]

const KIND_LABELS: Record<string, string> = {
  http_check: 'HTTP Check',
  tcp_check: 'TCP Check',
  script_exec: 'Script Exec',
  process_check: 'Process Check',
  scenario_check: 'Scenario Check',
  ai_analyze: 'AI Analyze',
}

function formListToRecord(
  arr: { key: string; value: string }[] | undefined,
): Record<string, string> {
  if (!arr) return {}
  return Object.fromEntries(
    arr
      .filter((item) => item.key)
      .map((item) => [item.key, item.value || '']),
  )
}

interface TaskFormProps {
  initialValues?: Record<string, unknown>
  mode: 'create' | 'edit'
  onSubmit: (def: TaskDefinition) => Promise<void>
}

export default function TaskForm({
  initialValues,
  mode,
  onSubmit,
}: TaskFormProps) {
  const [form] = Form.useForm()
  const [submitLoading, setSubmitLoading] = useState(false)
  const [taskDefs, setTaskDefs] = useState<TaskDefinition[]>([])
  const [taskDefsLoading, setTaskDefsLoading] = useState(false)

  const kind = Form.useWatch('kind', form)
  const trigger = Form.useWatch('trigger', form)

  useEffect(() => {
    setTaskDefsLoading(true)
    fetchTaskDefinitions()
      .then((defs) => setTaskDefs(defs.filter((d) => d.enabled)))
      .catch(() => {})
      .finally(() => setTaskDefsLoading(false))
  }, [])

  const handleFinish = async () => {
    try {
      setSubmitLoading(true)
      const values = await form.validateFields()

      const def = { ...values } as Record<string, unknown>

      if (Array.isArray(def.labels)) {
        def.labels = formListToRecord(
          def.labels as { key: string; value: string }[],
        )
      }

      const params = def.params as Record<string, unknown> | undefined
      if (params) {
        if (Array.isArray(params.headers)) {
          params.headers = formListToRecord(
            params.headers as { key: string; value: string }[],
          )
        }
        if (Array.isArray(params.env)) {
          params.env = formListToRecord(
            params.env as { key: string; value: string }[],
          )
        }
        const source = params.source as Record<string, unknown> | undefined
        if (source && Array.isArray(source.headers)) {
          source.headers = formListToRecord(
            source.headers as { key: string; value: string }[],
          )
        }
      }

      await onSubmit(def as unknown as TaskDefinition)
    } catch {
    } finally {
      setSubmitLoading(false)
    }
  }

  const makeScheduleValidator = () => ({
    validator(_: unknown, value: string) {
      if (!value) return Promise.resolve()
      const interval = form.getFieldValue('interval')
      const cron = form.getFieldValue('cron')
      if (interval && cron) {
        return Promise.reject(
          new Error('Cannot set both interval and cron — choose one'),
        )
      }
      return Promise.resolve()
    },
  })

  const DriverForm = kind ? driverForms[kind] : null

  return (
    <Form
      form={form}
      layout="horizontal"
      labelCol={{ xs: 24, sm: 6 }}
      wrapperCol={{ xs: 24, sm: 18 }}
      onFinish={handleFinish}
      initialValues={initialValues}
      scrollToFirstError
    >
      {/* a) Basic Info */}
      <Card title="Basic Info" style={{ marginBottom: 24 }}>
        <Form.Item
          name="task_id"
          label="Task ID"
          rules={
            mode === 'create'
              ? [{ required: true, message: 'Task ID is required' }]
              : []
          }
        >
          <Input disabled={mode === 'edit'} placeholder="e.g. my-health-check" />
        </Form.Item>

        <Form.Item
          name="name"
          label="Name"
          rules={[{ required: true, message: 'Name is required' }]}
        >
          <Input placeholder="Task display name" />
        </Form.Item>

        <Form.Item
          name="kind"
          label="Kind"
          rules={[{ required: true, message: 'Kind is required' }]}
        >
          <Select options={KIND_OPTIONS} placeholder="Select task kind" />
        </Form.Item>

        <Form.Item name="enabled" label="Enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Card>

      {/* b) Schedule */}
      <Card title="Schedule" style={{ marginBottom: 24 }}>
        <Form.Item
          name="interval"
          label="Interval"
          rules={[makeScheduleValidator()]}
        >
          <Input placeholder="e.g. 30s, 5m, 1h" />
        </Form.Item>

        <Form.Item
          name="cron"
          label="Cron"
          rules={[makeScheduleValidator()]}
        >
          <Input placeholder="e.g. 0 */6 * * *" />
        </Form.Item>

        <Form.Item name="timeout" label="Timeout">
          <Input placeholder="e.g. 10s" />
        </Form.Item>
      </Card>

      {/* c) Params */}
      <Card
        title={kind ? `${KIND_LABELS[kind] || kind} Params` : 'Params'}
        style={{ marginBottom: 24 }}
      >
        {DriverForm ? (
          <DriverForm />
        ) : (
          <div
            style={{ color: '#999', padding: '16px 0', textAlign: 'center' }}
          >
            Select a task kind to configure parameters
          </div>
        )}
      </Card>

      {/* d) Trigger */}
      <Card title="Trigger" style={{ marginBottom: 24 }}>
        <Form.Item name="trigger" label="Trigger Type">
          <Radio.Group>
            <Radio value="scheduled">Scheduled</Radio>
            <Radio value="manual">Manual</Radio>
            <Radio value="on_run">On Run</Radio>
          </Radio.Group>
        </Form.Item>

        {trigger === 'on_run' && (
          <>
            <Form.Item
              name="watch_task_id"
              label="Watch Task"
              rules={[{ required: true, message: 'Watch task is required when trigger is On Run' }]}
            >
              <Select
                loading={taskDefsLoading}
                notFoundContent={
                  taskDefsLoading ? <Spin size="small" /> : 'No enabled tasks'
                }
                placeholder="Select a task to watch"
                options={taskDefs.map((d) => ({
                  value: d.task_id,
                  label: `${d.name} (${d.task_id})`,
                }))}
                showSearch
                optionFilterProp="label"
              />
            </Form.Item>

            <Form.Item name="watch_condition" label="Watch Condition">
              <Input placeholder="e.g. status == 'success'" />
            </Form.Item>
          </>
        )}
      </Card>

      {/* e) Labels */}
      <Card title="Labels" style={{ marginBottom: 24 }}>
        <Form.List name="labels">
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
                    <Input placeholder="Key" />
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
                Add Label
              </Button>
            </>
          )}
        </Form.List>
      </Card>

      {/* f) Trace Policy */}
      <Collapse
        style={{ marginBottom: 24 }}
        items={[
          {
            key: 'trace',
            label: 'Trace Policy',
            children: (
              <>
                <Form.Item
                  name={['trace', 'enabled']}
                  label="Enabled"
                  valuePropName="checked"
                >
                  <Switch />
                </Form.Item>

                <Form.Item name={['trace', 'level']} label="Level">
                  <Select
                    options={[
                      { value: 'none', label: 'None' },
                      { value: 'summary', label: 'Summary' },
                      { value: 'detail', label: 'Detail' },
                      { value: 'debug', label: 'Debug' },
                    ]}
                  />
                </Form.Item>

                <Form.Item name={['trace', 'sinks']} label="Sinks">
                  <Select mode="tags" placeholder="e.g. console, file" />
                </Form.Item>

                <Form.Item name={['trace', 'retain_days']} label="Retain Days">
                  <InputNumber min={0} style={{ width: '100%' }} />
                </Form.Item>

                <Form.Item
                  name={['trace', 'store_stdout']}
                  label="Store Stdout"
                  valuePropName="checked"
                >
                  <Switch />
                </Form.Item>

                <Form.Item
                  name={['trace', 'store_stderr']}
                  label="Store Stderr"
                  valuePropName="checked"
                >
                  <Switch />
                </Form.Item>

                <Form.Item
                  name={['trace', 'store_result_payload']}
                  label="Store Result Payload"
                  valuePropName="checked"
                >
                  <Switch />
                </Form.Item>

                <Form.Item
                  name={['trace', 'max_payload_bytes']}
                  label="Max Payload Bytes"
                >
                  <InputNumber min={0} style={{ width: '100%' }} />
                </Form.Item>

                <Form.Item name={['trace', 'mask_fields']} label="Mask Fields">
                  <Select mode="tags" placeholder="Field names to mask" />
                </Form.Item>
              </>
            ),
          },
        ]}
      />

      {/* g) Alert Policy */}
      <Collapse
        style={{ marginBottom: 24 }}
        items={[
          {
            key: 'alert',
            label: 'Alert Policy',
            children: (
              <>
                <Form.Item
                  name={['alert', 'consecutive_failures']}
                  label="Consecutive Failures"
                >
                  <InputNumber min={1} style={{ width: '100%' }} />
                </Form.Item>

                <Form.Item name={['alert', 'channels']} label="Channels">
                  <Select
                    mode="tags"
                    placeholder="e.g. slack, email"
                  />
                </Form.Item>

                <Form.Item
                  name={['alert', 'recover_notify']}
                  label="Recover Notify"
                  valuePropName="checked"
                >
                  <Switch />
                </Form.Item>
              </>
            ),
          },
        ]}
      />

      {/* Submit */}
      <Form.Item style={{ textAlign: 'right' }}>
        <Button type="primary" htmlType="submit" loading={submitLoading}>
          {mode === 'create' ? 'Create Task' : 'Update Task'}
        </Button>
      </Form.Item>
    </Form>
  )
}
