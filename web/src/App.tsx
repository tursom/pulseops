import { Routes, Route, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu } from 'antd'
import { DashboardOutlined, UnorderedListOutlined, ApartmentOutlined } from '@ant-design/icons'
import Dashboard from './pages/Dashboard'
import TaskDetail from './pages/TaskDetail'
import './App.css'

const { Sider, Content } = Layout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: 'Dashboard' },
  { key: '/tasks', icon: <UnorderedListOutlined />, label: 'Tasks' },
  { key: '/pipeline', icon: <ApartmentOutlined />, label: 'Pipeline' },
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
            <Route path="/pipeline" element={<div style={{ padding: 24 }}><h2>Pipeline Editor</h2><p>Coming soon...</p></div>} />
            <Route path="/task-defs/new" element={<div style={{ padding: 24 }}><h2>Create Task</h2><p>Coming soon...</p></div>} />
            <Route path="/task-defs/:id/edit" element={<div style={{ padding: 24 }}><h2>Edit Task</h2><p>Coming soon...</p></div>} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  )
}
