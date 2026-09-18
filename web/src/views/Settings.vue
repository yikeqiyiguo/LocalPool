<template>
  <div class="page settings">
    <el-row :gutter="16">
      <el-col :span="12">
        <div class="card">
          <div class="card-head">
            <b><el-icon style="vertical-align:-2px"><UserFilled /></el-icon> 人机共存策略</b>
            <el-tag type="warning" size="small" style="margin-left:10px">LocalPool 独有特性</el-tag>
          </div>
          <p class="desc">Agent 检测到本机有人使用（键鼠活跃）时：不再往该机器下发“仅空闲执行”的新任务，并把正在运行的闲置任务终止/避让；用户离开恢复空闲后自动恢复调度。</p>

          <el-form label-width="170px">
            <el-form-item label="启用空闲调度与避让">
              <el-switch v-model="cfg.human_enabled" />
              <div class="hint">关闭后不再限制“仅空闲执行”任务的下发</div>
            </el-form-item>
            <el-form-item label="被占用时终止运行任务">
              <el-switch v-model="cfg.evict_on_human_active" />
              <div class="hint">机器检测到人机活动时终止其上正在运行的闲置任务</div>
            </el-form-item>
            <el-form-item label="判定“空闲”的阈值(秒)">
              <el-input-number v-model="cfg.node_idle_threshold_sec" :min="10" :max="3600 * 3" />
              <div class="hint">距离最近一次键鼠操作超过该秒数才认为空闲</div>
            </el-form-item>
          </el-form>
        </div>

        <div class="card" style="margin-top:16px">
          <div class="card-head"><b><el-icon style="vertical-align:-2px"><Operation /></el-icon> 默认任务参数</b></div>
          <el-form label-width="170px">
            <el-form-item label="默认超时(秒)">
              <el-input-number v-model="cfg.default_timeout_sec" :min="60" :max="86400 * 30" />
            </el-form-item>
            <el-form-item label="Web 标题">
              <el-input v-model="cfg.web_title" style="max-width:300px" />
            </el-form-item>
          </el-form>
        </div>
      </el-col>

      <el-col :span="12">
        <div class="card">
          <div class="card-head"><b><el-icon style="vertical-align:-2px"><Cpu /></el-icon> 调度参数</b></div>
          <el-form label-width="170px">
            <el-form-item label="心跳间隔(ms)">
              <el-input-number v-model="cfg.heartbeat_ms" :min="500" :max="30000" :step="500" />
              <div class="hint">Agent 心跳上报与领取任务的频率</div>
            </el-form-item>
            <el-form-item label="离线判定(ms)">
              <el-input-number v-model="cfg.offline_grace_ms" :min="3000" :max="60000" :step="1000" />
              <div class="hint">超过该时长无心跳即视为节点离线</div>
            </el-form-item>
            <el-form-item label="单节点并发任务数">
              <el-input-number v-model="cfg.max_tasks_per_node" :min="1" :max="32" />
            </el-form-item>
            <el-form-item label="节点丢失自动重试次数">
              <el-input-number v-model="cfg.max_retries" :min="0" :max="10" />
              <div class="hint">节点离线等异常导致的失败是否自动创建重试任务</div>
            </el-form-item>
            <el-form-item label="调度扫描间隔(ms)">
              <el-input-number v-model="cfg.dispatch_interval_ms" :min="200" :max="10000" :step="200" />
            </el-form-item>
          </el-form>
        </div>

        <div class="card" style="margin-top:16px">
          <div class="card-head"><b><el-icon style="vertical-align:-2px"><InfoFilled /></el-icon> 策略说明</b></div>
          <ul class="tips">
            <li>调度策略：<b>贪心调度</b>——从排队任务中按“优先级高 → 提交早”排序，优先分配到最空闲、资源充足的在线节点。</li>
            <li>资源软限制：任务可声明 CPU/内存上限；Windows 上超出后先降优先级，超内存终止；Linux 递归统计整棵进程树。</li>
            <li>队列与历史均持久化到 SQLite，服务重启不丢失；节点断网自动判离线并支持自动重试。</li>
            <li>MVP 边界：不做任务迁移与 checkpoint 续跑；强资源隔离留待容器化(roadmap V0.4)。</li>
          </ul>
        </div>
      </el-col>
    </el-row>

    <div class="row" style="margin-top:18px; justify-content:center; gap:12px">
      <el-button @click="load">恢复当前配置</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存设置</el-button>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const cfg = reactive({})
const saving = ref(false)

async function load() {
  Object.assign(cfg, await api.config())
}
async function save() {
  saving.value = true
  try {
    Object.assign(cfg, await api.saveConfig({ ...cfg }))
    ElMessage.success('设置已保存并持久化')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}
onMounted(load)
</script>

<style scoped>
.desc { color: #5b6b7f; font-size: 12.5px; line-height: 1.7; background: #f7f9fe; border-radius: 8px; padding: 10px 12px; margin: 0 0 8px; }
.hint { color: #98a2b3; font-size: 11px; margin-left: 12px; max-width: 220px; }
.tips { color: #5b6b7f; font-size: 12.5px; line-height: 2; padding-left: 18px; margin: 0; }
</style>
