<template>
  <div class="page">
    <div class="toolbar card row">
      <b>任务中心</b>
      <span class="spacer" />
      <el-button type="primary" :icon="'Plus'" @click="openCreate">提交新任务</el-button>
    </div>

    <div class="card filter-row row" style="margin-top:12px">
      <el-radio-group v-model="filters.status" size="small" @change="reload(1)">
        <el-radio-button label="">全部</el-radio-button>
        <el-radio-button label="queued">排队</el-radio-button>
        <el-radio-button label="running">运行</el-radio-button>
        <el-radio-button label="success">成功</el-radio-button>
        <el-radio-button label="failed">失败</el-radio-button>
        <el-radio-button label="canceled">取消</el-radio-button>
      </el-radio-group>
      <span class="spacer" />
      <el-input v-model="filters.q" placeholder="按名称/命令搜索" clearable style="width:240px" size="small"
        @keyup.enter="reload(1)" @clear="reload(1)" />
      <el-button size="small" style="margin-left:8px" @click="reload(1)">查询</el-button>
    </div>

    <div class="card" style="margin-top:12px">
      <el-table :data="list" v-loading="loading" stripe style="width:100%">
        <el-table-column label="任务" min-width="220">
          <template #default="{ row }">
            <div class="row">
              <el-tooltip :content="row.command" placement="top">
                <span class="cmd-text mono">{{ row.command }}</span>
              </el-tooltip>
            </div>
            <div class="mini-metric">
              <b>{{ row.name }}</b>
              <template v-if="row.retry_of"> · 重试#{{ row.retry_count }}</template>
              · {{ fmtTime(row.created_at) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="130">
          <template #default="{ row }">
            <el-tooltip :content="statusTip(row)" placement="top">
              <el-tag :type="statusMeta[row.status]?.tag" effect="light" size="small">
                {{ statusMeta[row.status]?.label || row.status }}
              </el-tag>
            </el-tooltip>
            <div v-if="row.exit_code !== null && (row.status==='success'||row.status==='failed')" class="mini-metric">
              exit={{ row.exit_code }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="节点" width="120">
          <template #default="{ row }">{{ row.node_name || '-' }}</template>
        </el-table-column>
        <el-table-column label="优先级" width="80">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" :type="row.priority >= 2 ? 'danger' : row.priority === 1 ? 'warning' : 'info'">
              {{ prioMeta[row.priority] || row.priority }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="资源/限制" width="200">
          <template #default="{ row }">
            <div class="mini-metric">
              <el-icon v-if="row.need_idle" style="vertical-align:-2px"><Timer /></el-icon>
              <span v-if="row.need_idle">仅空闲机 · </span>
              <span v-if="row.timeout_sec">超时 {{ fmtDur(row.timeout_sec * 1000) }}</span>
              <span v-if="row.max_mem_mb"> · 内存≤{{ row.max_mem_mb }}MB</span>
              <span v-if="row.max_cpu_pct"> · CPU≤{{ row.max_cpu_pct }}%</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="90">
          <template #default="{ row }">{{ fmtDur(row.duration_ms) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openLog(row)">日志</el-button>
            <el-button v-if="row.status==='queued'||row.status==='running'" link type="warning" size="small" @click="cancel(row)">取消</el-button>
            <el-button v-if="row.status==='failed'||row.status==='canceled'" link type="success" size="small" @click="retry(row)">重试</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="row" style="margin-top:14px">
        <span class="text-muted">共 {{ total }} 条</span>
        <span class="spacer" />
        <el-pagination background layout="prev, pager, next" :total="total" :page-size="filters.size"
          :current-page="filters.page" @current-change="reload" />
      </div>
    </div>

    <TaskLogDrawer v-model="drawer" :task="currentTask" @changed="reload(filters.page)" />

    <el-dialog v-model="createVisible" title="提交新任务" width="620px" top="6vh" append-to-body destroy-on-close>
      <el-form label-width="110px">
        <el-form-item label="任务名称">
          <el-input v-model="form.name" placeholder="留空则取命令前 40 字符" />
        </el-form-item>
        <el-form-item label="Shell 命令" required>
          <el-input v-model="form.command" type="textarea" :rows="4" placeholder="例如: go build ./...  或  python train.py --epochs 10" />
        </el-form-item>
        <el-form-item label="工作目录">
          <el-input v-model="form.cwd" placeholder="留空使用 Agent 默认目录" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item label="优先级">
              <el-select v-model="form.priority" style="width:100%">
                <el-option :value="0" label="低" />
                <el-option :value="1" label="普通" />
                <el-option :value="2" label="高" />
                <el-option :value="3" label="紧急" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="超时(秒)">
              <el-input-number v-model="form.timeout_sec" :min="10" :max="86400 * 7" controls-position="right" style="width:100%" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="Shell">
              <el-select v-model="form.shell" style="width:100%">
                <el-option value="auto" label="自动" />
                <el-option value="cmd" label="cmd (Windows)" />
                <el-option value="powershell" label="PowerShell" />
                <el-option value="bash" label="bash (Linux)" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="仅空闲机器执行">
          <el-switch v-model="form.need_idle" active-text="是" inactive-text="否" />
          <div class="mini-metric" style="margin-left:12px">机器被用户使用时自动避让/终止（人机共存）</div>
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item label="内存上限MB">
              <el-input-number v-model="form.max_mem_mb" :min="0" :max="1024 * 1024" controls-position="right" style="width:100%" />
              <div class="mini-metric">0=不限</div>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="CPU上限%">
              <el-input-number v-model="form.max_cpu_pct" :min="0" :max="cores_max" controls-position="right" style="width:100%" />
              <div class="mini-metric">0=不限（单核=100）</div>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="环境变量">
              <el-tooltip content="每行一个 K=V" placement="top">
                <el-input v-model="envText" type="textarea" :rows="2" placeholder="KEY=VALUE&#10;每行一个" />
              </el-tooltip>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">提交到队列</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'
import { statusMeta, reasonText, prioMeta, fmtTime, fmtDur, mb2str } from '../utils'
import TaskLogDrawer from './TaskLogDrawer.vue'

const route = useRoute()
const router = useRouter()
const list = ref([])
const total = ref(0)
const loading = ref(false)
const filters = reactive({ status: '', q: '', page: 1, size: 20 })
const cores_max = 512

const drawer = ref(false)
const currentTask = ref(null)

const createVisible = ref(false)
const submitting = ref(false)
const envText = ref('')
const form = reactive({
  name: '', command: '', cwd: '', priority: 1, timeout_sec: 3600,
  need_idle: true, max_mem_mb: 0, max_cpu_pct: 0, shell: 'auto'
})

async function reload(page) {
  if (page) filters.page = page
  loading.value = true
  try {
    const data = await api.tasks({ status: filters.status, q: filters.q, page: filters.page, size: filters.size })
    list.value = data.list
    total.value = data.total
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  envText.value = ''
  Object.assign(form, { name: '', command: '', cwd: '', priority: 1, timeout_sec: 3600, need_idle: true, max_mem_mb: 0, max_cpu_pct: 0, shell: 'auto' })
  createVisible.value = true
}
async function submit() {
  if (!form.command.trim()) { ElMessage.warning('请输入命令'); return }
  const env = {}
  envText.value.split('\n').map(s => s.trim()).filter(Boolean).forEach(line => {
    const i = line.indexOf('=')
    if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  })
  submitting.value = true
  try {
    await api.createTask({ ...form, env, need_idle: form.need_idle })
    ElMessage.success('已加入任务队列，等待调度')
    createVisible.value = false
    filters.status = ''
    reload(1)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    submitting.value = false
  }
}

function openLog(row) { currentTask.value = row; drawer.value = true }
async function cancel(row) {
  await ElMessageBox.confirm(`确认取消任务「${row.name}」？将终止其在节点上的执行。`, '取消任务', { type: 'warning' })
  try {
    await api.cancelTask(row.id)
    ElMessage.success('已发送取消指令')
    reload(filters.page)
  } catch (e) { ElMessage.error(e.message) }
}
async function retry(row) {
  try {
    const nt = await api.retryTask(row.id)
    ElMessage.success(`已创建重试任务 ${nt.id}`)
    reload(filters.page)
  } catch (e) { ElMessage.error(e.message) }
}
async function remove(row) {
  await ElMessageBox.confirm(`删除任务「${row.name}」及其日志？`, '删除任务', { type: 'warning' })
  try {
    await api.deleteTask(row.id)
    ElMessage.success('已删除')
    reload(filters.page)
  } catch (e) { ElMessage.error(e.message) }
}

function statusTip(row) {
  if (row.reason && reasonText[row.reason]) return reasonText[row.reason] + (row.error ? `：${row.error}` : '')
  if (row.error) return row.error
  return ''
}

onMounted(() => {
  reload(1)
  if (route.query.new) openCreate()
})
onUnmounted(() => {})
</script>

<style scoped>
.toolbar { padding: 12px 16px; }
.filter-row { padding: 10px 14px; flex-wrap: wrap; gap: 8px; }
</style>
