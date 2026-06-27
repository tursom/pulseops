import { Form } from 'antd'
import type { FormInstance } from 'antd'
import type { NamePath } from 'antd/es/form/interface'

export function useWatchedFormValue<T>(
  form: FormInstance | undefined,
  namePath: NamePath,
  fallback?: T,
): T | undefined {
  const watched = Form.useWatch(namePath, form) as T | undefined
  if (watched !== undefined) return watched

  const current = form?.getFieldValue(namePath) as T | undefined
  if (current !== undefined) return current

  return fallback
}
