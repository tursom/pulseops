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
  { value: 'http_check', label: 'HTTP 检查' },
  { value: 'tcp_check', label: 'TCP 检查' },
  { value: 'script_exec', label: '脚本执行' },
  { value: 'process_check', label: '进程检查' },
  { value: 'scenario_check', label: '场景检查' },
  { value: 'ai_analyze', label: 'AI 分析' },
  { value: 'data_process', label: '数据处理' },
]

const KIND_LABELS: Record<string, string> = {
  http_check: 'HTTP 检查',
  tcp_check: 'TCP 检查',
  script_exec: '脚本执行',
  process_check: '进程检查',
  scenario_check: '场景检查',
  ai_analyze: 'AI 分析',
  data_process: '数据处理',
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
          new Error('不能同时设置间隔和Cron表达式'),
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
      {/* a) 基本信息 */}
      <Card title="基本信息" style={{ marginBottom: 24 }}>
        <Form.Item name="task_id" label="任务ID">
          <Input disabled placeholder="自动生成" />
        </Form.Item>

        <Form.Item
          name="name"
          label="名称"
          rules={[{ required: true, message: '请输入名称' }]}
        >
          <Input placeholder="任务显示名称" />
        </Form.Item>

        <Form.Item
          name="kind"
          label="类型"
          rules={[{ required: true, message: '请选择任务类型' }]}
        >
          <Select options={KIND_OPTIONS} placeholder="选择任务类型" />
        </Form.Item>

        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Card>

      {/* b) 调度配置 */}
      <Card title="调度配置" style={{ marginBottom: 24 }}>
        <Form.Item
          name="interval"
          label="间隔"
          rules={[makeScheduleValidator()]}
        >
          <Input placeholder="如 30s, 5m, 1h" />
        </Form.Item>

        <Form.Item
          name="cron"
          label="Cron表达式"
          rules={[makeScheduleValidator()]}
        >
          <Input placeholder="如 0 */6 * * *" />
        </Form.Item>

        <Form.Item name="timeout" label="超时">
          <Input placeholder="如 10s" />
        </Form.Item>
      </Card>

      {/* c) Params */}
      <Card
          title={kind ? `${KIND_LABELS[kind] || kind} 参数` : '参数'}
        style={{ marginBottom: 24 }}
      >
        {DriverForm ? (
          <DriverForm />
        ) : (
          <div
            style={{ color: '#999', padding: '16px 0', textAlign: 'center' }}
          >
            选择任务类型以配置参数
          </div>
        )}
      </Card>

      {/* d) 触发器 */}
      <Card title="触发器" style={{ marginBottom: 24 }}>
        <Form.Item name="trigger" label="触发类型">
          <Radio.Group>
            <Radio value="scheduled">定时</Radio>
            <Radio value="manual">手动</Radio>
            <Radio value="on_run">依赖触发</Radio>
          </Radio.Group>
        </Form.Item>

        {trigger === 'on_run' && (
          <>
            <Form.Item
              name="watch_task_id"
              label="监听任务"
              rules={[{ required: true, message: '依赖触发时需要选择监听任务' }]}
            >
              <Select
                loading={taskDefsLoading}
                notFoundContent={
                  taskDefsLoading ? <Spin size="small" /> : '没有启用的任务'
                }
                placeholder="选择要监听的任务"
                options={taskDefs.map((d) => ({
                  value: d.task_id,
                  label: `${d.name} (${d.task_id})`,
                }))}
                showSearch
                optionFilterProp="label"
              />
            </Form.Item>

            <Form.Item name="watch_condition" label="触发条件">
              <Input placeholder="如 status == 'success'" />
            </Form.Item>
          </>
        )}
      </Card>

      {/* e) 标签 */}
      <Card title="标签" style={{ marginBottom: 24 }}>
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
                    rules={[{ required: true, message: '请输入键' }]}
                  >
                    <Input placeholder="键" />
                  </Form.Item>
                  <Form.Item
                    {...restField}
                    name={[name, 'value']}
                    rules={[{ required: true, message: '请输入值' }]}
                  >
                    <Input placeholder="值" />
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
                添加标签
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
            label: '追踪策略',
            children: (
              <>
                <Form.Item name={['trace', 'level']} label="级别">
                  <Select
                    options={[
                      { value: 'off', label: '关闭' },
                      { value: 'summary', label: '摘要' },
                      { value: 'detail', label: '详细' },
                      { value: 'debug', label: '调试' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name={['trace', 'retain_days']} label="保留天数">
                  <InputNumber min={0} style={{ width: '100%' }} placeholder="默认30天" />
                </Form.Item>
                <Form.Item name={['trace', 'mask_fields']} label="脱敏字段">
                  <Select mode="tags" placeholder="要脱敏的字段名" />
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
            label: '告警策略',
            children: (
              <>
                <Form.Item
                  name={['alert', 'consecutive_failures']}
                  label="连续失败次数"
                >
                  <InputNumber min={1} style={{ width: '100%' }} />
                </Form.Item>

                <Form.Item name={['alert', 'channels']} label="通知渠道">
                  <Select
                    mode="tags"
                    placeholder="如 slack, email"
                  />
                </Form.Item>

                <Form.Item
                  name={['alert', 'recover_notify']}
                  label="恢复通知"
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
          {mode === 'create' ? '创建任务' : '保存修改'}
        </Button>
      </Form.Item>
    </Form>
  )
}
