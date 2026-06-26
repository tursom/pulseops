import { Routes, Route, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu } from 'antd'
import {
  ApartmentOutlined,
  DashboardOutlined,
  SettingOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import Dashboard from './pages/Dashboard'
import TaskList from './pages/TaskList'
import TaskDetail from './pages/TaskDetail'
import RunDetail from './pages/RunDetail'
import TaskEditor from './pages/TaskEditor'
import PipelineList from './pages/PipelineList'
import PipelineEditor from './pages/PipelineEditor'
import Settings from './pages/Settings'
import './App.css'

const { Sider, Content } = Layout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '工作台' },
  { key: '/tasks', icon: <UnorderedListOutlined />, label: '任务监控' },
  { key: '/pipelines', icon: <ApartmentOutlined />, label: '依赖拓扑' },
  { key: '/settings', icon: <SettingOutlined />, label: '平台设置' },
]

export default function App() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = '/' + location.pathname.split('/')[1]

  return (
    <Layout className="app-shell">
      <Sider width={224} breakpoint="lg" collapsedWidth={0} className="app-sider">
        <div className="app-brand">
          <div className="app-brand-mark">P</div>
          <div>
            <div className="app-brand-title">PulseOps</div>
            <div className="app-brand-subtitle">运维工作台</div>
          </div>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Content className="app-content">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/tasks" element={<TaskList />} />
            <Route path="/tasks/:id/runs/:runId" element={<RunDetail />} />
            <Route path="/tasks/:id" element={<TaskDetail />} />
            <Route path="/pipelines" element={<PipelineList />} />
            <Route path="/pipelines/:id" element={<PipelineEditor />} />
            <Route path="/task-defs/new" element={<TaskEditor />} />
            <Route path="/task-defs/:id/edit" element={<TaskEditor />} />
            <Route path="/settings" element={<Settings />} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  )
}
