import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Select,
  Space,
  Spin,
  Steps,
  Switch,
  Tag,
  Typography,
  message,
} from 'antd'
import type { NamePath } from 'antd/es/form/interface'
import {
  CheckCircleOutlined,
  DatabaseOutlined,
  ProfileOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { PluginCapability, RunRecord, TaskDefinition, TaskValidationResponse } from '../../api/types'
import {
  dryRunTaskDefinition,
  fetchPluginCapabilities,
  fetchTaskDefinitions,
  PulseOpsAPIError,
  testRunTaskDefinition,
  validateTaskDefinition,
} from '../../api/client'
import { driverForms } from './DriverParamsForms'
import { safeJson } from '../../utils/pulseops'
import {
  buildTopologyDependency,
  buildTopologyParams,
  TASK_CAPABILITIES,
  TOPOLOGY_DEFAULT_CONDITION,
  TOPOLOGY_SOURCE_KEY,
  type TaskCreationContext,
} from '../../utils/taskCreationDefaults'
import { useWatchedFormValue } from './useWatchedFormValue'

const { Text } = Typography

const DEFAULT_KIND_OPTIONS = [
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

const AI_BUILTIN_ALIASES = new Set(['run_context', 'run_history', 'previous_analysis', 'http_call'])

type KindOption = { value: string; label: string; desc: string }
type TemplateOption = KindOption & {
  templateId: string
  pluginName?: string
  defaults?: Record<string, unknown>
  params?: Record<string, unknown>
}

type StepKey = 'template' | 'basic' | 'trigger' | 'params' | 'data_sources' | 'observability' | 'preview'

interface WizardStep {
  key: StepKey
  title: string
  icon: React.ReactNode
}

interface KeyValueItem {
  key?: string
  value?: string
}

function formListToRecord(arr: KeyValueItem[] | undefined): Record<string, string> {
  if (!arr) return {}
  return Object.fromEntries(
    arr
      .filter((item) => item.key)
      .map((item) => [item.key || '', item.value || '']),
  )
}

function recordToFormList(record: Record<string, unknown> | undefined): KeyValueItem[] | undefined {
  if (!record) return undefined
  return Object.entries(record).map(([key, value]) => ({ key, value: String(value ?? '') }))
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function buildTemplateOptions(templateCaps: PluginCapability[], kindOptions: KindOption[]): TemplateOption[] {
  const kinds = new Map(kindOptions.map((item) => [item.value, item]))
  const templates = templateCaps
    .filter((cap) => cap.enabled && cap.kind)
    .map((cap) => {
      const fallback = kinds.get(cap.kind || cap.name)
      return {
        value: cap.kind || cap.name,
        templateId: cap.name,
        label: cap.title || fallback?.label || cap.kind || cap.name,
        desc: cap.description || fallback?.desc || `${cap.plugin_name} · ${cap.plugin_version}`,
        pluginName: cap.plugin_name,
        defaults: isPlainRecord(cap.defaults) ? cap.defaults : undefined,
        params: isPlainRecord(cap.params) ? cap.params : undefined,
      }
    })
  if (templates.length > 0) return templates
  return kindOptions.map((item) => ({ ...item, templateId: item.value }))
}

function cloneRecord(value: Record<string, unknown>): Record<string, unknown> {
  return JSON.parse(JSON.stringify(value || {})) as Record<string, unknown>
}

function parseJSONField(value: unknown): unknown {
  if (typeof value !== 'string') return value
  const trimmed = value.trim()
  if (!trimmed) return undefined
  try {
    return JSON.parse(trimmed)
  } catch {
    return value
  }
}

function normalizeMapList(value: unknown): Record<string, string> | unknown {
  if (Array.isArray(value)) {
    return formListToRecord(value as KeyValueItem[])
  }
  return value
}

function normalizeOutputs(value: unknown): unknown {
  if (!Array.isArray(value)) return value
  return value
    .filter((item) => item)
    .map((item) => {
      if (typeof item === 'string') {
        return { type: 'summary', config: { field: item } }
      }
      if (isPlainRecord(item)) {
        const next = { ...item }
        if (!next.type) next.type = 'summary'
        if (!isPlainRecord(next.config)) next.config = {}
        return next
      }
      return item
    })
}

function normalizeTaskDefinition(values: Record<string, unknown>): TaskDefinition {
  const def = cloneRecord(values)
  const kind = String(def.kind || '')
  const capability = TASK_CAPABILITIES[kind] || { dependency: false, dataSources: false }

  if (Array.isArray(def.labels)) {
    def.labels = formListToRecord(def.labels as KeyValueItem[])
  }
  if (!def.labels) {
    def.labels = {}
  }

  const params = isPlainRecord(def.params) ? { ...def.params } : {}
  params.headers = normalizeMapList(params.headers)
  params.env = normalizeMapList(params.env)
  params.body = parseJSONField(params.body)
  params.sample = parseJSONField(params.sample)
  params.thresholds = parseJSONField(params.thresholds)

  const source = isPlainRecord(params.source) ? { ...params.source } : undefined
  if (source) {
    source.headers = normalizeMapList(source.headers)
    params.source = source
  }

  if (params.work_dir && !params.workdir) {
    params.workdir = params.work_dir
  }
  delete params.work_dir

  if (Array.isArray(params.expect_status)) {
    params.expect_status = params.expect_status
      .map((item) => Number(item))
      .filter((item) => Number.isFinite(item))
  }

  if (kind === 'ai_analyze') {
    if (typeof params.prompt === 'string') {
      params.prompt = { text: params.prompt }
    }
    params.outputs = normalizeOutputs(params.outputs)
  }

  def.params = params
  def.trigger = def.trigger || 'scheduled'

  if (def.trigger !== 'scheduled') {
    def.interval = ''
    def.cron = ''
  }

  if (def.trigger !== 'on_run') {
    def.dependencies = []
    def.watch_task_id = ''
    def.watch_condition = ''
  } else if (def.trigger === 'on_run') {
    if (capability.dependency) {
      const taskID = String(def.task_id || '')
      const deps = Array.isArray(def.dependencies) ? def.dependencies as Array<Record<string, unknown>> : []
      def.dependencies = deps
        .filter((dep) => dep.upstream_task_id)
        .map((dep) => ({
          id: String(dep.id || ''),
          upstream_task_id: String(dep.upstream_task_id || ''),
          downstream_task_id: taskID,
          condition: String(dep.condition || ''),
          source_key: String(dep.source_key || ''),
          params: isPlainRecord(dep.params) ? dep.params : undefined,
        }))
      def.watch_task_id = ''
      def.watch_condition = ''
    } else if (!Array.isArray(def.dependencies)) {
      delete def.dependencies
    }
  }

  return def as unknown as TaskDefinition
}

function collectSourceKeys(def: TaskDefinition): string[] {
  const keys: string[] = []
  for (const dep of def.dependencies || []) {
    if (dep.source_key) keys.push(dep.source_key)
  }
  const dataSources = def.params?.data_sources
  if (Array.isArray(dataSources)) {
    for (const source of dataSources) {
      if (isPlainRecord(source) && typeof source.key === 'string' && source.key) {
        keys.push(source.key)
      }
    }
  }
  return keys
}

function validateWizardRules(def: TaskDefinition): void {
  const kind = def.kind
  const capability = TASK_CAPABILITIES[kind] || { dependency: false, dataSources: false }
  if (def.trigger === 'scheduled' && !def.interval && !def.cron) {
    throw new Error('定时任务必须填写间隔或 Cron 表达式')
  }
  if (def.interval && def.cron) {
    throw new Error('不能同时设置间隔和 Cron 表达式')
  }
  if (def.trigger === 'on_run' && capability.dependency && (!def.dependencies || def.dependencies.length === 0)) {
    throw new Error('依赖触发任务必须至少选择一个上游')
  }
  if (kind === 'data_process') {
    const keys = collectSourceKeys(def)
    const dup = keys.find((item, index) => keys.indexOf(item) !== index)
    if (dup) throw new Error(`数据源 Key 重复：${dup}`)
    const exprs = def.params?.extract_exprs
    if (!Array.isArray(exprs) || exprs.length === 0) {
      throw new Error('数据处理任务至少需要一个提取表达式')
    }
    if (keys.length > 1) {
      for (const expr of exprs) {
        if (isPlainRecord(expr) && !expr.source_key) {
          throw new Error('多上游数据处理任务的提取表达式必须选择上游 Key')
        }
      }
    }
  }
  if (kind === 'ai_analyze') {
    const dataSources = def.params?.data_sources
    if (!Array.isArray(dataSources) || dataSources.length === 0) {
      throw new Error('AI 分析任务至少需要一个数据源')
    }
    const aliases: string[] = []
    for (const source of dataSources) {
      if (!isPlainRecord(source)) continue
      const alias = typeof source.alias === 'string' ? source.alias : ''
      if (alias) {
        if (AI_BUILTIN_ALIASES.has(alias)) {
          throw new Error(`AI 数据源别名不能使用内置名称：${alias}`)
        }
        aliases.push(alias)
      }
    }
    const duplicatedAlias = aliases.find((item, index) => aliases.indexOf(item) !== index)
    if (duplicatedAlias) throw new Error(`AI 数据源别名重复：${duplicatedAlias}`)
    const prompt = def.params?.prompt
    if (!isPlainRecord(prompt) || !prompt.text) {
      throw new Error('AI 分析任务必须填写 Prompt 模板')
    }
  }
}

function getErrorRecord(err: PulseOpsAPIError): Record<string, unknown> | null {
  return isPlainRecord(err.body) ? err.body : null
}

interface TaskFormProps {
  initialValues?: Record<string, unknown>
  mode: 'create' | 'edit'
  creationContext?: TaskCreationContext
  onSubmit: (def: TaskDefinition) => Promise<TaskDefinition | void>
}

export default function TaskForm({ initialValues, mode, creationContext, onSubmit }: TaskFormProps) {
  const [form] = Form.useForm()
  const [currentStep, setCurrentStep] = useState(mode === 'edit' ? 1 : 0)
  const [submitLoading, setSubmitLoading] = useState(false)
  const [taskDefs, setTaskDefs] = useState<TaskDefinition[]>([])
  const [taskDefsLoading, setTaskDefsLoading] = useState(false)
  const [testLoading, setTestLoading] = useState(false)
  const [validated, setValidated] = useState(false)
  const [testRun, setTestRun] = useState<RunRecord | null>(null)
  const [validationResult, setValidationResult] = useState<TaskValidationResponse | null>(null)
  const [validationSource, setValidationSource] = useState<'validate' | 'dry-run' | null>(null)
  const [kindOptions, setKindOptions] = useState<KindOption[]>(DEFAULT_KIND_OPTIONS)
  const [templateOptions, setTemplateOptions] = useState<TemplateOption[]>(
    DEFAULT_KIND_OPTIONS.map((item) => ({ ...item, templateId: item.value })),
  )

  const kind = useWatchedFormValue<string>(form, 'kind', initialValues?.kind as string | undefined)
  const trigger = useWatchedFormValue<string>(form, 'trigger', initialValues?.trigger as string | undefined)
  const watchedFormValues = Form.useWatch([], form) as Record<string, unknown> | undefined
  const formValues = isPlainRecord(watchedFormValues) && Object.keys(watchedFormValues).length > 0
    ? watchedFormValues
    : initialValues
  const capability = TASK_CAPABILITIES[kind || ''] || { dependency: false, dataSources: false }
  const lockTopologyUpstream = Boolean(creationContext?.lockedUpstreamTaskId && capability.dependency)
  const selectedKind = kindOptions.find((item) => item.value === kind)
  const DriverForm = kind && !capability.dataSources ? driverForms[kind] : null

  const steps = useMemo<WizardStep[]>(() => [
    { key: 'template', title: '模板', icon: <ProfileOutlined /> },
    { key: 'basic', title: '基础信息', icon: <SettingOutlined /> },
    { key: 'trigger', title: '触发方式', icon: <ThunderboltOutlined /> },
    { key: 'params', title: '任务参数', icon: <SettingOutlined /> },
    ...(capability.dataSources ? [{ key: 'data_sources' as StepKey, title: '数据源', icon: <DatabaseOutlined /> }] : []),
    { key: 'observability', title: '观测告警', icon: <SettingOutlined /> },
    { key: 'preview', title: '预览验证', icon: <CheckCircleOutlined /> },
  ], [capability.dataSources])

  const currentKey = steps[currentStep]?.key || 'template'
  const previewDefinition = useMemo(() => {
    try {
      return normalizeTaskDefinition(formValues || {})
    } catch {
      return formValues || {}
    }
  }, [formValues])

  useEffect(() => {
    setTaskDefsLoading(true)
    fetchTaskDefinitions()
      .then((defs) => setTaskDefs(defs.filter((d) => d.enabled)))
      .catch(() => {})
      .finally(() => setTaskDefsLoading(false))
  }, [])

  useEffect(() => {
    Promise.all([
      fetchPluginCapabilities('task_driver'),
      fetchPluginCapabilities('task_template'),
    ])
      .then(([driverCaps, templateCaps]) => {
        const defaults = new Map(DEFAULT_KIND_OPTIONS.map((item) => [item.value, item]))
        const nextKinds = driverCaps
          .filter((cap) => cap.enabled)
          .map((cap) => {
            const fallback = defaults.get(cap.name)
            return {
              value: cap.name,
              label: cap.title || fallback?.label || cap.name,
              desc: cap.description || fallback?.desc || `${cap.plugin_name} · ${cap.plugin_version}`,
            }
          })
        if (nextKinds.length > 0) setKindOptions(nextKinds)

        const nextTemplates = buildTemplateOptions(templateCaps, nextKinds.length > 0 ? nextKinds : DEFAULT_KIND_OPTIONS)
        if (nextTemplates.length > 0) setTemplateOptions(nextTemplates)
      })
      .catch(() => {
        setKindOptions(DEFAULT_KIND_OPTIONS)
        setTemplateOptions(DEFAULT_KIND_OPTIONS.map((item) => ({ ...item, templateId: item.value })))
      })
  }, [])

  useEffect(() => {
    if (!initialValues) return
    form.setFieldsValue(initialValues)
  }, [form, initialValues])

  useEffect(() => {
    if (currentStep >= steps.length) {
      setCurrentStep(steps.length - 1)
    }
  }, [currentStep, steps.length])

  const resetVerification = () => {
    setValidated(false)
    setTestRun(null)
    setValidationResult(null)
    setValidationSource(null)
  }

  const buildDefinition = async (): Promise<TaskDefinition> => {
    await form.validateFields()
    const def = normalizeTaskDefinition(form.getFieldsValue(true))
    validateWizardRules(def)
    return def
  }

  const validateCurrentStep = async () => {
    const fields = stepFields(currentKey, capability.dataSources)
    if (fields.length > 0) {
      await form.validateFields(fields)
    }
    if (currentKey === 'trigger' || currentKey === 'data_sources' || currentKey === 'preview') {
      const def = normalizeTaskDefinition(form.getFieldsValue(true))
      validateWizardRules(def)
    }
  }

  const handleNext = async () => {
    try {
      await validateCurrentStep()
      setCurrentStep((prev) => Math.min(prev + 1, steps.length - 1))
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '当前步骤未通过校验')
    }
  }

  const handleValidate = async () => {
    try {
      setTestLoading(true)
      const def = await buildDefinition()
      const result = await validateTaskDefinition(def)
      setValidationResult(result)
      setValidationSource('validate')
      setValidated(true)
      message.success('后端校验通过')
    } catch (err) {
      setValidated(false)
      if (err && typeof err === 'object' && 'errorFields' in err) return
      if (err instanceof PulseOpsAPIError) {
        setValidationResult({ valid: false, errors: err.errors.length ? err.errors : [err.message] })
        setValidationSource('validate')
      }
      message.error(err instanceof Error ? err.message : '校验失败')
    } finally {
      setTestLoading(false)
    }
  }

  const handleDryRun = async () => {
    try {
      setTestLoading(true)
      const def = await buildDefinition()
      const result = await dryRunTaskDefinition(def)
      setValidationResult(result)
      setValidationSource('dry-run')
      setValidated(true)
      message.success('dry-run 校验通过')
    } catch (err) {
      setValidated(false)
      if (err && typeof err === 'object' && 'errorFields' in err) return
      if (err instanceof PulseOpsAPIError) {
        setValidationResult({ valid: false, errors: err.errors.length ? err.errors : [err.message] })
        setValidationSource('dry-run')
      }
      message.error(err instanceof Error ? err.message : 'dry-run 失败')
    } finally {
      setTestLoading(false)
    }
  }

  const handleTestRun = async () => {
    try {
      setTestLoading(true)
      const def = await buildDefinition()
      const run = await testRunTaskDefinition(def)
      setTestRun(run)
      setValidated(true)
      message.success(`试运行完成：${run.run_status}/${run.check_status}`)
    } catch (err) {
      setValidated(false)
      if (err && typeof err === 'object' && 'errorFields' in err) return
      if (err instanceof PulseOpsAPIError) {
        const body = getErrorRecord(err)
        if (isPlainRecord(body?.record)) {
          setTestRun(body.record as unknown as RunRecord)
        }
      }
      message.error(err instanceof Error ? err.message : '试运行失败')
    } finally {
      setTestLoading(false)
    }
  }

  const handleFinish = async () => {
    try {
      setSubmitLoading(true)
      const taskDefinition = await buildDefinition()
      await validateTaskDefinition(taskDefinition)
      await onSubmit(taskDefinition)
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      if (err instanceof PulseOpsAPIError) {
        setValidationResult({ valid: false, errors: err.errors.length ? err.errors : [err.message] })
        setValidationSource('validate')
        setCurrentStep(steps.length - 1)
      }
      message.error(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSubmitLoading(false)
    }
  }

  const selectTemplate = (option: TemplateOption) => {
    const applyKind = () => {
      const value = option.value
      const nextCapability = TASK_CAPABILITIES[value] || { dependency: false, dataSources: false }
      const existingDeps = form.getFieldValue('dependencies')
      const existingDepsList = Array.isArray(existingDeps) ? existingDeps : []
      const topologyUpstream = creationContext?.lockedUpstreamTaskId
      const shouldUseTopologyDeps = Boolean(nextCapability.dependency && topologyUpstream)
      const nextDeps = shouldUseTopologyDeps
        ? (existingDepsList.length > 0 ? existingDepsList : [buildTopologyDependency(String(form.getFieldValue('task_id') || ''), topologyUpstream!)])
        : nextCapability.dependency
          ? existingDepsList
          : []
      const nextParams = shouldUseTopologyDeps && topologyUpstream
        ? { ...cloneRecord(option.params || {}), ...buildTopologyParams(value, {
            upstreamTaskId: topologyUpstream,
            upstreamName: creationContext?.lockedUpstreamName,
          }) }
        : cloneRecord(option.params || {})
      const defaults = option.defaults || {}
      const defaultTrigger = typeof defaults.trigger === 'string' ? defaults.trigger : ''
      const defaultTimeout = typeof defaults.timeout === 'string' ? defaults.timeout : ''
      const defaultInterval = typeof defaults.interval === 'string' ? defaults.interval : undefined
      const defaultCron = typeof defaults.cron === 'string' ? defaults.cron : undefined
      const defaultLabels = isPlainRecord(defaults.labels) ? recordToFormList(defaults.labels) : undefined
      const nextTrigger = nextCapability.dependency
        ? 'on_run'
        : defaultTrigger || (creationContext?.source === 'topology' ? 'manual' : 'scheduled')
      form.setFieldsValue({
        kind: value,
        timeout: defaultTimeout || form.getFieldValue('timeout'),
        interval: defaultInterval,
        cron: defaultCron,
        labels: defaultLabels,
        params: nextParams,
        dependencies: nextCapability.dependency ? nextDeps : undefined,
        watch_task_id: '',
        watch_condition: '',
        trigger: nextTrigger,
      })
      resetVerification()
    }
    if (mode === 'edit' && kind && kind !== option.value) {
      Modal.confirm({
        title: '确认切换任务类型？',
        content: '切换任务类型会清空不兼容的任务参数、依赖和数据源配置。',
        okText: '切换',
        cancelText: '取消',
        onOk: applyKind,
      })
      return
    }
    applyKind()
  }

  return (
    <Form
      form={form}
      layout="horizontal"
      labelCol={{ xs: 24, sm: 6 }}
      wrapperCol={{ xs: 24, sm: 18 }}
      initialValues={initialValues}
      scrollToFirstError
      preserve
      onValuesChange={resetVerification}
    >
      <Card className="ops-card" style={{ marginBottom: 16 }}>
        <Steps
          size="small"
          current={currentStep}
          items={steps.map((step) => ({ title: step.title, icon: step.icon }))}
          onChange={(next) => {
            if (next <= currentStep) setCurrentStep(next)
          }}
        />
      </Card>

      {renderStepContent({
        key: currentKey,
        mode,
        creationContext,
        kind,
        trigger,
        capability,
        lockTopologyUpstream,
        selectedKind,
        kindOptions,
        templateOptions,
        DriverForm,
        taskDefs,
        taskDefsLoading,
        form,
        previewDefinition,
        validated,
        testRun,
        validationResult,
        validationSource,
        onSelectTemplate: selectTemplate,
      })}

      <Card className="ops-card">
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
          <Button disabled={currentStep === 0} onClick={() => setCurrentStep((prev) => Math.max(prev - 1, 0))}>
            上一步
          </Button>
          <Space wrap>
            <Text type="secondary">
              {currentStep === steps.length - 1 ? '提交前会重新执行后端 validate' : '每一步只校验当前配置'}
            </Text>
            {currentStep < steps.length - 1 ? (
              <Button type="primary" onClick={handleNext}>
                下一步
              </Button>
            ) : (
              <>
                <Button onClick={handleValidate} loading={testLoading}>
                  校验配置
                </Button>
                <Button onClick={handleDryRun} loading={testLoading}>
                  dry-run
                </Button>
                <Button onClick={handleTestRun} loading={testLoading}>
                  试运行
                </Button>
                <Button type="primary" loading={submitLoading} onClick={handleFinish}>
                  {mode === 'create' ? '创建任务' : '保存修改'}
                </Button>
              </>
            )}
          </Space>
        </div>
      </Card>
    </Form>
  )
}

function stepFields(step: StepKey, hasDataSources: boolean): NamePath[] {
  switch (step) {
    case 'template':
      return ['kind']
    case 'basic':
      return ['task_id', 'name', 'kind', 'enabled', 'labels']
    case 'trigger':
      return ['trigger', 'interval', 'cron', 'timeout', 'dependencies']
    case 'params':
      return hasDataSources ? [] : ['params']
    case 'data_sources':
      return ['dependencies', 'params']
    case 'observability':
      return ['trace', 'alert']
    case 'preview':
      return []
    default:
      return []
  }
}

function renderStepContent(props: {
  key: StepKey
  mode: 'create' | 'edit'
  creationContext?: TaskCreationContext
  kind?: string
  trigger?: string
  capability: { dependency: boolean; dataSources: boolean }
  lockTopologyUpstream: boolean
  selectedKind?: { value: string; label: string; desc: string }
  kindOptions: KindOption[]
  templateOptions: TemplateOption[]
  DriverForm: React.ComponentType<{ form?: ReturnType<typeof Form.useForm>[0] }> | null
  taskDefs: TaskDefinition[]
  taskDefsLoading: boolean
  form: ReturnType<typeof Form.useForm>[0]
  previewDefinition: Record<string, unknown> | TaskDefinition
  validated: boolean
  testRun: RunRecord | null
  validationResult: TaskValidationResponse | null
  validationSource: 'validate' | 'dry-run' | null
  onSelectTemplate: (template: TemplateOption) => void
}) {
  switch (props.key) {
    case 'template':
      return <TemplateStep kind={props.kind} templateOptions={props.templateOptions} creationContext={props.creationContext} onSelectTemplate={props.onSelectTemplate} />
    case 'basic':
      return <BasicInfoStep mode={props.mode} kindOptions={props.kindOptions} creationContext={props.creationContext} />
    case 'trigger':
      return (
        <TriggerStep
          trigger={props.trigger}
          capability={props.capability}
          lockTopologyUpstream={props.lockTopologyUpstream}
          taskDefs={props.taskDefs}
          taskDefsLoading={props.taskDefsLoading}
          creationContext={props.creationContext}
        />
      )
    case 'params':
      return (
        <DriverParamsStep
          kind={props.kind}
          capability={props.capability}
          selectedKind={props.selectedKind}
          DriverForm={props.DriverForm}
          form={props.form}
        />
      )
    case 'data_sources':
      return <DataSourcesStep kind={props.kind} form={props.form} />
    case 'observability':
      return <ObservabilityStep />
    case 'preview':
      return (
        <PreviewStep
          preview={props.previewDefinition}
          validated={props.validated}
          testRun={props.testRun}
          validationResult={props.validationResult}
          validationSource={props.validationSource}
        />
      )
    default:
      return null
  }
}

function TemplateStep({
  kind,
  templateOptions,
  creationContext,
  onSelectTemplate,
}: {
  kind?: string
  templateOptions: TemplateOption[]
  creationContext?: TaskCreationContext
  onSelectTemplate: (template: TemplateOption) => void
}) {
  return (
    <Card className="ops-card" title="选择任务模板" style={{ marginBottom: 16 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))', gap: 10 }}>
        {templateOptions.map((item) => {
          const capability = TASK_CAPABILITIES[item.value]
          const recommended = creationContext?.recommendedKinds?.includes(item.value)
          return (
            <Button
              key={item.templateId}
              type={kind === item.value ? 'primary' : 'default'}
              style={{ height: 100, textAlign: 'left', justifyContent: 'flex-start' }}
              onClick={() => onSelectTemplate(item)}
            >
              <span style={{ width: '100%' }}>
                <strong>{item.label}</strong>
                <Text type="secondary" style={{ display: 'block', marginTop: 4, whiteSpace: 'normal' }}>
                  {item.desc}
                </Text>
                {capability?.dataSources && <Tag style={{ marginTop: 7 }}>支持上游数据源</Tag>}
                {recommended && <Tag color="blue" style={{ marginTop: 7 }}>拓扑推荐</Tag>}
                {item.pluginName && <Tag color="geekblue" style={{ marginTop: 7 }}>{item.pluginName}</Tag>}
              </span>
            </Button>
          )
        })}
      </div>
    </Card>
  )
}

function BasicInfoStep({ mode, kindOptions, creationContext }: { mode: 'create' | 'edit'; kindOptions: KindOption[]; creationContext?: TaskCreationContext }) {
  return (
    <Card className="ops-card" title="基础信息" style={{ marginBottom: 16 }}>
      <Form.Item name="task_id" label="任务 ID" rules={[{ required: true, message: '任务 ID 缺失' }]}>
        <Input disabled={mode === 'edit'} placeholder="自动生成" />
      </Form.Item>
      <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
        <Input placeholder="任务显示名称" />
      </Form.Item>
      <Form.Item name="kind" label="类型" rules={[{ required: true, message: '请选择任务类型' }]}>
        <Select
          options={kindOptions.map((item) => ({ value: item.value, label: item.label }))}
          placeholder="请先选择任务模板"
          disabled
        />
      </Form.Item>
      <Form.Item name="enabled" label="启用" valuePropName="checked">
        <Switch />
      </Form.Item>
      {creationContext?.lockedPipelineId && (
        <Form.Item label="任务组">
          <Alert
            type="info"
            showIcon
            message={creationContext.lockedPipelineId}
            description="从依赖拓扑创建时自动绑定当前任务组。"
          />
        </Form.Item>
      )}
      <Form.Item label="标签">
        <Form.List name="labels">
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name, ...restField }) => (
                <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                  <Form.Item {...restField} name={[name, 'key']} rules={[{ required: true, message: '请输入键' }]}>
                    <Input placeholder="如 env" />
                  </Form.Item>
                  <Form.Item {...restField} name={[name, 'value']} rules={[{ required: true, message: '请输入值' }]}>
                    <Input placeholder="如 prod" />
                  </Form.Item>
                  <Button danger type="link" onClick={() => remove(name)}>删除</Button>
                </Space>
              ))}
              <Button type="dashed" onClick={() => add()} block>
                添加标签
              </Button>
            </>
          )}
        </Form.List>
      </Form.Item>
    </Card>
  )
}

function TriggerStep({
  trigger,
  capability,
  lockTopologyUpstream,
  taskDefs,
  taskDefsLoading,
  creationContext,
}: {
  trigger?: string
  capability: { dependency: boolean; dataSources: boolean }
  lockTopologyUpstream: boolean
  taskDefs: TaskDefinition[]
  taskDefsLoading: boolean
  creationContext?: TaskCreationContext
}) {
  const lockedUpstream = lockTopologyUpstream ? creationContext?.lockedUpstreamTaskId : undefined
  const lockedUpstreamName = lockTopologyUpstream ? creationContext?.lockedUpstreamName : undefined
  return (
    <Card className="ops-card" title="触发方式" style={{ marginBottom: 16 }}>
      <Form.Item name="trigger" label="触发类型">
        <Radio.Group disabled={Boolean(lockedUpstream)}>
          <Radio.Button value="scheduled">定时</Radio.Button>
          <Radio.Button value="manual">手动</Radio.Button>
          {capability.dependency && <Radio.Button value="on_run">依赖触发</Radio.Button>}
        </Radio.Group>
      </Form.Item>

      {trigger === 'scheduled' && (
        <>
          <Form.Item name="interval" label="间隔">
            <Input placeholder="如 30s, 5m, 1h" />
          </Form.Item>
          <Form.Item name="cron" label="Cron 表达式">
            <Input placeholder="如 0 */6 * * *" />
          </Form.Item>
        </>
      )}

      <Form.Item name="timeout" label="超时">
        <Input placeholder="如 10s" />
      </Form.Item>

      {trigger === 'on_run' && capability.dependency && lockedUpstream && (
        <Form.Item label="上游依赖">
          <Alert
            type="info"
            showIcon
            message={lockedUpstreamName || lockedUpstream}
            description={`已从拓扑带入：source_key=${TOPOLOGY_SOURCE_KEY}，触发条件=${TOPOLOGY_DEFAULT_CONDITION}`}
          />
        </Form.Item>
      )}

      {trigger === 'on_run' && capability.dependency && !lockedUpstream && (
        <Form.Item label="上游依赖">
          <Form.List name="dependencies">
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name, ...restField }) => (
                  <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 8, flexWrap: 'wrap' }}>
                    <Form.Item {...restField} name={[name, 'id']} hidden>
                      <Input />
                    </Form.Item>
                    <Form.Item
                      {...restField}
                      name={[name, 'upstream_task_id']}
                      rules={[{ required: true, message: '请选择上游任务' }]}
                    >
                      <Select
                        loading={taskDefsLoading}
                        notFoundContent={taskDefsLoading ? <Spin size="small" /> : '没有启用的任务'}
                        placeholder="选择上游任务"
                        options={taskDefs.map((d) => ({ value: d.task_id, label: `${d.name} (${d.task_id})` }))}
                        showSearch
                        optionFilterProp="label"
                        style={{ width: 280 }}
                      />
                    </Form.Item>
                    <Form.Item
                      {...restField}
                      name={[name, 'source_key']}
                      rules={[{ required: true, message: '请输入 source_key' }]}
                    >
                      <Input placeholder="source_key" style={{ width: 150 }} />
                    </Form.Item>
                    <Form.Item {...restField} name={[name, 'condition']}>
                      <Select
                        allowClear
                        placeholder="总是触发"
                        options={[
                          { value: 'check_status == pass', label: '检查通过时触发' },
                          { value: 'run_status == success', label: '运行成功时触发' },
                        ]}
                        style={{ width: 180 }}
                      />
                    </Form.Item>
                    <Button danger type="link" onClick={() => remove(name)}>删除</Button>
                  </Space>
                ))}
                <Button
                  type="dashed"
                  onClick={() => add({ source_key: `source_${fields.length + 1}`, condition: 'run_status == success' })}
                  block
                >
                  添加上游
                </Button>
              </>
            )}
          </Form.List>
        </Form.Item>
      )}

    </Card>
  )
}

function DriverParamsStep({
  kind,
  capability,
  selectedKind,
  DriverForm,
  form,
}: {
  kind?: string
  capability: { dependency: boolean; dataSources: boolean }
  selectedKind?: { value: string; label: string; desc: string }
  DriverForm: React.ComponentType<{ form?: ReturnType<typeof Form.useForm>[0] }> | null
  form: ReturnType<typeof Form.useForm>[0]
}) {
  return (
    <Card
      className="ops-card"
      title={kind ? `${KIND_LABELS[kind] || kind} 参数` : '任务参数'}
      style={{ marginBottom: 16 }}
      extra={selectedKind ? <Text type="secondary">{selectedKind.desc}</Text> : null}
    >
      {!kind ? (
        <Alert type="info" showIcon message="请先选择任务模板" />
      ) : capability.dataSources ? (
        <Alert
          type="info"
          showIcon
          message="该任务的核心配置在下一步完成"
          description="数据处理和 AI 分析需要先配置上游数据源，再填写字段提取或 Prompt。"
        />
      ) : DriverForm ? (
        <DriverForm form={form} />
      ) : (
        <Alert type="warning" showIcon message="当前任务类型暂无参数表单" />
      )}
    </Card>
  )
}

function DataSourcesStep({ kind, form }: { kind?: string; form: ReturnType<typeof Form.useForm>[0] }) {
  const DriverForm = kind ? driverForms[kind] : null
  return (
    <Card
      className="ops-card"
      title={kind === 'ai_analyze' ? 'AI 分析输入' : '上游数据源'}
      style={{ marginBottom: 16 }}
    >
      {DriverForm ? <DriverForm form={form} /> : <Alert type="info" showIcon message="当前任务类型不需要数据源配置" />}
    </Card>
  )
}

function ObservabilityStep() {
  return (
    <Card className="ops-card" title="观测与告警" style={{ marginBottom: 16 }}>
      <Form.Item name={['trace', 'level']} label="留痕级别">
        <Select
          allowClear
          options={[
            { value: 'off', label: '关闭' },
            { value: 'summary', label: '摘要' },
            { value: 'detail', label: '详细' },
            { value: 'debug', label: '调试' },
          ]}
        />
      </Form.Item>
      <Form.Item name={['trace', 'retain_days']} label="保留天数">
        <InputNumber min={0} style={{ width: '100%' }} placeholder="默认继承平台设置" />
      </Form.Item>
      <Form.Item name={['trace', 'mask_fields']} label="脱敏字段">
        <Select mode="tags" placeholder="authorization, token, password" />
      </Form.Item>
      <Form.Item name={['alert', 'consecutive_failures']} label="连续失败次数">
        <InputNumber min={1} style={{ width: '100%' }} placeholder="留空表示不单独告警" />
      </Form.Item>
      <Form.Item name={['alert', 'channels']} label="通知渠道">
        <Select mode="tags" placeholder="如 feishu, email" />
      </Form.Item>
      <Form.Item name={['alert', 'recover_notify']} label="恢复通知" valuePropName="checked">
        <Switch />
      </Form.Item>
    </Card>
  )
}

function PreviewStep({
  preview,
  validated,
  testRun,
  validationResult,
  validationSource,
}: {
  preview: Record<string, unknown> | TaskDefinition
  validated: boolean
  testRun: RunRecord | null
  validationResult: TaskValidationResponse | null
  validationSource: 'validate' | 'dry-run' | null
}) {
  return (
    <Card className="ops-card" title="预览与验证" style={{ marginBottom: 16 }}>
      <Alert
        type={validated ? 'success' : 'info'}
        showIcon
        message={validated ? '当前配置已通过最近一次后端校验' : '只读预览'}
        description="这里展示的是规范化后的 TaskDefinition；需要修改请返回对应步骤。"
        style={{ marginBottom: 12 }}
      />
      {testRun && (
        <Alert
          type={testRun.run_status === 'success' ? 'success' : 'warning'}
          showIcon
          message={`最近试运行：${testRun.run_status}/${testRun.check_status}`}
          description={testRun.error_message || `run_id: ${testRun.run_id}`}
          style={{ marginBottom: 12 }}
        />
      )}
      {validationResult && (
        <Alert
          type={validationResult.valid ? 'success' : 'error'}
          showIcon
          message={`${validationSource === 'dry-run' ? 'dry-run' : 'validate'}：${validationResult.valid ? '通过' : '未通过'}`}
          description={
            validationResult.errors?.length
              ? validationResult.errors.join('；')
              : '后端已返回规范化任务定义。'
          }
          style={{ marginBottom: 12 }}
        />
      )}
      <pre className="code-block">{safeJson(preview)}</pre>
    </Card>
  )
}
