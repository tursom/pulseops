import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Collapse,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd'
import type { Rule } from 'antd/es/form'
import type { NamePath } from 'antd/es/form/interface'
import type { ColumnsType } from 'antd/es/table'
import {
  CheckCircleOutlined,
  EditOutlined,
  PlusOutlined,
  SaveOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type {
  PluginAsset,
  PluginCapability,
  PluginConfigClass,
  PluginConfigEvent,
  PluginConfigField,
  PluginConfigInstance,
  PluginConfigInstanceDetail,
  PluginConfigSchema,
  PluginConfigVersion,
  PluginSecret,
  PluginView,
} from '../api/types'
import {
  activatePluginConfigVersion,
  activatePluginAssetVersion,
  createPluginConfig,
  createPluginAsset,
  createPluginConfigVersion,
  disablePluginConfig,
  fetchCapabilityConfigSchema,
  fetchPluginAssets,
  fetchPluginConfig,
  fetchPluginConfigEvents,
  fetchPluginConfigs,
  fetchPluginConfigSchema,
  fetchPluginSecrets,
  upsertPluginSecret,
  uploadPluginAssetVersion,
  updatePluginConfigVersion,
  validatePluginAssetVersion,
  validatePluginConfigVersion,
} from '../api/client'
import {
  configReferenceID,
  normalizePluginConfigValues,
  toAssetReference,
  toSecretReference,
} from '../utils/pluginConfigRefs'

const { Text } = Typography

type ConfigTarget = {
  key: string
  label: string
  scope: 'plugin' | 'capability'
  capability?: PluginCapability
}

type ConfigFieldGroup = {
  key: string
  label: string
  order: number
  collapsed: boolean
  fields: Array<[string, PluginConfigField]>
}

function statusColor(status: string): string {
  switch (status) {
    case 'active':
    case 'validated':
      return 'green'
    case 'draft':
      return 'gold'
    case 'failed':
      return 'red'
    case 'retired':
    case 'disabled':
      return 'default'
    default:
      return 'blue'
  }
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function formatJSON(value: unknown): string {
  if (value === undefined || value === null || value === '') return ''
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function defaultValues(schema?: PluginConfigSchema): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [name, field] of Object.entries(schema?.fields || {})) {
    if (field.default !== undefined) out[name] = field.default
  }
  return out
}

function sortedConfigFields(fields?: Record<string, PluginConfigField>): Array<[string, PluginConfigField]> {
  return Object.entries(fields || {}).sort(([, a], [, b]) => (a.ui?.order || 0) - (b.ui?.order || 0))
}

function sortedFields(schema?: PluginConfigSchema): Array<[string, PluginConfigField]> {
  return sortedConfigFields(schema?.fields)
}

function groupedFields(schema?: PluginConfigSchema): ConfigFieldGroup[] {
  const groups = new Map<string, ConfigFieldGroup>()
  for (const [name, field] of sortedFields(schema)) {
    const groupKey = field.ui?.advanced ? '__advanced' : field.ui?.group || '__default'
    const groupLabel = field.ui?.advanced ? '高级配置' : field.ui?.group || '基础配置'
    const fieldOrder = field.ui?.order || 0
    const current = groups.get(groupKey)
    if (current) {
      current.order = Math.min(current.order, fieldOrder)
      current.collapsed = current.collapsed || Boolean(field.ui?.collapsed)
      current.fields.push([name, field])
      continue
    }
    groups.set(groupKey, {
      key: groupKey,
      label: groupLabel,
      order: field.ui?.advanced ? Number.MAX_SAFE_INTEGER : fieldOrder,
      collapsed: Boolean(field.ui?.advanced || field.ui?.collapsed),
      fields: [[name, field]],
    })
  }
  return Array.from(groups.values()).sort((a, b) => a.order - b.order || a.label.localeCompare(b.label))
}

function namePathString(path: NamePath): string {
  const parts = Array.isArray(path) ? path : [path]
  return parts.map(String).join('.')
}

function fieldValue(values: Record<string, unknown>, path: string): unknown {
  const parts = path
    .replace(/\[(\d+)\]/g, '.$1')
    .split('.')
    .filter(Boolean)
  let current: unknown = values
  for (const part of parts) {
    if (current === null || current === undefined) return undefined
    if (Array.isArray(current)) {
      current = current[Number(part)]
      continue
    }
    if (typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function isEmptyValue(value: unknown): boolean {
  if (value === undefined || value === null || value === '') return true
  if (Array.isArray(value)) return value.length === 0
  if (typeof value === 'object') return Object.keys(value as Record<string, unknown>).length === 0
  return false
}

function conditionVisible(field: PluginConfigField, values: Record<string, unknown>, basePath = ''): boolean {
  const condition = field.ui?.visible_when
  if (!condition?.field || !condition.op) return true
  const conditionPath = basePath && !condition.field.includes('.') && !condition.field.includes('[')
    ? `${basePath}.${condition.field}`
    : condition.field
  const actual = fieldValue(values, conditionPath)
  switch (condition.op) {
    case 'eq':
      return Object.is(actual, condition.value)
    case 'ne':
      return !Object.is(actual, condition.value)
    case 'in':
      return Array.isArray(condition.value) && condition.value.some((item) => Object.is(item, actual))
    case 'not_in':
      return Array.isArray(condition.value) && !condition.value.some((item) => Object.is(item, actual))
    case 'exists':
      return !isEmptyValue(actual)
    case 'empty':
      return isEmptyValue(actual)
    default:
      return true
  }
}

function JsonValueInput({
  value,
  onChange,
  placeholder,
}: {
  value?: unknown
  onChange?: (value: unknown) => void
  placeholder?: string
}) {
  const [text, setText] = useState(formatJSON(value))
  const [invalid, setInvalid] = useState(false)

  useEffect(() => {
    setText(formatJSON(value))
    setInvalid(false)
  }, [value])

  return (
    <Input.TextArea
      value={text}
      autoSize={{ minRows: 3, maxRows: 10 }}
      placeholder={placeholder}
      status={invalid ? 'error' : undefined}
      onChange={(event) => {
        const next = event.target.value
        setText(next)
        if (next.trim() === '') {
          setInvalid(false)
          onChange?.(undefined)
          return
        }
        try {
          onChange?.(JSON.parse(next))
          setInvalid(false)
        } catch {
          setInvalid(true)
        }
      }}
    />
  )
}

function optionKey(value: unknown): string {
  const encoded = JSON.stringify(value)
  return `${typeof value}:${encoded === undefined ? String(value) : encoded}`
}

function ConfigSelectInput({
  field,
  value,
  onChange,
  multiple,
}: {
  field: PluginConfigField
  value?: unknown
  onChange?: (value: unknown) => void
  multiple?: boolean
}) {
  const valueByKey = new Map((field.options || []).map((option) => [optionKey(option.value), option.value]))
  const options = (field.options || []).map((option) => ({
    value: optionKey(option.value),
    label: option.label || String(option.value),
  }))
  if (multiple) {
    return (
      <Select
        mode="multiple"
        allowClear={!field.required}
        placeholder={field.ui?.placeholder}
        value={Array.isArray(value) ? value.map(optionKey) : undefined}
        options={options}
        onChange={(keys: string[]) => onChange?.(keys.map((key) => valueByKey.get(key)))}
      />
    )
  }
  return (
    <Select
      allowClear={!field.required}
      placeholder={field.ui?.placeholder}
      value={value === undefined ? undefined : optionKey(value)}
      options={options}
      onChange={(key?: string) => onChange?.(key === undefined ? undefined : valueByKey.get(key))}
    />
  )
}

function assetSelectOptions(assets: PluginAsset[], kind?: string, scope?: string, configInstanceID?: string) {
  return assets
    .filter((asset) => {
      if (kind && asset.kind !== kind) return false
      if (scope && asset.scope !== scope) return false
      if (asset.scope === 'config_instance' && asset.config_instance_id !== configInstanceID) return false
      return asset.status === 'active' && Boolean(asset.active_version)
    })
    .map((asset) => ({
      value: asset.id,
      label: `${asset.title || asset.id} #${asset.active_version}`,
    }))
}

function secretSelectOptions(secrets: PluginSecret[]) {
  return secrets
    .filter((secret) => secret.status === 'active')
    .map((secret) => ({
      value: secret.id,
      label: `${secret.title || secret.id} ${secret.masked || ''}`.trim(),
    }))
}

function configFieldRules(label: string, field: PluginConfigField): Rule[] | undefined {
  const rules: Rule[] = []
  if (field.required) {
    rules.push({ required: true, message: `${label} 必填` })
  }
  if (field.type === 'string') {
    if (field.validation?.min_len) {
      rules.push({ min: field.validation.min_len, message: `${label} 至少 ${field.validation.min_len} 个字符` })
    }
    if (field.validation?.max_len) {
      rules.push({ max: field.validation.max_len, message: `${label} 最多 ${field.validation.max_len} 个字符` })
    }
    if (field.validation?.pattern) {
      try {
        rules.push({ pattern: new RegExp(field.validation.pattern), message: `${label} 格式不正确` })
      } catch {
        // Manifest validation rejects invalid regex; keep the form usable if stale data appears.
      }
    }
  }
  if (field.type === 'number') {
    if (field.validation?.min !== undefined) {
      rules.push({ type: 'number', min: field.validation.min, message: `${label} 不能小于 ${field.validation.min}` })
    }
    if (field.validation?.max !== undefined) {
      rules.push({ type: 'number', max: field.validation.max, message: `${label} 不能大于 ${field.validation.max}` })
    }
  }
  return rules.length > 0 ? rules : undefined
}

function ConfigFieldControl({
  name,
  namePath,
  field,
  classes,
  assets,
  secrets,
  configInstanceID,
  requiredAllowed = true,
}: {
  name: string
  namePath: NamePath
  field: PluginConfigField
  classes: Record<string, PluginConfigClass>
  assets: PluginAsset[]
  secrets: PluginSecret[]
  configInstanceID?: string
  requiredAllowed?: boolean
}) {
  const label = field.ui?.label || name
  const help = field.ui?.help || field.description
  const rules = configFieldRules(label, requiredAllowed ? field : { ...field, required: false })
  const common = {
    name: namePath,
    label,
    tooltip: help,
    rules,
  }

  switch (field.type) {
    case 'number':
      return (
        <Form.Item {...common}>
          <InputNumber
            style={{ width: '100%' }}
            min={field.validation?.min}
            max={field.validation?.max}
            step={field.validation?.step}
          />
        </Form.Item>
      )
    case 'bool':
      return (
        <Form.Item {...common} valuePropName="checked">
          <Switch />
        </Form.Item>
      )
    case 'select':
      return (
        <Form.Item {...common}>
          <ConfigSelectInput field={field} />
        </Form.Item>
      )
    case 'multi_select':
      return (
        <Form.Item {...common}>
          <ConfigSelectInput field={field} multiple />
        </Form.Item>
      )
    case 'object':
      if (field.class && field.class !== 'JSONObject' && classes[field.class]?.fields) {
        const childBasePath = namePathString(namePath)
        const childFields = sortedConfigFields(classes[field.class].fields)
        return (
          <div style={{ marginBottom: 16 }}>
            <Space direction="vertical" size={2} style={{ width: '100%' }}>
              <Text strong>{label}</Text>
              {help && <Text type="secondary">{help}</Text>}
              <div style={{ borderLeft: '1px solid #d9d9d9', paddingLeft: 12 }}>
                {childFields.map(([childName, childField]) => (
                  <Form.Item noStyle shouldUpdate key={childName}>
                    {({ getFieldsValue }) => {
                      const values = getFieldsValue(true) as Record<string, unknown>
                      if (!conditionVisible(childField, values, childBasePath)) return null
                      return (
                        <ConfigFieldControl
                          name={childName}
                          namePath={[...(Array.isArray(namePath) ? namePath : [namePath]), childName]}
                          field={childField}
                          classes={classes}
                          assets={assets}
                          secrets={secrets}
                          configInstanceID={configInstanceID}
                          requiredAllowed={requiredAllowed && field.required}
                        />
                      )
                    }}
                  </Form.Item>
                ))}
              </div>
            </Space>
          </div>
        )
      }
      return (
        <Form.Item {...common}>
          <JsonValueInput placeholder={field.ui?.placeholder || '{}'} />
        </Form.Item>
      )
    case 'array':
      if (field.items?.type === 'object' && field.items.class && field.items.class !== 'JSONObject' && classes[field.items.class]?.fields) {
        const listNamePath = Array.isArray(namePath) ? namePath : [namePath]
        const childFields = sortedConfigFields(classes[field.items.class].fields)
        return (
          <div style={{ marginBottom: 16 }}>
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>{label}</Text>
              {help && <Text type="secondary">{help}</Text>}
              <Form.List name={listNamePath}>
                {(items, { add, remove }) => (
                  <Space direction="vertical" size={10} style={{ width: '100%' }}>
                    {items.map((item, index) => {
                      const itemBasePath = namePathString([...listNamePath, item.name])
                      return (
                        <div key={item.key} style={{ borderLeft: '1px solid #d9d9d9', paddingLeft: 12 }}>
                          <Space direction="vertical" size={4} style={{ width: '100%' }}>
                            <Space>
                              <Text type="secondary">#{index + 1}</Text>
                              <Button danger size="small" type="link" onClick={() => remove(item.name)}>
                                删除
                              </Button>
                            </Space>
                            {childFields.map(([childName, childField]) => (
                              <Form.Item noStyle shouldUpdate key={childName}>
                                {({ getFieldsValue }) => {
                                  const values = getFieldsValue(true) as Record<string, unknown>
                                  if (!conditionVisible(childField, values, itemBasePath)) return null
                                  return (
                                    <ConfigFieldControl
                                      name={childName}
                                      namePath={[...listNamePath, item.name, childName]}
                                      field={childField}
                                      classes={classes}
                                      assets={assets}
                                      secrets={secrets}
                                      configInstanceID={configInstanceID}
                                      requiredAllowed={requiredAllowed}
                                    />
                                  )
                                }}
                              </Form.Item>
                            ))}
                          </Space>
                        </div>
                      )
                    })}
                    <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({})}>
                      添加{label}
                    </Button>
                  </Space>
                )}
              </Form.List>
            </Space>
          </div>
        )
      }
      if (field.items?.type === 'file') {
        return (
          <Form.Item
            {...common}
            getValueProps={(value) => ({
              value: Array.isArray(value) ? value.map((item) => configReferenceID(item, 'asset_id')).filter(Boolean) : [],
            })}
            getValueFromEvent={(value: string[]) => value.map(toAssetReference).filter(Boolean)}
          >
            <Select
              mode="multiple"
              allowClear={!field.required}
              placeholder={field.ui?.placeholder || '选择资产'}
              options={assetSelectOptions(assets, field.items.asset_kind, field.items.asset_scope, configInstanceID)}
              showSearch
              optionFilterProp="label"
            />
          </Form.Item>
        )
      }
      return (
        <Form.Item {...common}>
          <JsonValueInput placeholder={field.type === 'array' ? '[]' : '{}'} />
        </Form.Item>
      )
    case 'file':
      return (
        <Form.Item
          {...common}
          getValueProps={(value) => ({ value: configReferenceID(value, 'asset_id') })}
          getValueFromEvent={toAssetReference}
        >
          <Select
            allowClear={!field.required}
            placeholder={field.ui?.placeholder || '选择资产'}
            options={assetSelectOptions(assets, field.asset_kind, field.asset_scope, configInstanceID)}
            showSearch
            optionFilterProp="label"
          />
        </Form.Item>
      )
    case 'secret':
      return (
        <Form.Item
          {...common}
          getValueProps={(value) => ({ value: configReferenceID(value, 'secret_id') })}
          getValueFromEvent={toSecretReference}
        >
          <Select
            allowClear={!field.required}
            placeholder={field.ui?.placeholder || '选择 Secret'}
            options={secretSelectOptions(secrets)}
            showSearch
            optionFilterProp="label"
          />
        </Form.Item>
      )
    default:
      return (
        <Form.Item {...common}>
          <Input placeholder={field.ui?.placeholder} />
        </Form.Item>
      )
  }
}

function ConfigVersionForm({
  schema,
  classes,
  assets,
  secrets,
  configInstanceID,
}: {
  schema?: PluginConfigSchema
  classes: Record<string, PluginConfigClass>
  assets: PluginAsset[]
  secrets: PluginSecret[]
  configInstanceID?: string
}) {
  const groups = groupedFields(schema)
  if (groups.length === 0) {
    return <Alert type="info" showIcon message="当前目标没有声明配置字段" />
  }
  return (
    <Form.Item noStyle shouldUpdate>
      {({ getFieldsValue }) => {
        const values = getFieldsValue(true) as Record<string, unknown>
        const visibleGroups = groups
          .map((group) => ({
            ...group,
            fields: group.fields.filter(([, field]) => conditionVisible(field, values)),
          }))
          .filter((group) => group.fields.length > 0)
        if (visibleGroups.length === 0) {
          return <Alert type="info" showIcon message="当前条件下没有可编辑字段" />
        }
        return (
          <Collapse
            ghost
            destroyOnHidden={false}
            defaultActiveKey={visibleGroups.filter((group) => !group.collapsed).map((group) => group.key)}
            items={visibleGroups.map((group) => ({
              key: group.key,
              label: group.label,
              children: (
                <>
                  {group.fields.map(([name, field]) => (
                    <ConfigFieldControl
                      key={name}
                      name={name}
                      namePath={[name]}
                      field={field}
                      classes={classes}
                      assets={assets}
                      secrets={secrets}
                      configInstanceID={configInstanceID}
                    />
                  ))}
                </>
              ),
            }))}
          />
        )
      }}
    </Form.Item>
  )
}

export default function PluginConfigPanel({ plugin }: { plugin: PluginView }) {
  const [targetKey, setTargetKey] = useState('plugin')
  const [schema, setSchema] = useState<PluginConfigSchema | undefined>()
  const [configClasses, setConfigClasses] = useState<Record<string, PluginConfigClass>>({})
  const [schemaError, setSchemaError] = useState('')
  const [instances, setInstances] = useState<PluginConfigInstance[]>([])
  const [assets, setAssets] = useState<PluginAsset[]>([])
  const [secrets, setSecrets] = useState<PluginSecret[]>([])
  const [events, setEvents] = useState<PluginConfigEvent[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<PluginConfigInstanceDetail | null>(null)
  const [editingVersion, setEditingVersion] = useState<PluginConfigVersion | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [assetOpen, setAssetOpen] = useState(false)
  const [assetTarget, setAssetTarget] = useState<PluginAsset | null>(null)
  const [assetFile, setAssetFile] = useState<File | null>(null)
  const [secretOpen, setSecretOpen] = useState(false)
  const [acting, setActing] = useState('')
  const [createForm] = Form.useForm<{ id: string; title?: string }>()
  const [versionForm] = Form.useForm<Record<string, unknown>>()
  const [assetForm] = Form.useForm<{ id: string; kind: string; title?: string; scope: string; config_instance_id?: string }>()
  const [secretForm] = Form.useForm<{ id: string; title?: string; value: string }>()
  const assetScope = Form.useWatch('scope', assetForm)

  const targets = useMemo<ConfigTarget[]>(() => {
    const capabilityTargets = (plugin.capabilities || [])
      .filter((cap) => cap.config)
      .map((cap) => ({
        key: cap.id,
        label: `${cap.title || cap.name} · ${cap.type}`,
        scope: 'capability' as const,
        capability: cap,
      }))
    return [
      { key: 'plugin', label: '插件级配置', scope: 'plugin' as const },
      ...capabilityTargets,
    ]
  }, [plugin.capabilities])

  const target = targets.find((item) => item.key === targetKey) || targets[0]
  const assetScopeOptions = useMemo(() => (
    target.scope === 'capability'
      ? [
        { value: 'capability_shared', label: '能力共享资产' },
        { value: 'config_instance', label: '配置实例私有资产' },
      ]
      : [
        { value: 'plugin_shared', label: '插件共享资产' },
        { value: 'config_instance', label: '配置实例私有资产' },
      ]
  ), [target.scope])
  const privateAssetConfigOptions = useMemo(() => (
    instances
      .filter((item) => {
        if (item.status === 'disabled') return false
        if (target.scope === 'capability') {
          return item.scope === 'capability' && item.capability_id === target.capability?.id
        }
        return item.scope === 'plugin' && !item.capability_id
      })
      .map((item) => ({
        value: item.id,
        label: `${item.title || item.id} · ${item.status}${item.active_version ? ` · v${item.active_version}` : ''}`,
      }))
  ), [instances, target.capability?.id, target.scope])
  const configInstanceAssetBlocked = !assetTarget && assetScope === 'config_instance' && privateAssetConfigOptions.length === 0

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const schemaResp = target.scope === 'plugin'
        ? await fetchPluginConfigSchema(plugin.package.id)
        : await fetchCapabilityConfigSchema(target.capability?.id || '')
      setSchema(schemaResp.config)
      setConfigClasses(schemaResp.config_classes || {})
      setSchemaError(schemaResp.config ? '' : '当前目标没有声明配置 schema')
      const [configRows, assetRows, secretRows, eventRows] = await Promise.all([
        fetchPluginConfigs({
          plugin_id: plugin.package.id,
          capability_id: target.scope === 'capability' ? target.capability?.id : undefined,
        }),
        fetchPluginAssets({
          plugin_id: plugin.package.id,
          capability_id: target.scope === 'capability' ? target.capability?.id : undefined,
        }),
        fetchPluginSecrets({
          plugin_id: plugin.package.id,
          scope: target.scope,
        }),
        fetchPluginConfigEvents({
          plugin_id: plugin.package.id,
          limit: 50,
        }),
      ])
      setInstances(configRows)
      setAssets(assetRows)
      setSecrets(secretRows)
      setEvents(eventRows)
    } catch (err) {
      setSchema(undefined)
      setConfigClasses({})
      setSchemaError(err instanceof Error ? err.message : '加载插件配置失败')
      setInstances([])
      setAssets([])
      setSecrets([])
      setEvents([])
    } finally {
      setLoading(false)
    }
  }, [plugin.package.id, target])

  useEffect(() => {
    void load()
  }, [load])

  const openEditor = async (instanceID: string) => {
    setActing(`${instanceID}:open`)
    try {
      const detail = await fetchPluginConfig(instanceID)
      const nextVersion = detail.versions.find((version) => version.status === 'draft' || version.status === 'failed')
        || detail.active
        || detail.versions[0]
        || null
      setEditing(detail)
      setEditingVersion(nextVersion)
      versionForm.resetFields()
      versionForm.setFieldsValue(nextVersion?.values || defaultValues(schema))
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载配置实例失败')
    } finally {
      setActing('')
    }
  }

  const createInstance = async () => {
    const values = await createForm.validateFields()
    setActing('create-config')
    try {
      const instance = await createPluginConfig({
        id: values.id,
        title: values.title,
        plugin_id: plugin.package.id,
        capability_id: target.scope === 'capability' ? target.capability?.id : undefined,
        scope: target.scope,
      })
      await createPluginConfigVersion(instance.id, defaultValues(schema))
      setCreateOpen(false)
      createForm.resetFields()
      await load()
      await openEditor(instance.id)
      message.success('配置实例已创建')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '创建配置实例失败')
    } finally {
      setActing('')
    }
  }

  const saveVersion = async (): Promise<PluginConfigVersion | null> => {
    if (!editing) return null
    const values = normalizePluginConfigValues(
      schema?.fields,
      configClasses,
      versionForm.getFieldsValue(true) as Record<string, unknown>,
    )
    versionForm.setFieldsValue(values)
    setActing('save-version')
    try {
      const saved = editingVersion && (editingVersion.status === 'draft' || editingVersion.status === 'failed')
        ? await updatePluginConfigVersion(editing.instance.id, editingVersion.version, values)
        : await createPluginConfigVersion(editing.instance.id, values)
      const detail = await fetchPluginConfig(editing.instance.id)
      setEditing(detail)
      setEditingVersion(saved)
      await load()
      message.success('配置版本已保存')
      return saved
    } catch (err) {
      message.error(err instanceof Error ? err.message : '保存配置版本失败')
      return null
    } finally {
      setActing('')
    }
  }

  const validateVersion = async () => {
    const saved = await saveVersion()
    if (!editing || !saved) return
    setActing('validate-version')
    try {
      const result = await validatePluginConfigVersion(editing.instance.id, saved.version)
      const detail = await fetchPluginConfig(editing.instance.id)
      setEditing(detail)
      setEditingVersion(result.version)
      await load()
      if (result.valid) {
        message.success('配置校验通过')
      } else {
        message.error(result.errors?.join('；') || '配置校验失败')
      }
    } catch (err) {
      message.error(err instanceof Error ? err.message : '配置校验失败')
    } finally {
      setActing('')
    }
  }

  const activateVersion = async () => {
    if (!editing || !editingVersion) return
    setActing('activate-version')
    try {
      const detail = await activatePluginConfigVersion(editing.instance.id, editingVersion.version)
      setEditing(detail)
      setEditingVersion(detail.active || editingVersion)
      await load()
      message.success('配置版本已激活')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '激活配置版本失败')
    } finally {
      setActing('')
    }
  }

  const disableInstance = async (instanceID: string) => {
    setActing(`${instanceID}:disable`)
    try {
      await disablePluginConfig(instanceID)
      await load()
      message.success('配置实例已禁用')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '禁用配置实例失败')
    } finally {
      setActing('')
    }
  }

  const openCreateAsset = () => {
    setAssetTarget(null)
    setAssetFile(null)
    assetForm.resetFields()
    assetForm.setFieldsValue({
      scope: target.scope === 'capability' ? 'capability_shared' : 'plugin_shared',
    })
    setAssetOpen(true)
  }

  const openUploadAssetVersion = (asset: PluginAsset) => {
    setAssetTarget(asset)
    setAssetFile(null)
    assetForm.setFieldsValue({
      id: asset.id,
      kind: asset.kind,
      title: asset.title,
      scope: asset.scope,
      config_instance_id: asset.config_instance_id,
    })
    setAssetOpen(true)
  }

  const saveAsset = async () => {
    setActing('save-asset')
    try {
      const values = await assetForm.validateFields()
      const asset = assetTarget || await createPluginAsset({
        id: values.id,
        title: values.title,
        kind: values.kind,
        scope: values.scope,
        plugin_id: plugin.package.id,
        capability_id: values.scope === 'capability_shared' ? target.capability?.id : undefined,
        config_instance_id: values.scope === 'config_instance' ? values.config_instance_id : undefined,
      })
      if (assetFile) {
        const version = await uploadPluginAssetVersion(asset.id, assetFile)
        const validated = await validatePluginAssetVersion(asset.id, version.version)
        await activatePluginAssetVersion(asset.id, validated.version)
      }
      setAssetOpen(false)
      setAssetTarget(null)
      setAssetFile(null)
      assetForm.resetFields()
      await load()
      message.success('资产已保存')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '保存资产失败')
    } finally {
      setActing('')
    }
  }

  const saveSecret = async () => {
    setActing('save-secret')
    try {
      const values = await secretForm.validateFields()
      await upsertPluginSecret({
        id: values.id,
        title: values.title,
        value: values.value,
        plugin_id: plugin.package.id,
        scope: target.scope,
      })
      setSecretOpen(false)
      secretForm.resetFields()
      await load()
      message.success('Secret 已保存')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '保存 Secret 失败')
    } finally {
      setActing('')
    }
  }

  const configColumns: ColumnsType<PluginConfigInstance> = [
    {
      title: '配置实例',
      key: 'instance',
      render: (_, item) => (
        <Space direction="vertical" size={0}>
          <Text strong>{item.title || item.id}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>{item.id}</Text>
        </Space>
      ),
    },
    {
      title: 'Scope',
      dataIndex: 'scope',
      width: 110,
      render: (scope: string) => <Tag>{scope}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: string) => <Tag color={statusColor(status)}>{status}</Tag>,
    },
    {
      title: 'Active',
      dataIndex: 'active_version',
      width: 100,
      render: (version?: number) => version ? <Tag color="green">v{version}</Tag> : <Text type="secondary">—</Text>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 180,
      render: formatTime,
    },
    {
      title: '操作',
      key: 'actions',
      width: 190,
      render: (_, item) => (
        <Space>
          <Button
            size="small"
            icon={<EditOutlined />}
            loading={acting === `${item.id}:open`}
            onClick={() => void openEditor(item.id)}
          >
            编辑
          </Button>
          <Button
            size="small"
            danger
            disabled={item.status === 'disabled'}
            loading={acting === `${item.id}:disable`}
            onClick={() => void disableInstance(item.id)}
          >
            禁用
          </Button>
        </Space>
      ),
    },
  ]

  const assetColumns: ColumnsType<PluginAsset> = [
    { title: '资产', dataIndex: 'title', render: (title: string, item) => title || item.id },
    { title: 'Kind', dataIndex: 'kind', width: 160, render: (kind: string) => <Tag>{kind}</Tag> },
    { title: 'Scope', dataIndex: 'scope', width: 150, render: (scope: string) => <Tag>{scope}</Tag> },
    { title: '配置实例', dataIndex: 'config_instance_id', width: 180, render: (id?: string) => id || '—' },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: string) => <Tag color={statusColor(status)}>{status}</Tag> },
    { title: 'Active', dataIndex: 'active_version', width: 100, render: (version?: number) => version ? `v${version}` : '—' },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, item) => (
        <Button size="small" onClick={() => openUploadAssetVersion(item)}>
          上传版本
        </Button>
      ),
    },
  ]

  const secretColumns: ColumnsType<PluginSecret> = [
    { title: 'Secret', dataIndex: 'title', render: (title: string, item) => title || item.id },
    { title: 'Scope', dataIndex: 'scope', width: 120, render: (scope?: string) => scope || '—' },
    { title: 'Masked', dataIndex: 'masked', width: 140 },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: string) => <Tag color={statusColor(status)}>{status}</Tag> },
  ]

  const eventColumns: ColumnsType<PluginConfigEvent> = [
    { title: '时间', dataIndex: 'created_at', width: 180, render: formatTime },
    {
      title: '资源',
      key: 'resource',
      render: (_, item) => (
        <Space direction="vertical" size={0}>
          <Tag>{item.resource_type}</Tag>
          <Text type="secondary" style={{ fontSize: 12 }}>{item.resource_id}</Text>
        </Space>
      ),
    },
    { title: '操作', dataIndex: 'action', width: 120 },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: string) => <Tag color={statusColor(status)}>{status}</Tag> },
    { title: '消息', dataIndex: 'message', render: (messageText?: string) => messageText || '—' },
  ]

  return (
    <div>
      <Space wrap style={{ marginBottom: 12 }}>
        <Select
          style={{ minWidth: 280 }}
          value={target.key}
          options={targets.map((item) => ({ value: item.key, label: item.label }))}
          onChange={setTargetKey}
        />
        <Button onClick={() => void load()} loading={loading}>刷新配置</Button>
      </Space>

      {schemaError && <Alert type="info" showIcon message={schemaError} style={{ marginBottom: 12 }} />}

      <Tabs
        items={[
          {
            key: 'configs',
            label: '配置实例',
            children: (
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Space wrap>
                  <Text type="secondary">{schema?.title || '配置 schema'}</Text>
                  {schema?.validate_action && <Tag color="blue">{schema.validate_action}</Tag>}
                  {schema?.allow_plugin_config_ref && <Tag>允许引用插件级配置</Tag>}
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    disabled={!schema}
                    onClick={() => setCreateOpen(true)}
                  >
                    新建配置实例
                  </Button>
                </Space>
                <Table<PluginConfigInstance>
                  size="small"
                  rowKey="id"
                  columns={configColumns}
                  dataSource={instances}
                  loading={loading}
                  pagination={false}
                />
              </Space>
            ),
          },
          {
            key: 'assets',
            label: '资产',
            children: (
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreateAsset}>
                  新建资产
                </Button>
                <Table<PluginAsset>
                  size="small"
                  rowKey="id"
                  columns={assetColumns}
                  dataSource={assets}
                  loading={loading}
                  pagination={false}
                />
              </Space>
            ),
          },
          {
            key: 'secrets',
            label: 'Secret',
            children: (
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => {
                    secretForm.resetFields()
                    setSecretOpen(true)
                  }}
                >
                  录入 Secret
                </Button>
                <Table<PluginSecret>
                  size="small"
                  rowKey="id"
                  columns={secretColumns}
                  dataSource={secrets}
                  loading={loading}
                  pagination={false}
                />
              </Space>
            ),
          },
          {
            key: 'events',
            label: '审计事件',
            children: (
              <Table<PluginConfigEvent>
                size="small"
                rowKey="id"
                columns={eventColumns}
                dataSource={events}
                loading={loading}
                pagination={false}
              />
            ),
          },
        ]}
      />

      <Modal
        title="新建配置实例"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => void createInstance()}
        okButtonProps={{ loading: acting === 'create-config' }}
      >
        <Form form={createForm} layout="vertical">
          <Form.Item name="id" label="实例 ID" rules={[{ required: true, message: '实例 ID 必填' }]}>
            <Input placeholder="grpc-prod-common" />
          </Form.Item>
          <Form.Item name="title" label="名称">
            <Input placeholder={schema?.title || target.label} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={assetTarget ? `上传资产版本 · ${assetTarget.id}` : '新建资产'}
        open={assetOpen}
        onCancel={() => setAssetOpen(false)}
        onOk={() => void saveAsset()}
        okButtonProps={{
          loading: acting === 'save-asset',
          disabled: configInstanceAssetBlocked,
        }}
      >
        <Form form={assetForm} layout="vertical">
          <Form.Item name="id" label="资产 ID" rules={[{ required: true, message: '资产 ID 必填' }]}>
            <Input disabled={Boolean(assetTarget)} placeholder="inventory-proto" />
          </Form.Item>
          <Form.Item name="kind" label="资产类型" rules={[{ required: true, message: '资产类型必填' }]}>
            <Input disabled={Boolean(assetTarget)} placeholder="proto_files" />
          </Form.Item>
          <Form.Item name="scope" label="作用域" rules={[{ required: true, message: '作用域必填' }]}>
            <Select disabled={Boolean(assetTarget)} options={assetScopeOptions} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, curr) => prev.scope !== curr.scope}>
            {({ getFieldValue }) => (
              getFieldValue('scope') === 'config_instance' ? (
                <>
                  {privateAssetConfigOptions.length === 0 && !assetTarget ? (
                    <Alert
                      type="warning"
                      showIcon
                      message="当前目标还没有可用配置实例，无法创建配置实例私有资产"
                      description="请先在配置实例页创建配置实例，再回来创建私有资产。"
                      style={{ marginBottom: 12 }}
                    />
                  ) : null}
                  <Form.Item
                    name="config_instance_id"
                    label="配置实例"
                    rules={[{ required: true, message: '配置实例必填' }]}
                  >
                    <Select
                      disabled={Boolean(assetTarget) || privateAssetConfigOptions.length === 0}
                      showSearch
                      optionFilterProp="label"
                      placeholder={privateAssetConfigOptions.length === 0 ? '先创建配置实例' : '选择当前目标的配置实例'}
                      options={privateAssetConfigOptions}
                    />
                  </Form.Item>
                </>
              ) : null
            )}
          </Form.Item>
          <Form.Item name="title" label="名称">
            <Input disabled={Boolean(assetTarget)} />
          </Form.Item>
          <Form.Item label="文件">
            <Upload
              maxCount={1}
              beforeUpload={(file) => {
                setAssetFile(file)
                return false
              }}
              onRemove={() => {
                setAssetFile(null)
              }}
            >
              <Button>选择文件</Button>
            </Upload>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="录入 Secret"
        open={secretOpen}
        onCancel={() => setSecretOpen(false)}
        onOk={() => void saveSecret()}
        okButtonProps={{ loading: acting === 'save-secret' }}
      >
        <Form form={secretForm} layout="vertical">
          <Form.Item name="id" label="Secret ID" rules={[{ required: true, message: 'Secret ID 必填' }]}>
            <Input placeholder="sec-auth" />
          </Form.Item>
          <Form.Item name="title" label="名称">
            <Input />
          </Form.Item>
          <Form.Item name="value" label="Secret 值" rules={[{ required: true, message: 'Secret 值必填' }]}>
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={editing ? `${editing.instance.title || editing.instance.id} · v${editingVersion?.version || 'new'}` : '配置版本'}
        open={Boolean(editing)}
        onCancel={() => {
          setEditing(null)
          setEditingVersion(null)
          versionForm.resetFields()
        }}
        width={760}
        footer={[
          <Button key="close" onClick={() => {
            setEditing(null)
            setEditingVersion(null)
            versionForm.resetFields()
          }}>关闭</Button>,
          <Button key="save" icon={<SaveOutlined />} loading={acting === 'save-version'} onClick={() => void saveVersion()}>
            保存
          </Button>,
          <Button key="validate" icon={<CheckCircleOutlined />} loading={acting === 'validate-version'} onClick={() => void validateVersion()}>
            校验
          </Button>,
          <Button key="activate" type="primary" icon={<ThunderboltOutlined />} loading={acting === 'activate-version'} onClick={() => void activateVersion()}>
            激活
          </Button>,
        ]}
      >
        {editingVersion?.validation_error && (
          <Alert type="error" showIcon message={editingVersion.validation_error} style={{ marginBottom: 12 }} />
        )}
        {editing && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Space wrap>
              {(editing.versions || []).map((version) => (
                <Button
                  key={version.version}
                  size="small"
                  type={editingVersion?.version === version.version ? 'primary' : 'default'}
                  onClick={() => {
                    setEditingVersion(version)
                    versionForm.resetFields()
                    versionForm.setFieldsValue(version.values || defaultValues(schema))
                  }}
                >
                  v{version.version} <Tag color={statusColor(version.status)} style={{ marginInlineStart: 6 }}>{version.status}</Tag>
                </Button>
              ))}
            </Space>
            <Form form={versionForm} layout="vertical" initialValues={editingVersion?.values || defaultValues(schema)}>
              <ConfigVersionForm
                schema={schema}
                classes={configClasses}
                assets={assets}
                secrets={secrets}
                configInstanceID={editing.instance.id}
              />
            </Form>
          </Space>
        )}
      </Modal>
    </div>
  )
}
