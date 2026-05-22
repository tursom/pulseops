import { Routes, Route, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu } from 'antd'
import { DashboardOutlined, UnorderedListOutlined, ApartmentOutlined } from '@ant-design/icons'
import Dashboard from './pages/Dashboard'
import TaskDetail from './pages/TaskDetail'
import TaskEditor from './pages/TaskEditor'
import PipelineList from './pages/PipelineList'
import PipelineEditor from './pages/PipelineEditor'
import './App.css'

const { Sider, Content } = Layout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/tasks', icon: <UnorderedListOutlined />, label: '任务列表' },
  { key: '/pipelines', icon: <ApartmentOutlined />, label: '流水线' },
]

export default function App() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = '/' + location.pathname.split('/')[1]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={220}>
        <div
          style={{
            height: 64,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontSize: 18,
            fontWeight: 700,
            letterSpacing: 1,
          }}
        >
          PulseOps
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
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/tasks" element={<Dashboard />} />
            <Route path="/tasks/:id" element={<TaskDetail />} />
            <Route path="/pipelines" element={<PipelineList />} />
            <Route path="/pipelines/:id" element={<PipelineEditor />} />
            <Route path="/task-defs/new" element={<TaskEditor />} />
            <Route path="/task-defs/:id/edit" element={<TaskEditor />} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  )
}
