<template>
  <div class="page">
    <div class="toolbar card row">
      <b>历史任务统计</b>
      <span class="spacer" />
      <el-radio-group v-model="hours" size="small" @change="load">
        <el-radio-button :label="6">6 小时</el-radio-button>
        <el-radio-button :label="24">24 小时</el-radio-button>
        <el-radio-button :label="72">3 天</el-radio-button>
        <el-radio-button :label="168">7 天</el-radio-button>
      </el-radio-group>
    </div>

    <el-row :gutter="16" style="margin-top:16px">
      <el-col :span="6">
        <div class="card kpi">
          <div class="m-label">任务总数</div>
          <div class="m-value">{{ totals.all }}</div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="card kpi">
          <div class="m-label">成功</div>
          <div class="m-value green">{{ totals.success }}</div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="card kpi">
          <div class="m-label">失败 / 取消</div>
          <div class="m-value red">{{ totals.failed + totals.canceled }}</div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="card kpi">
          <div class="m-label">成功率</div>
          <div class="m-value blue">{{ successRate }}</div>
        </div>
      </el-col>
    </el-row>

    <div class="card" style="margin-top:16px">
      <div class="card-head"><b>任务结果分布（按小时）</b></div>
      <el-empty v-if="!buckets.length" description="该时间段内暂无任务" :image-size="90" />
      <div v-else class="chart">
        <div v-for="b in buckets" :key="b.hour" class="col">
          <div class="bar-wrap">
            <div class="bar success" :style="{ height: h(b.success, 'success') + 'px' }" :title="`成功 ${b.success}`" />
            <div class="bar running" :style="{ height: h(b.running, 'running') + 'px' }" :title="`运行 ${b.running}`" />
            <div class="bar failed" :style="{ height: h(b.failed, 'failed') + 'px' }" :title="`失败 ${b.failed}`" />
          </div>
          <div class="xlabel">{{ hourLabel(b.hour) }}</div>
        </div>
      </div>
      <div class="legend">
        <span><i class="dot s" />成功</span>
        <span><i class="dot r" />运行中</span>
        <span><i class="dot f" />失败</span>
      </div>
    </div>

    <div class="card" style="margin-top:16px">
      <div class="card-head"><b>最近任务明细</b></div>
      <el-table :data="recent" stripe size="small">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="任务" min-width="200">
          <template #default="{ row }">
            <span class="cmd-text mono">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="节点" width="130" prop="node_name" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusMeta[row.status]?.tag" size="small" effect="light">{{ statusMeta[row.status]?.label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ fmtDur(row.duration_ms) }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { statusMeta, fmtTime, fmtDur } from '../utils'

const hours = ref(24)
const buckets = ref([])
const recent = ref([])

const max = { success: 1, running: 1, failed: 1 }

async function load() {
  buckets.value = await api.timeline(hours.value)
  computeMax()
  const t = await api.tasks({ page: 1, size: 12 })
  recent.value = t.list
}
function computeMax() {
  max.success = 1; max.running = 1; max.failed = 1
  for (const b of buckets.value) {
    max.success = Math.max(max.success, b.success)
    max.running = Math.max(max.running, b.running)
    max.failed = Math.max(max.failed, b.failed)
  }
}
function h(v, k) { return Math.max(2, Math.round(v / max[k] * 140)) }
function hourLabel(h) {
  const [d, t] = String(h).split(' ')
  return `${d ? d.slice(5) : ''}\n${t ? t.slice(0, 2) + '时' : ''}`
}
const totals = computed(() => buckets.value.reduce((acc, b) => {
  acc.all += b.success + b.failed + b.running
  acc.success += b.success
  acc.failed += b.failed
  acc.running += b.running
  return acc
}, { all: 0, success: 0, failed: 0, running: 0, canceled: 0 }))
const successRate = computed(() => {
  const done = totals.value.success + totals.value.failed
  if (!done) return '-'
  return (totals.value.success / done * 100).toFixed(1) + '%'
})

onMounted(load)
</script>

<style scoped>
.toolbar { padding: 12px 16px; }
.kpi { min-height: 92px; }
.green { color: #00a36c; } .red { color: #e5484d; } .blue { color: #3b6bff; }
.chart { display: flex; align-items: flex-end; gap: 6px; height: 180px; overflow-x: auto; padding: 8px 2px 0; }
.col { flex: 1 0 34px; display: flex; flex-direction: column; align-items: center; height: 100%; }
.bar-wrap { display: flex; align-items: flex-end; gap: 2px; height: 150px; }
.bar { width: 8px; border-radius: 2px 2px 0 0; transition: height .5s; }
.bar.success { background: #00c98d; }
.bar.running { background: #3b6bff; }
.bar.failed { background: #ff6b6b; }
.xlabel { font-size: 10px; color: #98a2b3; margin-top: 4px; white-space: pre; }
.legend { display: flex; gap: 18px; margin-top: 10px; color: #5b6b7f; font-size: 12px; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 4px; }
.dot.s { background: #00c98d; } .dot.r { background: #3b6bff; } .dot.f { background: #ff6b6b; }
.card-head { margin-bottom: 12px; }
</style>
