import { useEffect, useMemo, useState } from 'react'
import { Form, Input, InputNumber, Select, Space, Spin, Switch, Tag } from 'antd'
import type { NamePath } from 'antd/es/form/interface'
import { fetchPluginConfigs } from '../../../api/client'
import type { PluginCapability, PluginConfigClass, PluginConfigField, PluginConfigInstance } from '../../../api/types'
import {
  configReferenceID,
  normalizePluginConfigValues,
  toAssetReference,
  toSecretReference,
} from '../../../utils/pluginConfigRefs'

function sortedOverridableFields(capability: PluginCapability): Array<[string, PluginConfigField]> {
  return Object.entries(capability.config?.fields || {})
    .filter(([, field]) => field.overridable)
    .sort(([, a], [, b]) => (a.ui?.order || 0) - (b.ui?.order || 0))
}

function configOptions(instances: PluginConfigInstance[]) {
  return instances
    .filter((item) => item.status === 'active' && item.active_version)
    .map((item) => ({
      value: item.id,
      label: `${item.title || item.id} #${item.active_version}`,
    }))
}

function parseJSONInput(raw: string): unknown {
  const text = raw.trim()
  if (!text) return undefined
  try {
    return JSON.parse(text)
  } catch {
    return raw
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function JSONValueField({
  commonProps,
  placeholder,
  width = 240,
  normalize,
}: {
  commonProps: Record<string, unknown>
  placeholder: string
  width?: number
  normalize?: (value: unknown) => unknown
}) {
  return (
    <Form.Item
      {...commonProps}
      getValueFromEvent={(event) => {
        const value = parseJSONInput(event.target.value)
        return normalize ? normalize(value) : value
      }}
      getValueProps={(value) => ({
        value: value && typeof value === 'object' ? JSON.stringify(value, null, 2) : value,
      })}
    >
      <Input.TextArea rows={1} placeholder={placeholder} style={{ width }} />
    </Form.Item>
  )
}

function appendName(baseName: NamePath, ...parts: Array<string | number>): NamePath {
  return [...(Array.isArray(baseName) ? baseName : [baseName]), ...parts]
}

function normalizeOverrideValue(
  fieldName: string,
  field: PluginConfigField,
  classes: Record<string, PluginConfigClass>,
  value: unknown,
): unknown {
  return normalizePluginConfigValues({ [fieldName]: field }, classes, { [fieldName]: value })[fieldName]
}

function OverrideField({
  fieldName,
  field,
  classes,
  baseName,
  restField,
}: {
  fieldName: string
  field: PluginConfigField
  classes: Record<string, PluginConfigClass>
  baseName: NamePath
  restField?: Record<string, unknown>
}) {
  const label = field.ui?.label || fieldName
  const commonProps = {
    ...(restField || {}),
    name: appendName(baseName, 'overrides', fieldName),
    label,
    tooltip: field.ui?.help || field.description,
    preserve: false,
    style: { marginBottom: 0 },
  }

  switch (field.type) {
    case 'number':
      return (
        <Form.Item {...commonProps}>
          <InputNumber
            style={{ width: 140 }}
            min={field.validation?.min}
            max={field.validation?.max}
            step={field.validation?.step}
          />
        </Form.Item>
      )
    case 'bool':
      return (
        <Form.Item {...commonProps} valuePropName="checked">
          <Switch />
        </Form.Item>
      )
    case 'select':
      return (
        <Form.Item {...commonProps}>
          <Select
            allowClear
            style={{ width: 180 }}
            options={(field.options || []).map((option) => ({
              value: option.value as string | number | boolean,
              label: option.label || String(option.value),
            }))}
          />
        </Form.Item>
      )
    case 'multi_select':
      return (
        <Form.Item {...commonProps}>
          <Select
            mode="multiple"
            allowClear
            style={{ width: 220 }}
            options={(field.options || []).map((option) => ({
              value: option.value as string | number | boolean,
              label: option.label || String(option.value),
            }))}
          />
        </Form.Item>
      )
    case 'object':
      return (
        <JSONValueField
          commonProps={commonProps}
          placeholder={field.ui?.placeholder || '{}'}
          normalize={(value) => normalizeOverrideValue(fieldName, field, classes, value)}
        />
      )
    case 'array':
      if (field.items?.type === 'file') {
        return (
          <JSONValueField
            commonProps={commonProps}
            placeholder='[{"asset_id":"asset-id"}]'
            normalize={(value) => Array.isArray(value) ? value.map(toAssetReference).filter(Boolean) : value}
          />
        )
      }
      return (
        <JSONValueField
          commonProps={commonProps}
          placeholder={field.ui?.placeholder || '[]'}
          normalize={(value) => normalizeOverrideValue(fieldName, field, classes, value)}
        />
      )
    case 'file':
      return (
        <JSONValueField
          commonProps={commonProps}
          placeholder='{"asset_id":"asset-id"}'
          width={220}
          normalize={toAssetReference}
        />
      )
    case 'secret':
      return (
        <Form.Item
          {...commonProps}
          getValueFromEvent={(event) => toSecretReference(event.target.value)}
          getValueProps={(value) => ({ value: configReferenceID(value, 'secret_id') })}
        >
          <Input placeholder="secret_id" style={{ width: 180 }} />
        </Form.Item>
      )
    default:
      return (
        <Form.Item {...commonProps}>
          <Input placeholder={field.ui?.placeholder || field.description || fieldName} style={{ width: 180 }} />
        </Form.Item>
      )
  }
}

export default function PluginConfigRefFields({
  capability,
  name,
  restField,
  baseName,
  absoluteBaseName,
}: {
  capability: PluginCapability
  name?: number
  restField?: Record<string, unknown>
  baseName?: NamePath
  absoluteBaseName?: NamePath
}) {
  const fieldBaseName = useMemo<NamePath>(() => baseName ?? [name ?? 0], [baseName, name])
  const normalizeBaseName = useMemo<NamePath>(
    () => absoluteBaseName ?? fieldBaseName,
    [absoluteBaseName, fieldBaseName],
  )
  const form = Form.useFormInstance()
  const [pluginConfigs, setPluginConfigs] = useState<PluginConfigInstance[]>([])
  const [capabilityConfigs, setCapabilityConfigs] = useState<PluginConfigInstance[]>([])
  const [loading, setLoading] = useState(false)
  const allowPluginConfigRef = Boolean(capability.config?.allow_plugin_config_ref)
  const overrideFields = useMemo(() => sortedOverridableFields(capability), [capability])
  const pluginOptions = useMemo(
    () => configOptions(pluginConfigs.filter((item) => item.scope === 'plugin')),
    [pluginConfigs],
  )
  const capabilityOptions = useMemo(() => configOptions(capabilityConfigs), [capabilityConfigs])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all([
      allowPluginConfigRef ? fetchPluginConfigs({ plugin_id: capability.plugin_id }) : Promise.resolve([]),
      fetchPluginConfigs({ capability_id: capability.id }),
    ])
      .then(([pluginRows, capabilityRows]) => {
        if (cancelled) return
        setPluginConfigs(pluginRows)
        setCapabilityConfigs(capabilityRows)
      })
      .catch(() => {
        if (cancelled) return
        setPluginConfigs([])
        setCapabilityConfigs([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [allowPluginConfigRef, capability.id, capability.plugin_id])

  useEffect(() => {
    const fields = capability.config?.fields || {}
    if (Object.keys(fields).length === 0) return
    const overridePath = appendName(normalizeBaseName, 'overrides')
    const current = form.getFieldValue(overridePath)
    if (!isRecord(current)) return
    const normalized = normalizePluginConfigValues(fields, capability.config_classes || {}, current)
    if (JSON.stringify(normalized) !== JSON.stringify(current)) {
      form.setFieldValue(overridePath, normalized)
    }
  }, [capability.config?.fields, capability.config_classes, form, normalizeBaseName])

  return (
    <>
      <Space wrap align="baseline">
        {allowPluginConfigRef ? (
          <Form.Item
            {...(restField || {})}
            name={appendName(fieldBaseName, 'plugin_config_ref')}
            label="插件配置"
            preserve={false}
          >
            <Select
              allowClear
              loading={loading}
              notFoundContent={loading ? <Spin size="small" /> : '无可用配置'}
              placeholder="公共配置"
              style={{ width: 220 }}
              options={pluginOptions}
              showSearch
              optionFilterProp="label"
            />
          </Form.Item>
        ) : null}
        <Form.Item
          {...(restField || {})}
          name={appendName(fieldBaseName, 'capability_config_ref')}
          label="能力配置"
          preserve={false}
        >
          <Select
            allowClear
            loading={loading}
            notFoundContent={loading ? <Spin size="small" /> : '无可用配置'}
            placeholder="调用配置"
            style={{ width: 220 }}
            options={capabilityOptions}
            showSearch
            optionFilterProp="label"
          />
        </Form.Item>
        {capability.config?.validate_action ? <Tag>validate_config</Tag> : null}
      </Space>
      {overrideFields.length > 0 ? (
        <Space wrap align="baseline">
          {overrideFields.map(([fieldName, field]) => (
            <OverrideField
              key={fieldName}
              fieldName={fieldName}
              field={field}
              classes={capability.config_classes || {}}
              baseName={fieldBaseName}
              restField={restField}
            />
          ))}
        </Space>
      ) : null}
    </>
  )
}
