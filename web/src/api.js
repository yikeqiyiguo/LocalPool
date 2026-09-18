const base = '/api'
const KEY_STORAGE = 'lp_access_key'
let lastKeyPrompt = 0

// 访问密钥存取：服务器启用 -secret 后，浏览器需在首个请求前通过提示框输入一次
function getKey() {
  return localStorage.getItem(KEY_STORAGE) || ''
}
function setKey(k) {
  if (k) localStorage.setItem(KEY_STORAGE, k)
  else localStorage.removeItem(KEY_STORAGE)
}
// 401 时向用户索要密钥；节流避免轮询造成弹窗风暴
async function ensureKey() {
  const now = Date.now()
  if (now - lastKeyPrompt < 30000) return false
  lastKeyPrompt = now
  const input = window.prompt('调度中心已启用访问密钥，请输入密钥（服务器启动参数 -secret 配置的值）：', getKey())
  if (!input || !input.trim()) return false
  setKey(input.trim())
  return true
}

async function http(url, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) }
  const key = getKey()
  if (key) headers['X-API-Key'] = key
  const init = { ...options, headers }
  const res = await fetch(base + url, init)
  let json = null
  try {
    json = await res.json()
  } catch (e) { /* ignore */ }
  if (res.status === 401 && json && json.code === 401 && (await ensureKey())) {
    return http(url, options) // 输入/修正密钥后重试一次
  }
  if (!res.ok || !json || json.code !== 0) {
    const msg = (json && json.msg) || `HTTP ${res.status}`
    throw new Error(msg)
  }
  return json.data
}

export const api = {
  overview: () => http('/overview'),
  nodes: () => http('/nodes'),
  node: (id) => http('/nodes/' + id),

  tasks: (params = {}) => {
    const qs = new URLSearchParams()
    Object.entries(params).forEach(([k, v]) => { if (v !== '' && v !== undefined && v !== null) qs.set(k, v) })
    return http('/tasks?' + qs.toString())
  },
  task: (id) => http('/tasks/' + id),
  createTask: (body) => http('/tasks', { method: 'POST', body: JSON.stringify(body) }),
  cancelTask: (id, reason = '') => http(`/tasks/${id}/cancel`, { method: 'POST', body: JSON.stringify({ reason }) }),
  retryTask: (id) => http(`/tasks/${id}/retry`, { method: 'POST' }),
  deleteTask: (id) => http('/tasks/' + id, { method: 'DELETE' }),
  taskLogs: (id, params = {}) => {
    const qs = new URLSearchParams(params)
    return http(`/tasks/${id}/logs?` + qs.toString())
  },
  timeline: (hours = 24) => http('/stats/timeline?hours=' + hours),
  config: () => http('/config'),
  saveConfig: (cfg) => http('/config', { method: 'PUT', body: JSON.stringify(cfg) })
}

// SSE 实时流工具：服务端 task 日志
export function openLogStream(taskId, after, handlers) {
  const ctrl = new AbortController()
  let buf = ''
  let running = true
  const parser = (chunk) => {
    buf += chunk
    const parts = buf.split('\n\n')
    buf = parts.pop() || ''
    for (const part of parts) {
      for (const line of part.split('\n')) {
        if (line.startsWith('event: ')) {
          const ev = line.slice(7).trim()
          if (ev === 'done') handlers.onDone && handlers.onDone()
        } else if (line.startsWith('data: ')) {
          try {
            handlers.onLine && handlers.onLine(JSON.parse(line.slice(6)))
          } catch (e) { /* ignore */ }
        }
      }
    }
  }
  const start = async () => {
    try {
      const headers = {}
      const key = getKey()
      if (key) headers['X-API-Key'] = key
      const res = await fetch(`${base}/tasks/${taskId}/logs/stream?after=${after || 0}`, { signal: ctrl.signal, headers })
      if (res.status === 401) { handlers.onError && handlers.onError(new Error('访问密钥缺失或无效')); return }
      if (!res.ok || !res.body) { handlers.onError && handlers.onError(new Error('stream unavailable')); return }
      const reader = res.body.getReader()
      const dec = new TextDecoder()
      while (running) {
        const { done, value } = await reader.read()
        if (done) break
        parser(dec.decode(value, { stream: true }))
      }
    } catch (e) {
      if (e.name !== 'AbortError') handlers.onError && handlers.onError(e)
    } finally {
      handlers.onClose && handlers.onClose()
    }
  }
  start()
  return () => { running = false; ctrl.abort() }
}
