<template>
  <el-drawer v-model="show" :size="'62%'" :title="'任务日志'" destroy-on-close>
    <template #default>
      <div class="meta" v-if="detail">
        <div class="row wrap">
          <el-tag :type="statusMeta[detail.status]?.tag" effect="light" size="small">{{ statusMeta[detail.status]?.label }}</el-tag>
          <b style="margin-left:8px">{{ detail.name }}</b>
          <span class="mono cmd" :title="detail.command">{{ detail.command }}</span>
        </div>
        <div class="meta-lines">
          <span>节点：{{ detail.node_name || '-' }}</span>
          <span>提交：{{ fmtTime(detail.created_at) }}</span>
          <span>开始：{{ fmtTime(detail.started_at) }}</span>
          <span>耗时：{{ fmtDur(detail.duration_ms) }}</span>
          <span>内存峰值：{{ mb(detail.peak_mem_mb) }}</span>
          <span v-if="detail.error" class="err-text" :title="detail.error">错误：{{ detail.error }}</span>
        </div>
      </div>

      <div class="toolbar row">
        <el-checkbox v-model="autoScroll">自动滚动</el-checkbox>
        <el-tag size="small" effect="plain">{{ total }} 行</el-tag>
        <span class="spacer" />
        <el-button size="small" @click="reloadLogs">刷新</el-button>
        <el-button v-if="detail && (detail.status==='queued'||detail.status==='running')" size="small" type="warning" @click="cancel">取消任务</el-button>
        <el-button v-if="detail && (detail.status==='failed'||detail.status==='canceled')" size="small" type="success" @click="retry">重试</el-button>
        <el-tag v-if="live" type="success" effect="light" size="small">● LIVE</el-tag>
      </div>

      <div ref="termEl" class="terminal">
        <template v-if="!lines.length && loading"><div class="term-hint">加载中…</div></template>
        <template v-else-if="!lines.length"><div class="term-hint">暂无日志输出</div></template>
        <div v-for="l in lines" :key="l.id" class="log-line" :class="l.s">
          <span class="lt">{{ ltime(l.t) }}</span>
          <span v-if="l.s==='err'" class="mark err">ERR</span>
          <span v-else-if="l.s==='warn'" class="mark warn">WRN</span>
          <pre class="lp">{{ l.l }}</pre>
        </div>
        <div v-if="done && detail && isTerm(detail.status)" class="term-end">
          — 任务已{{ statusMeta[detail.status]?.label }}（exit={{ detail.exit_code }}）—
        </div>
      </div>
    </template>
  </el-drawer>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, openLogStream } from '../api'
import { statusMeta, fmtTime, fmtDur, mb2str as mb } from '../utils'

const props = defineProps({ modelValue: Boolean, task: Object })
const emit = defineEmits(['update:modelValue', 'changed'])

const show = ref(props.modelValue)
const task = ref(props.task)
const detail = ref(null)
const lines = ref([])
const total = ref(0)
const loading = ref(false)
const autoScroll = ref(true)
const live = ref(false)
const done = ref(false)
const termEl = ref(null)
let stopStream = null

watch(() => props.modelValue, (v) => { show.value = v })
watch(() => props.task, (v) => { task.value = v; if (v) open() })

watch(show, async (v) => {
  if (v) {
    task.value = props.task
    await open()
  } else {
    if (stopStream) stopStream()
    stopStream = null
  }
})

async function open() {
  if (!task.value) return
  lines.value = []
  total.value = 0
  live.value = false
  done.value = false
  if (stopStream) stopStream()
  loading.value = true
  try {
    const t = await api.task(task.value.id)
    detail.value = { ...task.value, ...t.task }
    total.value = t.log_lines
    const logs = await api.taskLogs(task.value.id, { tail: 1500 })
    lines.value = logs.lines
    total.value = logs.total
    done.value = isTerm(detail.value.status)
    startStream(logs.last_id)
  } catch (e) {
    ElMessage.error('加载任务详情失败: ' + e.message)
  } finally {
    loading.value = false
  }
}

function startStream(after) {
  stopStream = openLogStream(task.value.id, after, {
    onLine: (ll) => {
      lines.value.push(ll)
      total.value++
      if (lines.value.length > 6000) lines.value.splice(0, lines.value.length - 6000)
      scroll()
    },
    onDone: () => { done.value = true; live.value = false },
    onError: () => { live.value = false },
    onClose: () => { live.value = false }
  })
  live.value = true
}

async function reloadLogs() {
  if (!task.value) return
  const logs = await api.taskLogs(task.value.id, { tail: 2000 })
  lines.value = logs.lines
  total.value = logs.total
  scroll(true)
}

async function scroll(force = false) {
  if (!autoScroll.value && !force) return
  await nextTick()
  if (termEl.value) termEl.value.scrollTop = termEl.value.scrollHeight
}

async function cancel() {
  await ElMessageBox.confirm('确认取消该任务？将终止其执行进程。', '取消任务', { type: 'warning' })
  try {
    await api.cancelTask(task.value.id)
    ElMessage.success('已发送取消指令')
    emit('changed')
    open()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || String(e))
  }
}
function retry() {
  api.retryTask(task.value.id).then(() => {
    ElMessage.success('已创建重试任务')
    emit('changed')
  }).catch(e => ElMessage.error(e.message))
}

function isTerm(s) { return s === 'success' || s === 'failed' || s === 'canceled' }
function ltime(ms) { const d = new Date(ms); return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}` }
</script>

<style scoped>
.meta { margin-bottom: 8px; }
.meta .cmd { margin-left: 10px; color: #5b6b7f; max-width: 60%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.meta-lines { display: flex; flex-wrap: wrap; gap: 6px 18px; color: #7a8699; font-size: 12px; margin-top: 6px; }
.err-text { color: #e5484d; max-width: 60%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.toolbar { margin: 10px 0; gap: 10px; }
.terminal {
  background: #0d1117; color: #d5dce6; border-radius: 8px; padding: 10px 12px;
  height: calc(100vh - 250px); overflow-y: auto; font-family: Consolas, Menlo, 'Courier New', monospace; font-size: 12.5px; line-height: 1.55;
}
.log-line { display: flex; gap: 8px; white-space: pre-wrap; word-break: break-all; }
.log-line.err .lp { color: #ff8b8b; }
.log-line.warn .lp { color: #ffd479; }
.lt { color: #58616f; flex-shrink: 0; }
.mark { font-size: 10px; font-weight: 700; flex-shrink: 0; }
.mark.err { color: #ff6b6b; }
.mark.warn { color: #ffc233; }
.lp { margin: 0; white-space: pre-wrap; word-break: break-all; font-family: inherit; }
.term-hint, .term-end { color: #6b7280; padding: 12px; text-align: center; }
.term-end { color: #58a6ff; }
</style>
