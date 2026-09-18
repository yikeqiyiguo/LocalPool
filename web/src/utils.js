export function fmtTime(ms) {
  if (!ms) return '-'
  const d = new Date(ms)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function fmtAgo(ms) {
  if (!ms || ms <= 0) return '-'
  const s = Math.floor(ms / 1000)
  if (s < 60) return s + ' 秒前'
  const m = Math.floor(s / 60)
  if (m < 60) return m + ' 分钟前'
  const h = Math.floor(m / 60)
  if (h < 24) return h + ' 小时前'
  return Math.floor(h / 24) + ' 天前'
}

export function fmtDur(ms) {
  if (!ms || ms < 0) return '-'
  const s = Math.floor(ms / 1000)
  if (s < 60) return s + 's'
  const m = Math.floor(s / 60)
  if (m < 60) return m + 'm ' + (s % 60) + 's'
  const h = Math.floor(m / 60)
  return h + 'h ' + (m % 60) + 'm'
}

export function mb2str(mb) {
  if (mb == null || mb === 0) return '-'
  if (mb >= 1024) return (mb / 1024).toFixed(2) + ' GB'
  return Math.round(mb) + ' MB'
}

export function num2str(n) {
  if (n == null || n === 0) return '0'
  if (n >= 1000) return (n / 1000).toFixed(2) + 'k'
  return String(Math.round(n))
}

export const statusMeta = {
  queued: { label: '排队中', tag: 'warning' },
  running: { label: '运行中', tag: 'primary' },
  success: { label: '成功', tag: 'success' },
  failed: { label: '失败', tag: 'danger' },
  canceled: { label: '已取消', tag: 'info' }
}

export const reasonText = {
  user_cancel: '用户取消',
  timeout: '执行超时',
  node_lost: '节点离线',
  human_evict: '人机避让(机器被占用)',
  mem_limit: '内存超限',
  agent_quit: 'Agent 退出',
  '': ''
}

export const prioMeta = { 0: '低', 1: '普通', 2: '高', 3: '紧急' }
