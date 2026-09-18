import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', name: 'dashboard', component: () => import('./views/Dashboard.vue'), meta: { title: '算力总览' } },
  { path: '/nodes', name: 'nodes', component: () => import('./views/Nodes.vue'), meta: { title: '节点管理' } },
  { path: '/tasks', name: 'tasks', component: () => import('./views/Tasks.vue'), meta: { title: '任务中心' } },
  { path: '/stats', name: 'stats', component: () => import('./views/Stats.vue'), meta: { title: '历史统计' } },
  { path: '/settings', name: 'settings', component: () => import('./views/Settings.vue'), meta: { title: '调度设置' } }
]

export default createRouter({
  history: createWebHistory(),
  routes
})
