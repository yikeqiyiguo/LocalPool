<template>
  <div class="page">
    <el-row :gutter="16">
      <el-col :span="5">
        <div class="card metric">
          <div class="m-label">在线节点</div>
          <div class="m-value">{{ ov.nodes_online }}<span class="m-unit">/{{ ov.nodes_total }}</span></div>
          <el-progress :percentage="onlinePct" :show-text="false" :stroke-width="6" color="#3b6bff" />
        </div>
      </el-col>
      <el-col :span="5">
        <div class="card metric">
          <div class="m-label">可调度 CPU 核</div>
          <div class="m-value">{{ ov.cores_online }}<span class="m-unit"> cores</span></div>
          <div class="mini-metric">全网 CPU 平均 {{ ov.cpu_avg }}%</div>
        </div>
      </el-col>
      <el-col :span="5">
        <div class="card metric">
          <div class="m-label">内存使用</div>
          <div class="m-value">{{ mb(ov.mem_used_mb) }}<span class="m-unit"> / {{ mb(ov.mem_total_mb) }}</span></div>
          <el-progress :percentage="memPct" :show-text="false" :stroke-width="6" color="#6a3bff" />
        </div>
      </el-col>
      <el-col :span="5">
        <div class="card metric">
          <div class="m-label">正在运行 / 排队</div>
          <div class="m-value"><span class="run">{{ ov.tasks.running }}</span><span class="m-unit"> / {{ ov.tasks.queued }}</span></div>
          <div class="mini-metric">今日成功 {{ ov.today.success }} · 失败 {{ ov.today.failed }}</div>
        </div>
      </el-col>
      <el-col :span="4">
        <div class="card metric total">
          <div class="m-label">全部任务</div>
          <div class="m-value">{{ totalTasks }}</div>
          <el-button type="primary" size="small" class="go-btn" @click="$router.push('/tasks?new=1')">提交新任务</el-button>
        </div>
      </el-col>
    </el-row>

    <el-alert v-if="ov.human_active && ov.human_active.length" type="warning" :closable="false" class="warn-bar" show-icon>
      <template #title>
        检测到 {{ ov.human_active.length }} 台机器正在被使用：{{ ov.human_active.map(h => h.name).join('、') }} —— 运行中的闲置任务将按“人机共存”策略暂停/终止
      </template>
    </el-alert>

    <el-row :gutter="16" class="mt16">
      <el-col :span="14">
        <div class="card">
          <div class="card-head"><b>节点运行分布</b><span class="spacer" /><el-button link type="primary" @click="$router.push('/nodes')">查看全部节点</el-button></div>
          <el-empty v-if="!nodes.length" description="暂无在线节点，请在其它机器启动 Agent" :image-size="90" />
          <div v-for="row in nodeRows" :key="row.id" class="node-row">
            <div class="node-info">
              <div class="row">
                <el-icon :color="row.human_active ? '#f59e0b' : row.online ? '#2ecc71' : '#c0c8d8'">
                  <Monitor />
                </el-icon>
                <b style="margin-left:6px">{{ row.name }}</b>
                <el-tag v-if="row.human_active" type="warning" size="small" effect="light" style="margin-left:8px">人机共存中</el-tag>
                <el-tag v-else-if="row.online" type="success" size="small" effect="light" style="margin-left:8px">在线</el-tag>
                <el-tag v-else type="info" size="small" effect="light" style="margin-left:8px">离线</el-tag>
              </div>
              <div class="mini-metric">{{ row.os }}/{{ row.arch }} · {{ row.cpu_model }}</div>
            </div>
            <div class="node-meter">
              <div class="bar"><div class="fill cpu" :style="{ width: row.cpu_pct + '%' }" /></div>
              <div class="bar"><div class="fill mem" :style="{ width: memSinglePct(row) + '%' }" /></div>
              <div class="mini-metric">{{ row.cpu_pct.toFixed(1) }}% CPU · {{ mb(row.mem_used_mb) }}/{{ mb(row.mem_total_mb) }} · {{ row.running_tasks }} 任务</div>
            </div>
          </div>
        </div>
      </el-col>

      <el-col :span="10">
        <div class="card">
          <div class="card-head"><b>任务速览</b><span class="spacer" /><el-button link type="primary" @click="$router.push('/tasks')">任务中心</el-button></div>
          <el-row :gutter="10">
            <el-col v-for="(meta, key) in statusCards" :key="key" :span="8">
              <div class="task-stat" :class="key">
                <div class="ts-num">{{ ov.tasks[key] }}</div>
                <div class="ts-label">{{ meta }}</div>
              </div>
            </el-col>
          </el-row>
          <el-divider />
          <div class="mini-head"><b>运行中任务</b></div>
          <el-empty v-if="!ov.running_by_node || !ov.running_by_node.length" description="当前没有运行中的任务" :image-size="70" />
          <div v-for="r in ov.running_by_node" :key="r.node_id" class="run-row">
            <span>{{ r.node_name || r.node_id }}</span>
            <span class="spacer" />
            <el-tag type="primary" effect="light" size="small">{{ r.running }} 个任务</el-tag>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { mb2str as mb } from '../utils'

const ov = ref(emptyOverview())
const nodes = ref([])
let timer = null

function emptyOverview() {
  return {
    nodes_total: 0, nodes_online: 0, cores_online: 0, cpu_avg: 0,
    mem_total_mb: 0, mem_used_mb: 0, human_active: [], running_by_node: [],
    tasks: { queued: 0, running: 0, success: 0, failed: 0, canceled: 0 },
    today: { success: 0, failed: 0, canceled: 0 }
  }
}

async function refresh() {
  try {
    const o = await api.overview()
    if (o) ov.value = o
    nodes.value = await api.nodes()
  } catch (e) { /* 连接异常在顶栏提示 */ }
}

const statusCards = {
  queued: '排队中', running: '运行中', success: '成功',
  failed: '失败', canceled: '已取消'
}
const totalTasks = computed(() =>
  ov.value.tasks.queued + ov.value.tasks.running + ov.value.tasks.success + ov.value.tasks.failed + ov.value.tasks.canceled)
const onlinePct = computed(() => {
  if (!ov.value.nodes_total) return 0
  return Math.round(ov.value.nodes_online / ov.value.nodes_total * 100)
})
const memPct = computed(() => {
  if (!ov.value.mem_total_mb) return 0
  return Math.min(100, Math.round(ov.value.mem_used_mb / ov.value.mem_total_mb * 100))
})
const nodeRows = computed(() => nodes.value.slice(0, 12))
function memSinglePct(n) { return n.mem_total_mb ? Math.min(100, +(n.mem_used_mb / n.mem_total_mb * 100).toFixed(1)) : 0 }

onMounted(() => { refresh(); timer = setInterval(refresh, 3000) })
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.mt16 { margin-top: 16px; }
.metric { min-height: 112px; }
.metric.total { display: flex; flex-direction: column; justify-content: center; }
.m-label { color: #7a8699; font-size: 12px; }
.m-value { font-size: 30px; font-weight: 700; margin: 4px 0 8px; }
.m-unit { font-size: 13px; font-weight: 400; color: #98a2b3; margin-left: 2px; }
.m-value .run { color: var(--lp-primary); }
.go-btn { margin-top: 2px; }
.warn-bar { margin-top: 16px; }
.card-head { display: flex; align-items: center; font-size: 14px; margin-bottom: 12px; }
.node-row { display: flex; align-items: center; gap: 16px; padding: 8px 0; border-bottom: 1px dashed #eef1f6; }
.node-info { width: 240px; flex-shrink: 0; }
.node-meter { flex: 1; }
.bar { height: 6px; border-radius: 3px; background: #eef1f6; margin: 3px 0; overflow: hidden; }
.fill { height: 100%; border-radius: 3px; transition: width .6s; }
.fill.cpu { background: linear-gradient(90deg, #3b6bff, #6a3bff); }
.fill.mem { background: linear-gradient(90deg, #00b894, #55efc4); }
.task-stat { border-radius: 8px; padding: 10px 6px; text-align: center; background: #f6f8fc; }
.task-stat.running { background: #eef3ff; }
.task-stat.success { background: #eafaf3; }
.task-stat.failed { background: #fef0ef; }
.ts-num { font-size: 22px; font-weight: 700; }
.task-stat.queued .ts-num { color: #b8860b; }
.task-stat.running .ts-num { color: #3b6bff; }
.task-stat.success .ts-num { color: #00a36c; }
.task-stat.failed .ts-num { color: #e5484d; }
.task-stat.canceled .ts-num { color: #98a2b3; }
.ts-label { font-size: 11px; color: #7a8699; }
.mini-head { font-size: 12px; margin-bottom: 8px; }
.run-row { display: flex; align-items: center; padding: 6px 0; border-bottom: 1px dashed #f0f2f6; font-size: 13px; }
</style>
