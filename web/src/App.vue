<template>
  <el-container class="layout">
    <el-aside width="212px" class="sidebar">
      <div class="brand">
        <div class="logo"><el-icon><Cpu /></el-icon></div>
        <div>
          <div class="brand-name">LocalPool</div>
          <div class="brand-sub">闲置算力编排平台</div>
        </div>
      </div>
      <el-menu
        :default-active="$route.path"
        class="menu"
        router
        background-color="transparent"
        text-color="#aab3c9"
        active-text-color="#ffffff"
      >
        <el-menu-item index="/dashboard"><el-icon><Odometer /></el-icon><span>算力总览</span></el-menu-item>
        <el-menu-item index="/nodes"><el-icon><Monitor /></el-icon><span>节点管理</span></el-menu-item>
        <el-menu-item index="/tasks"><el-icon><List /></el-icon><span>任务中心</span></el-menu-item>
        <el-menu-item index="/stats"><el-icon><TrendCharts /></el-icon><span>历史统计</span></el-menu-item>
        <el-menu-item index="/settings"><el-icon><Setting /></el-icon><span>调度设置</span></el-menu-item>
      </el-menu>
      <div class="foot">
        <div class="dot" :class="{ on: overview && overview.nodes_online > 0 }" />
        <span>在线节点 {{ overview ? overview.nodes_online : 0 }}/{{ overview ? overview.nodes_total : 0 }}</span>
      </div>
    </el-aside>

    <el-container class="right">
      <el-header class="topbar" height="54px">
        <div class="topbar-title">{{ $route.meta.title || 'LocalPool' }}</div>
        <div class="spacer" />
        <el-tag v-if="alive" type="success" effect="light" size="small">调度中心在线</el-tag>
        <el-tag v-else type="danger" effect="light" size="small">连接断开</el-tag>
      </el-header>
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from './api'

const overview = ref(null)
const alive = ref(true)
let timer = null

async function refresh() {
  try {
    overview.value = await api.overview()
    alive.value = true
  } catch (e) {
    alive.value = false
  }
}
onMounted(() => {
  refresh()
  timer = setInterval(refresh, 4000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.layout { height: 100vh; }
.sidebar {
  background: var(--lp-sidebar);
  display: flex; flex-direction: column; padding: 18px 10px 12px;
}
.brand { display: flex; gap: 10px; align-items: center; padding: 2px 8px 16px; color: #fff; }
.logo {
  width: 38px; height: 38px; border-radius: 10px; display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, #3b6bff, #6a3bff); font-size: 20px; color: #fff;
}
.brand-name { font-weight: 700; font-size: 17px; letter-spacing: 0.5px; }
.brand-sub { font-size: 10px; color: #7d8aa8; }
.menu { border-right: none; flex: 1; }
.menu :deep(.el-menu-item) { height: 44px; border-radius: 8px; margin: 2px 0; }
.menu :deep(.el-menu-item.is-active) { background: var(--lp-primary); }
.foot { color: #7d8aa8; font-size: 11px; display: flex; align-items: center; gap: 6px; padding: 8px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #e5484d; }
.dot.on { background: #2ecc71; box-shadow: 0 0 6px #2ecc71; }
.right { min-width: 0; }
.topbar {
  background: #fff; display: flex; align-items: center; gap: 12px;
  border-bottom: 1px solid #eef1f6; box-shadow: 0 1px 3px rgba(20,26,46,.03);
}
.topbar-title { font-size: 16px; font-weight: 600; }
.main { padding: 18px 22px; overflow-y: auto; }
</style>
