import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { Tree, Input, Spin, Alert, Button, Segmented, Typography, Empty, Tag } from 'antd'
import { ReloadOutlined, EyeOutlined, EditOutlined } from '@ant-design/icons'
import type { DataNode } from 'antd/es/tree'
import { fetchTaskSample } from '../api/client'

const { Text } = Typography

interface JsonFieldPickerProps {
  sourceTaskId: string | null
  source: string
  value?: string
  onChange?: (expr: string) => void
}

function resolveValue(data: unknown, path: string): unknown {
  if (!data || !path) return undefined
  // Convert jq path to dot-path: .data.items[0].name → data.items.0.name
  const dotPath = path
    .replace(/^\./, '')
    .replace(/\[(\d+)\]/g, '.$1')
  const parts = dotPath.split('.')
  let current: any = data
  for (const part of parts) {
    if (current === null || current === undefined) return undefined
    if (Array.isArray(current)) {
      const idx = parseInt(part, 10)
      if (isNaN(idx)) return undefined
      current = current[idx]
    } else if (typeof current === 'object') {
      current = current[part]
    } else {
      return undefined
    }
  }
  return current
}

interface BuildContext {
  path: string
}

function jsonToTreeNodes(data: unknown, ctx: BuildContext): DataNode[] {
  if (data === null || data === undefined) {
    return [{
      title: <Text type="secondary">null</Text>,
      key: ctx.path,
      isLeaf: true,
      selectable: false,
    }]
  }

  if (typeof data === 'string' || typeof data === 'number' || typeof data === 'boolean') {
    const display = typeof data === 'string'
      ? (data.length > 40 ? `"${data.slice(0, 40)}..."` : `"${data}"`)
      : String(data)
    return [{
      title: <Text type="secondary">{display}</Text>,
      key: ctx.path,
      isLeaf: true,
      selectable: false,
    }]
  }

  if (Array.isArray(data)) {
    if (data.length === 0) {
      return [{ title: <Text type="secondary">[] (空)</Text>, key: ctx.path, isLeaf: true, selectable: false }]
    }
    return data.slice(0, 20).map((item, i) => {
      const childPath = `${ctx.path}[${i}]`
      const children = jsonToTreeNodes(item, { path: childPath })
      return {
        title: <Text style={{ color: '#d46b08' }}>[{i}]</Text>,
        key: childPath,
        children: children.length === 1 && children[0].isLeaf ? undefined : children,
        selectable: false,
      }
    })
  }

  if (typeof data === 'object') {
    const entries = Object.entries(data as Record<string, unknown>)
    if (entries.length === 0) {
      return [{ title: <Text type="secondary">{'{} (空)'}</Text>, key: ctx.path, isLeaf: true, selectable: false }]
    }
    return entries.map(([key, value]) => {
      const childPath = ctx.path ? `${ctx.path}.${key}` : `.${key}`
      const isPrimitive = value === null || typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean'
      if (isPrimitive) {
        let display = ''
        if (value === null) display = 'null'
        else if (typeof value === 'string') display = value.length > 30 ? `"${value.slice(0, 30)}..."` : `"${value}"`
        else display = String(value)
        return {
          title: (
            <span>
              <Text strong>{key}</Text>
              <Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>: {display}</Text>
            </span>
          ),
          key: childPath,
          isLeaf: true,
          selectable: true,
        }
      }
      const isArray = Array.isArray(value)
      return {
        title: (
          <span>
            <Text>{key}</Text>
            {isArray && (
              <Tag style={{ marginLeft: 6, fontSize: 10, lineHeight: '16px' }} color="orange">
                {(value as unknown[]).length} items
              </Tag>
            )}
          </span>
        ),
        key: childPath,
        children: jsonToTreeNodes(value, { path: childPath }),
        selectable: false,
      }
    })
  }

  return [{ title: String(data), key: ctx.path, isLeaf: true, selectable: false }]
}

export default function JsonFieldPicker({ sourceTaskId, source, value, onChange }: JsonFieldPickerProps) {
  const [mode, setMode] = useState<'visual' | 'raw'>('visual')
  const [sample, setSample] = useState<unknown>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [noData, setNoData] = useState(false)
  const loadIdRef = useRef(0)

  const fetchSample = useCallback(async () => {
    if (!sourceTaskId || !source) return
    const loadId = ++loadIdRef.current
    setLoading(true)
    setError(null)
    setNoData(false)
    try {
      const resp = await fetchTaskSample(sourceTaskId, source)
      if (loadId !== loadIdRef.current) return
      if (resp.available && resp.data) {
        setSample(resp.data)
      } else {
        setNoData(true)
        setSample(null)
      }
    } catch (err) {
      if (loadId !== loadIdRef.current) return
      setError(err instanceof Error ? err.message : '加载样本数据失败')
      setSample(null)
    } finally {
      if (loadId === loadIdRef.current) setLoading(false)
    }
  }, [sourceTaskId, source])

  useEffect(() => {
    setSample(null)
    setError(null)
    setNoData(false)
    fetchSample()
  }, [fetchSample])

  const treeData = useMemo(() => {
    if (!sample) return []
    return jsonToTreeNodes(sample, { path: '' })
  }, [sample])

  const previewValue = useMemo(() => {
    if (!value || !sample) return undefined
    return resolveValue(sample, value)
  }, [value, sample])

  const handleSelect = useCallback((keys: React.Key[]) => {
    if (keys.length > 0 && onChange) {
      onChange(String(keys[0]))
    }
  }, [onChange])

  const handleRefresh = () => fetchSample()

  if (!sourceTaskId) {
    return <Text type="secondary">请先选择上游任务</Text>
  }

  if (!source) {
    return <Text type="secondary">选择数据源后自动获取样例数据</Text>
  }

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <Segmented
          size="small"
          value={mode}
          onChange={(v) => setMode(v as 'visual' | 'raw')}
          options={[
            { value: 'visual', label: <span><EyeOutlined /> 可视</span> },
            { value: 'raw', label: <span><EditOutlined /> 手写</span> },
          ]}
        />
        {mode === 'visual' && (
          <Button size="small" icon={<ReloadOutlined />} onClick={handleRefresh} loading={loading}>
            刷新
          </Button>
        )}
      </div>

      {mode === 'raw' ? (
        <Input
          value={value}
          onChange={(e) => onChange?.(e.target.value)}
          placeholder="如 .duration_ms"
        />
      ) : loading ? (
        <div style={{ textAlign: 'center', padding: 24 }}>
          <Spin size="small" />
          <div style={{ marginTop: 8 }}>
            <Text type="secondary">加载样本数据...</Text>
          </div>
        </div>
      ) : error ? (
        <Alert type="error" message="加载失败" description={error} showIcon
          action={<Button size="small" onClick={handleRefresh}>重试</Button>}
        />
      ) : noData ? (
        <Alert type="info" message="暂无样本数据"
          description="上游任务尚无成功运行记录，请先触发一次运行。"
          showIcon
        />
      ) : !sample ? (
        <Empty description="所选数据源无数据" />
      ) : (
        <>
          <div style={{ maxHeight: 260, overflow: 'auto', border: '1px solid #d9d9d9', borderRadius: 6, padding: '4px 0' }}>
            <Tree
              treeData={treeData}
              onSelect={handleSelect}
              selectedKeys={value ? [value] : []}
              defaultExpandAll={false}
              showLine={{ showLeafIcon: false }}
              style={{ fontSize: 13 }}
            />
          </div>
          {value && (
            <div style={{
              marginTop: 8, padding: '6px 10px', background: '#f6f8fa',
              borderRadius: 6, fontSize: 12, display: 'flex', alignItems: 'center', gap: 8
            }}>
              <Text type="secondary" style={{ flexShrink: 0 }}>JQ:</Text>
              <Text code style={{ fontSize: 12 }}>{value}</Text>
              {previewValue !== undefined && (
                <>
                  <Text type="secondary" style={{ flexShrink: 0 }}>→</Text>
                  <Text style={{
                    fontFamily: 'monospace', color: '#1a8000',
                    maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'
                  }}>
                    {typeof previewValue === 'string' ? previewValue : JSON.stringify(previewValue)}
                  </Text>
                </>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
