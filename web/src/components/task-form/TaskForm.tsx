import { useState, useEffect } from 'react'
import {
  Alert,
  Form,
  Input,
  Select,
  Switch,
  Card,
  Collapse,
  Button,
  InputNumber,
  message,
  Radio,
  Space,
  Spin,
  Steps,
  Typography,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined, ProfileOutlined, SettingOutlined, ThunderboltOutlined } from '@ant-design/icons'
import type { TaskDefinition } from '../../api/types'
import { dryRunTaskDefinition, fetchTaskDefinitions, testRunTaskDefinition, validateTaskDefinition } from '../../api/client'
import { driverForms } from './DriverParamsForms'
import { safeJson } from '../../utils/pulseops'

const { Text } = Typography

const KIND_OPTIONS = [
  { value: 'http_check', label: 'HTTP 检查', desc: 'URL、方法、状态码、Header、Body' },
  { value: 'tcp_check', label: 'TCP 检查', desc: 'host:port 连通性' },
  { value: 'script_exec', label: '脚本执行', desc: '命令、参数、工作目录、环境变量' },
  { value: 'process_check', label: '进程检查', desc: '本机进程存在性' },
  { value: 'scenario_check', label: '场景巡检', desc: '源接口、采样、fanout、evaluator' },
  { value: 'data_process', label: '数据处理', desc: '上游样本、字段选择、JQ、聚合' },
  { value: 'ai_analyze', label: 'AI 分析', desc: '数据源、Prompt、输出配置' },
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

function normalizeFormDefinition(values: Record<string, unknown>): TaskDefinition {
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

  return def as unknown as TaskDefinition
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
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [testLoading, setTestLoading] = useState(false)
  const [preview, setPreview] = useState<Record<string, unknown>>({})

  const kind = Form.useWatch('kind', form)
  const trigger = Form.useWatch('trigger', form)
  const formValues = Form.useWatch([], form)

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
      const taskDefinition = normalizeFormDefinition(values)
      await validateTaskDefinition(taskDefinition)
      await onSubmit(taskDefinition)
    } catch {
    } finally {
      setSubmitLoading(false)
    }
  }

  const handleDryRun = async () => {
    try {
      setTestLoading(true)
      const values = await form.validateFields()
      await dryRunTaskDefinition(normalizeFormDefinition(values))
      message.success('后端校验通过')
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '校验失败')
    } finally {
      setTestLoading(false)
    }
  }

  const handleTestRun = async () => {
    try {
      setTestLoading(true)
      const values = await form.validateFields()
      const run = await testRunTaskDefinition(normalizeFormDefinition(values))
      message.success(`试运行完成：${run.run_status}/${run.check_status}`)
      setPreview({ ...form.getFieldsValue(true), test_run: run })
      setAdvancedOpen(true)
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '试运行失败')
    } finally {
      setTestLoading(false)
    }
  }

  useEffect(() => {
    setPreview(form.getFieldsValue(true))
  }, [form, formValues])

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
  const currentStep = !kind ? 0 : trigger ? 2 : 1
  const selectedKind = KIND_OPTIONS.find((item) => item.value === kind)

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
      <Card className="ops-card" style={{ marginBottom: 16 }}>
        <Steps
          size="small"
          current={currentStep}
          items={[
            { title: '模板', icon: <ProfileOutlined /> },
            { title: '调度与参数', icon: <SettingOutlined /> },
            { title: '预览确认', icon: <ThunderboltOutlined /> },
          ]}
        />
      </Card>

      {mode === 'create' && (
        <Card className="ops-card" title="任务模板" style={{ marginBottom: 16 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 10 }}>
            {KIND_OPTIONS.map((item) => (
              <Button
                key={item.value}
                type={kind === item.value ? 'primary' : 'default'}
                style={{ height: 72, textAlign: 'left', justifyContent: 'flex-start' }}
                onClick={() => form.setFieldValue('kind', item.value)}
              >
                <span>
                  <strong>{item.label}</strong>
                  <Text type="secondary" style={{ display: 'block', marginTop: 4, whiteSpace: 'normal' }}>{item.desc}</Text>
                </span>
              </Button>
            ))}
          </div>
        </Card>
      )}

      {/* a) 基本信息 */}
      <Card className="ops-card" title="基本信息" style={{ marginBottom: 16 }}>
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

        <Form.Item name="kind" label="类型" rules={[{ required: true, message: '请选择任务类型' }]}>
          <Select
            options={KIND_OPTIONS.map((item) => ({ value: item.value, label: item.label }))}
            placeholder="选择任务类型"
          />
        </Form.Item>

        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Card>

      {/* b) 调度配置 */}
      <Card className="ops-card" title="调度 / 触发" style={{ marginBottom: 16 }}>
        <Form.Item name="trigger" label="触发类型">
          <Radio.Group>
            <Radio value="scheduled">定时</Radio>
            <Radio value="manual">手动</Radio>
            <Radio value="on_run">依赖触发</Radio>
          </Radio.Group>
        </Form.Item>

        <Form.Item
          name="interval"
          label="间隔"
          rules={[makeScheduleValidator()]}
          extra={trigger === 'manual' ? '手动任务可以不设置 interval 或 cron' : undefined}
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

        {trigger === 'on_run' && (
          <>
            <Form.Item
              name="watch_task_id"
              label="监听上游"
              rules={[{ required: true, message: '依赖触发时需要选择监听任务' }]}
            >
              <Select
                loading={taskDefsLoading}
                notFoundContent={
                  taskDefsLoading ? <Spin size="small" /> : '没有启用的任务'
                }
                placeholder="选择要监听的上游任务"
                options={taskDefs.map((d) => ({
                  value: d.task_id,
                  label: `${d.name} (${d.task_id})`,
                }))}
                showSearch
                optionFilterProp="label"
              />
            </Form.Item>

            <Form.Item name="watch_condition" label="触发条件">
              <Select
                allowClear
                placeholder="不限制（总是触发）"
                options={[
                  { value: 'check_status == pass', label: '上游检查通过时触发' },
                  { value: 'run_status == success', label: '上游运行成功时触发' },
                ]}
              />
            </Form.Item>
          </>
        )}
      </Card>

      {/* c) Params */}
      <Card
        className="ops-card"
        title={kind ? `${KIND_LABELS[kind] || kind} 参数` : '参数'}
        style={{ marginBottom: 16 }}
        extra={selectedKind ? <Text type="secondary">{selectedKind.desc}</Text> : null}
      >
        {DriverForm ? (
          <DriverForm form={form} />
        ) : (
          <div
            style={{ color: '#999', padding: '16px 0', textAlign: 'center' }}
          >
            选择任务类型以配置参数
          </div>
        )}
      </Card>

      {/* e) 标签 */}
      <Card className="ops-card" title="标签" style={{ marginBottom: 16 }}>
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
        style={{ marginBottom: 16 }}
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
        style={{ marginBottom: 16 }}
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

      <Collapse
        style={{ marginBottom: 16 }}
        activeKey={advancedOpen ? ['advanced'] : []}
        onChange={(keys) => setAdvancedOpen(Array.isArray(keys) ? keys.includes('advanced') : keys === 'advanced')}
        items={[
          {
            key: 'advanced',
            label: '高级模式：原始 JSON 预览',
            children: (
              <>
                <Alert
                  type="warning"
                  showIcon
                  message="高级模式会直接暴露底层字段"
                  description="保存前会调用后端 validate；也可以在提交前执行 dry-run 或 test-run 调试。"
                  style={{ marginBottom: 12 }}
                />
                <pre className="code-block">{safeJson(preview)}</pre>
              </>
            ),
          },
        ]}
      />

      {/* Submit */}
      <Form.Item style={{ textAlign: 'right' }}>
        <Space>
          <Text type="secondary">保存成功后返回来源页面</Text>
          <Button onClick={handleDryRun} loading={testLoading}>
            校验配置
          </Button>
          <Button onClick={handleTestRun} loading={testLoading}>
            试运行
          </Button>
          <Button type="primary" htmlType="submit" loading={submitLoading}>
            {mode === 'create' ? '创建任务' : '保存修改'}
          </Button>
        </Space>
      </Form.Item>
    </Form>
  )
}
