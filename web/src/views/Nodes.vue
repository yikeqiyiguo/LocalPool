<template>
  <div class="page">
    <div class="toolbar card row">
      <b>节点管理</b>
      <span class="text-muted" style="margin-left:10px">Agent 安装后自动入网并上报资源</span>
      <span class="spacer" />
      <el-button :icon="'Refresh'" circle size="small" @click="refresh" />
    </div>

    <div class="card" style="margin-top:14px">
      <el-table :data="nodes" v-loading="loading" stripe style="width:100%">
        <el-table-column label="节点" min-width="210">
          <template #default="{ row }">
            <div class="row">
              <span class="status-dot" :class="state(row)"></span>
              <b style="margin-left:6px">{{ row.name }}</b>
              <el-tag v-if="row.human_active" type="warning" size="small" style="margin-left:8px">被使用</el-tag>
              <el-tag v-else-if="row.online" type="success" size="small">空闲可调度</el-tag>
              <el-tag v-else type="info" size="small">离线</el-tag>
            </div>
            <div class="mini-metric">{{ row.host }} · {{ row.os }} {{ row.arch }} · Agent {{ row.agent_version }}</div>
          </template>
        </el-table-column>
        <el-table-column label="CPU" width="170">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.cpu_pct)" :stroke-width="8" :color="barColor(row.cpu_pct)" />
            <div class="mini-metric">{{ row.cpu_pct.toFixed(1) }}% · {{ row.cores }} 核</div>
          </template>
        </el-table-column>
        <el-table-column label="内存" width="170">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.mem_used_mb, row.mem_total_mb)" :stroke-width="8" color="#00b894" />
            <div class="mini-metric">{{ mb(row.mem_used_mb) }} / {{ mb(row.mem_total_mb) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="磁盘" width="150">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.disk_used_mb, row.disk_total_mb)" :stroke-width="8" color="#f59e0b" />
            <div class="mini-metric">{{ mb(row.disk_used_mb) }} / {{ mb(row.disk_total_mb) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="运行任务" width="90">
          <template #default="{ row }">
            <el-tag :type="row.running_tasks ? 'primary' : 'info'" effect="light">{{ row.running_tasks }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="空闲" width="90">
          <template #default="{ row }">
            {{ idleTxt(row) }}
          </template>
        </el-table-column>
        <el-table-column label="最后心跳" width="130">
          <template #default="{ row }">
            <span :style="row.online ? '' : 'color:#e5484d'">{{ fmtAgo(row.last_seen_ago_ms) }}</span>
          </template>
        </el-table-column>
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="detail-grid">
              <div><span class="text-muted">节点ID</span><div class="mono">{{ row.id }}</div></div>
              <div><span class="text-muted">CPU 型号</span><div>{{ row.cpu_model }}</div></div>
              <div><span class="text-muted">网络速率(出/入)</span><div>{{ rate(row.net_out_bps) }} ↑ / {{ rate(row.net_in_bps) }} ↓</div></div>
              <div><span class="text-muted">负载(Load1)</span><div>{{ row.load1 || '-' }}</div></div>
              <div><span class="text-muted">开机时长</span><div>{{ uptime(row.uptime_sec) }}</div></div>
              <div><span class="text-muted">人机状态</span>
                <div>{{ row.human_active ? (row.human_reason || '正在使用') : '空闲' }}（空闲 {{ Math.round(row.idle_sec) }}s）</div>
              </div>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { mb2str as mb, fmtAgo } from '../utils'

const nodes = ref([])
const loading = ref(false)
let timer = null

async function refresh() {
  try {
    nodes.value = await api.nodes()
  } catch (e) { /* ignore */ } finally {
    loading.value = false
  }
}
function state(row) { return row.online ? (row.human_active ? 'busy' : 'online') : 'offline' }
function pct(a, b) {
  if (b == null) return Math.min(100, +a.toFixed(0))
  if (!b) return 0
  return Math.min(100, Math.round(a / b * 100))
}
function barColor(v) { return v > 85 ? '#e5484d' : v > 60 ? '#f59e0b' : '#3b6bff' }
function idleTxt(row) {
  if (!row.online) return '-'
  if (row.human_active) return '使用中'
  return row.idle_sec >= 0 ? `已空闲 ${fmtHuman(row.idle_sec)}` : '空闲'
}
function fmtHuman(s) {
  if (s >= 3600) return (s / 3600).toFixed(1) + 'h'
  if (s >= 60) return Math.round(s / 60) + 'm'
  return Math.round(s) + 's'
}
function rate(bps) { return bps ? (bps / 1024 / 1024).toFixed(1) + ' MB/s' : '-' }
function uptime(sec) {
  if (!sec) return '-'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  return d ? `${d}天${h}时` : h ? `${h}时` : `${Math.floor(sec / 60)}分`
}

onMounted(() => { refresh(); timer = setInterval(refresh, 3000) })
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.toolbar { padding: 12px 16px; }
.status-dot { width: 9px; height: 9px; border-radius: 50%; flex-shrink: 0; }
.status-dot.online { background: #2ecc71; }
.status-dot.busy { background: #f59e0b; }
.status-dot.offline { background: #c0c8d8; }
.detail-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 10px 24px; padding: 8px 40px; }
</style>
