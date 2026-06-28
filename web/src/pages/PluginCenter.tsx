import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Popconfirm,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Upload,
  Typography,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  CheckCircleOutlined,
  DownloadOutlined,
  ExportOutlined,
  ReloadOutlined,
  RollbackOutlined,
  StopOutlined,
  ThunderboltOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import type { PluginCatalog, PluginCapability, PluginRelease, PluginView } from '../api/types'
import {
  activatePluginRelease,
  disablePlugin,
  enablePlugin,
  exportPluginRelease,
  fetchPluginCatalog,
  gcPlugins,
  importPluginArchive,
  reloadPlugins,
  rollbackPlugin,
  validatePluginRelease,
} from '../api/client'

const { Title, Text } = Typography

function statusColor(status: string): string {
  switch (status) {
    case 'enabled':
    case 'active':
    case 'ready':
    case 'validated':
      return 'green'
    case 'disabled':
    case 'draining':
    case 'retired':
      return 'default'
    case 'degraded':
    case 'failed':
      return 'red'
    case 'staged':
    case 'validating':
      return 'gold'
    default:
      return 'blue'
  }
}

function capabilityLabel(type: string): string {
  const labels: Record<string, string> = {
    task_template: '模板',
    task_driver: '任务驱动',
    data_source: '数据源',
    ai_data_source: 'AI 数据源',
    output_writer: '输出写入',
    evaluator: 'Evaluator',
    trace_sink: 'Trace Sink',
    hook: 'Hook',
    ui_extension: '前端入口',
  }
  return labels[type] || type
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function PluginCapabilities({ capabilities }: { capabilities: PluginCapability[] }) {
  const navigate = useNavigate()
  const groups = useMemo(() => {
    const map = new Map<string, PluginCapability[]>()
    for (const cap of capabilities) {
      const list = map.get(cap.type) || []
      list.push(cap)
      map.set(cap.type, list)
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b))
  }, [capabilities])

  if (groups.length === 0) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前 generation 未暴露能力" />
  }

  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      {groups.map(([type, caps]) => (
        <div key={type}>
          <Text strong>{capabilityLabel(type)}</Text>
          <div style={{ marginTop: 6 }}>
            <Space wrap size={[6, 6]}>
              {caps.map((cap) => {
                const extensionURL = cap.type === 'ui_extension' && cap.path
                  ? `/plugins/extension?plugin=${encodeURIComponent(cap.plugin_id)}&capability=${encodeURIComponent(cap.name)}`
                  : ''
                return (
                  <Tooltip key={cap.id} title={cap.description || cap.id}>
                    <Tag color={cap.enabled ? 'blue' : 'default'}>
                      {cap.title || cap.name}
                      {cap.runtime ? ` · ${cap.runtime}` : ''}
                      {extensionURL && (
                        <Button
                          type="link"
                          size="small"
                          icon={<ExportOutlined />}
                          style={{ padding: '0 0 0 6px', height: 18 }}
                          onClick={(event) => {
                            event.preventDefault()
                            navigate(extensionURL)
                          }}
                        />
                      )}
                    </Tag>
                  </Tooltip>
                )
              })}
            </Space>
          </div>
        </div>
      ))}
    </Space>
  )
}

export default function PluginCenter() {
  const [catalog, setCatalog] = useState<PluginCatalog | null>(null)
  const [loading, setLoading] = useState(true)
  const [acting, setActing] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const loadCatalog = useCallback(async () => {
    try {
      const data = await fetchPluginCatalog()
      setCatalog(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载插件中心失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadCatalog()
  }, [loadCatalog])

  const runCatalogAction = async (key: string, action: () => Promise<PluginCatalog>, success: string) => {
    try {
      setActing(key)
      const data = await action()
      setCatalog(data)
      message.success(success)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActing(null)
    }
  }

  const runReleaseAction = async (key: string, action: () => Promise<unknown>, success: string) => {
    try {
      setActing(key)
      await action()
      await loadCatalog()
      message.success(success)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActing(null)
    }
  }

  const importArchive = async (file: File) => {
    try {
      setActing('import')
      await importPluginArchive(file)
      await loadCatalog()
      message.success('插件包已导入')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '导入失败')
    } finally {
      setActing(null)
    }
  }

  const exportRelease = async (pluginID: string, version: string) => {
    const key = `${pluginID}:${version}:export`
    try {
      setActing(key)
      const blob = await exportPluginRelease(pluginID, version)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `${pluginID.replace(/[/:\\]/g, '_')}-${version}.tar.gz`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '导出失败')
    } finally {
      setActing(null)
    }
  }

  const releaseColumns = (pluginID: string): ColumnsType<PluginRelease> => [
    {
      title: '版本',
      dataIndex: 'version',
      width: 180,
      render: (version: string, release) => (
        <Space direction="vertical" size={0}>
          <Text strong>{version}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>{release.path || (release.bundled ? 'bundled official' : '—')}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (status: string) => <Tag color={statusColor(status)}>{status}</Tag>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 190,
      render: formatTime,
    },
    {
      title: '校验',
      dataIndex: 'validation_error',
      render: (value: string) => value ? <Text type="danger">{value}</Text> : <Text type="secondary">通过或未校验错误</Text>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      render: (_, release) => (
        <Space wrap>
          <Button
            size="small"
            icon={<CheckCircleOutlined />}
            loading={acting === `${pluginID}:${release.version}:validate`}
            onClick={() => runReleaseAction(
              `${pluginID}:${release.version}:validate`,
              () => validatePluginRelease(pluginID, release.version),
              'Release 校验完成',
            )}
          >
            校验
          </Button>
          <Button
            size="small"
            type="primary"
            icon={<ThunderboltOutlined />}
            loading={acting === `${pluginID}:${release.version}:activate`}
            onClick={() => runCatalogAction(
              `${pluginID}:${release.version}:activate`,
              () => activatePluginRelease(pluginID, release.version),
              'Release 已激活',
            )}
          >
            激活
          </Button>
          {!release.bundled && (
            <Button
              size="small"
              icon={<DownloadOutlined />}
              loading={acting === `${pluginID}:${release.version}:export`}
              onClick={() => void exportRelease(pluginID, release.version)}
            >
              导出
            </Button>
          )}
        </Space>
      ),
    },
  ]

  const columns: ColumnsType<PluginView> = [
    {
      title: '插件',
      key: 'plugin',
      render: (_, item) => (
        <Space direction="vertical" size={1}>
          <Space wrap>
            <Text strong>{item.package.name}</Text>
            {item.package.official && <Tag color="blue">official</Tag>}
            {item.package.bundled && <Tag>bundled</Tag>}
          </Space>
          <Text type="secondary" style={{ fontSize: 12 }}>{item.package.id}</Text>
          {item.package.description && <Text type="secondary">{item.package.description}</Text>}
        </Space>
      ),
    },
    {
      title: 'Active',
      dataIndex: 'active_version',
      width: 130,
      render: (version: string) => version ? <Tag color="green">{version}</Tag> : <Text type="secondary">—</Text>,
    },
    {
      title: '状态',
      key: 'status',
      width: 130,
      render: (_, item) => <Tag color={statusColor(item.package.status)}>{item.package.status}</Tag>,
    },
    {
      title: '能力',
      key: 'capabilities',
      width: 120,
      render: (_, item) => <Tag color="blue">{item.capabilities.length}</Tag>,
    },
    {
      title: '权限',
      key: 'permissions',
      render: (_, item) => (
        <Space wrap size={[4, 4]}>
          {(item.permissions || []).length === 0
            ? <Text type="secondary">—</Text>
            : item.permissions?.map((perm) => <Tag key={perm}>{perm}</Tag>)}
        </Space>
      ),
    },
    {
      title: '最近错误',
      dataIndex: ['package', 'last_error'],
      render: (value: string) => value ? <Text type="danger">{value}</Text> : <Text type="secondary">—</Text>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 240,
      render: (_, item) => {
        const enabled = item.package.status === 'enabled'
        return (
          <Space wrap>
            {enabled ? (
              <Popconfirm
                title="禁用插件后，新任务和新运行将不能使用该插件能力。"
                okText="禁用"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                onConfirm={() => runCatalogAction(
                  `${item.package.id}:disable`,
                  () => disablePlugin(item.package.id),
                  '插件已禁用',
                )}
              >
                <Button size="small" danger icon={<StopOutlined />} loading={acting === `${item.package.id}:disable`}>
                  禁用
                </Button>
              </Popconfirm>
            ) : (
              <Button
                size="small"
                type="primary"
                icon={<ThunderboltOutlined />}
                loading={acting === `${item.package.id}:enable`}
                onClick={() => runCatalogAction(
                  `${item.package.id}:enable`,
                  () => enablePlugin(item.package.id),
                  '插件已启用',
                )}
              >
                启用
              </Button>
            )}
            <Button
              size="small"
              icon={<RollbackOutlined />}
              loading={acting === `${item.package.id}:rollback`}
              onClick={() => runCatalogAction(
                `${item.package.id}:rollback`,
                () => rollbackPlugin(item.package.id),
                '插件已回滚',
              )}
            >
              回滚
            </Button>
          </Space>
        )
      },
    },
  ]

  if (loading) {
    return (
      <div className="page-shell" style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Title level={2} className="page-title">插件中心</Title>
          <Text className="page-subtitle">
            管理插件 catalog、release 状态和当前 active generation。
          </Text>
        </div>
        <Space wrap>
          <Button
            icon={<ReloadOutlined />}
            loading={acting === 'reload'}
            onClick={() => runCatalogAction('reload', reloadPlugins, '插件目录已重新扫描')}
          >
            重新扫描
          </Button>
          <Button
            loading={acting === 'gc'}
            onClick={() => runCatalogAction('gc', gcPlugins, 'GC 操作已记录')}
          >
            GC
          </Button>
          <Upload
            accept=".tar.gz,.tgz,application/gzip"
            showUploadList={false}
            beforeUpload={(file) => {
              void importArchive(file)
              return false
            }}
          >
            <Button icon={<UploadOutlined />} loading={acting === 'import'}>导入</Button>
          </Upload>
        </Space>
      </div>

      {error && (
        <Alert
          type="error"
          showIcon
          message="插件中心加载失败"
          description={error}
          action={<Button size="small" onClick={loadCatalog}>重试</Button>}
          style={{ marginBottom: 14 }}
        />
      )}

      {catalog?.errors && catalog.errors.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message="插件 catalog 存在降级项"
          description={catalog.errors.join('；')}
          style={{ marginBottom: 14 }}
        />
      )}

      <div className="metric-strip" style={{ marginBottom: 14 }}>
        <div className="metric-tile">
          <div className="metric-label">Catalog 状态</div>
          <div className="metric-value" style={{ fontSize: 24 }}>{catalog?.status || '—'}</div>
        </div>
        <div className="metric-tile">
          <div className="metric-label">插件总数</div>
          <div className="metric-value">{catalog?.stats.total ?? 0}</div>
        </div>
        <div className="metric-tile">
          <div className="metric-label">启用插件</div>
          <div className="metric-value">{catalog?.stats.enabled ?? 0}</div>
        </div>
        <div className="metric-tile">
          <div className="metric-label">能力数量</div>
          <div className="metric-value">{catalog?.stats.capabilities ?? 0}</div>
        </div>
      </div>

      <Card className="ops-card" style={{ marginBottom: 14 }}>
        <Descriptions column={{ xs: 1, sm: 2, lg: 3 }} size="small">
          <Descriptions.Item label="插件目录">{catalog?.plugin_dir || '—'}</Descriptions.Item>
          <Descriptions.Item label="Active Generation">{catalog?.active_generation_id || '—'}</Descriptions.Item>
          <Descriptions.Item label="生成时间">{formatTime(catalog?.generated_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card className="ops-card" title="插件列表">
        <Table<PluginView>
          className="dense-table"
          rowKey={(item) => item.package.id}
          columns={columns}
          dataSource={catalog?.plugins || []}
          pagination={false}
          expandable={{
            expandedRowRender: (item) => (
              <Space direction="vertical" size={14} style={{ width: '100%' }}>
                <PluginCapabilities capabilities={item.capabilities || []} />
                <Table<PluginRelease>
                  size="small"
                  rowKey={(release) => `${release.plugin_id}:${release.version}`}
                  columns={releaseColumns(item.package.id)}
                  dataSource={item.releases || []}
                  pagination={false}
                  locale={{ emptyText: '暂无 release' }}
                />
              </Space>
            ),
          }}
          locale={{ emptyText: <Empty description="暂无插件" /> }}
        />
      </Card>
    </div>
  )
}
