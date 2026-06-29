import type { PluginConfigClass, PluginConfigField } from '../api/types'

type ReferenceKey = 'asset_id' | 'secret_id'

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export function configReferenceID(value: unknown, key: ReferenceKey): string | undefined {
  if (typeof value === 'string') {
    const text = value.trim()
    return text || undefined
  }
  if (isRecord(value)) {
    const id = value[key]
    return typeof id === 'string' && id.trim() ? id.trim() : undefined
  }
  return undefined
}

function toReference(value: unknown, key: ReferenceKey): unknown {
  const id = configReferenceID(value, key)
  if (!id) return undefined
  return isRecord(value) ? { ...value, [key]: id } : { [key]: id }
}

export function toAssetReference(value: unknown): unknown {
  return toReference(value, 'asset_id')
}

export function toSecretReference(value: unknown): unknown {
  return toReference(value, 'secret_id')
}

function normalizeFieldValue(
  field: PluginConfigField,
  classes: Record<string, PluginConfigClass>,
  value: unknown,
): unknown {
  if (value === undefined || value === null || value === '') return value

  switch (field.type) {
    case 'file':
      return toAssetReference(value)
    case 'secret':
      return toSecretReference(value)
    case 'object': {
      if (!field.class || field.class === 'JSONObject' || !classes[field.class]?.fields || !isRecord(value)) {
        return value
      }
      return normalizePluginConfigValues(classes[field.class].fields || {}, classes, value)
    }
    case 'array': {
      if (!field.items || !Array.isArray(value)) return value
      return value.map((item) => normalizeFieldValue(field.items as PluginConfigField, classes, item))
    }
    default:
      return value
  }
}

export function normalizePluginConfigValues(
  fields: Record<string, PluginConfigField> | undefined,
  classes: Record<string, PluginConfigClass>,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...values }
  for (const [name, field] of Object.entries(fields || {})) {
    if (Object.prototype.hasOwnProperty.call(next, name)) {
      next[name] = normalizeFieldValue(field, classes, next[name])
    }
  }
  return next
}
