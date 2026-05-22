import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Card, Row, Col, Spin, Alert, Typography } from 'antd'
import { fetchTasks } from '../api/client'
import type { TaskState } from '../api/types'
export default function Dashboard() {
  const [tasks, setTasks] = useState<TaskState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  const loadTasks = async () => {
    try {
      const data = await fetchTasks()
      setTasks(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadTasks()
    const interval = setInterval(loadTasks, 15000)
    return () => clearInterval(interval)
  }, [])
  const totalTasks = tasks.length
  const enabledTasks = tasks.filter((t) => t.enabled).length
  const failedTasks = tasks.filter((t) => t.last_run_status === 'failed').length
  const errorTasks = tasks.filter((t) => t.last_error !== '').length
  if (error) {
    return (
      <div style={{ padding: 24 }}>
        <Alert
          type="error"
          message="加载任务失败"
          description={error}
          showIcon
        />
      </div>
    )
  }

  return (
    <div style={{ padding: 24 }}>
      <Typography.Title level={3} style={{ marginBottom: 24 }}>
        仪表盘
      </Typography.Title>

      {loading ? (
        <div style={{ textAlign: 'center', padding: 80 }}>
          <Spin size="large" />
        </div>
      ) : (
        <>
          <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
            <Col xs={24} sm={12} md={6}>
              <Card hoverable onClick={() => navigate('/tasks')}>
                <Typography.Text type="secondary">任务总数</Typography.Text>
                <Typography.Title level={2} style={{ margin: '8px 0 0' }}>
                  {totalTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card hoverable onClick={() => navigate('/tasks')}>
                <Typography.Text type="secondary">已启用</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#52c41a' }}
                >
                  {enabledTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card hoverable onClick={() => navigate('/tasks?runStatus=failed')}>
                <Typography.Text type="secondary">失败</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#ff4d4f' }}
                >
                  {failedTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card hoverable onClick={() => navigate('/tasks?error=1')}>
                <Typography.Text type="secondary">有错误</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#faad14' }}
                >
                  {errorTasks}
                </Typography.Title>
              </Card>
            </Col>
          </Row>

          <div style={{ textAlign: 'right', marginTop: 8 }}>
            <Link to="/tasks">查看全部任务 →</Link>
          </div>
        </>
      )}
    </div>
  )
}
