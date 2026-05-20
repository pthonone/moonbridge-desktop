<script lang="ts" setup>
import { ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import Sidebar from './components/Sidebar.vue'
import LogViewer from './components/LogViewer.vue'
import ProviderEditor from './components/ProviderEditor.vue'
import ModelCard from './components/ModelCard.vue'
import { EventsOn, EventsOff } from '@wailsjs/runtime/runtime'
import { GetStatus, StartMoonBridge, StopMoonBridge, GetConfig, GetUsageStats, GetProviderPresets, ListModels, ListProviders, GetUsageDailyStats, GetUsageRecentRecords, GetUsageByModel, ClearUsageToday, ClearUsageAll } from '@wailsjs/go/main/App'

interface MBStatus { running: boolean; port: number; current_route: string; error: string }
interface DesktopConfig { port: number; log_level: string; providers: any[]; models: any[]; routes: any[]; default_route: string; max_tokens: number; metrics_enabled: boolean }
interface UsageStats { input_tokens: number; output_tokens: number; cache_read: number; cache_write: number; total_cost: number; request_count: number; cache_hit_rate: number }
interface DailyRow { date: string; input_tokens: number; output_tokens: number; cache_read: number; cache_write: number; cost: number; request_count: number }
interface UsageRec { id: number; timestamp: string; model: string; input_tokens: number; output_tokens: number; cache_read: number; cache_write: number; cost: number }

const status = ref<MBStatus>({ running: false, port: 38440, current_route: 'moonbridge', error: '' })
const config = ref<DesktopConfig>({ port: 38440, log_level: 'info', providers: [], models: [], routes: [], default_route: 'moonbridge', max_tokens: 65536, metrics_enabled: true })
const usage = ref<UsageStats>({ input_tokens: 0, output_tokens: 0, cache_read: 0, cache_write: 0, total_cost: 0, request_count: 0, cache_hit_rate: 0 })
const models = ref<any[]>([])
const providers = ref<any[]>([])
const routes = ref<any[]>([])
const loading = ref(false)
const showProviderEditor = ref(false)
const selectedPreset = ref<any>(null)
const editingProviderKey = ref<string | null>(null)
const statusError = ref('')
const activeTab = ref('models')

// Models tab state
const modelSearch = ref('')
const modelCapFilter = ref('')

const ALL_CAPS = [
  { id: 'vision', label: '视觉' },
  { id: 'reasoning', label: '推理' },
  { id: 'coding', label: '编码' },
  { id: 'long_context', label: '长上下文' },
  { id: 'web_search', label: '联网' },
]

// Build a map: model slug -> offer config (from providers)
const offerMap = computed(() => {
  const m: Record<string, { offer: any; providerName: string; providerKey: string }> = {}
  for (const p of providers.value) {
    for (const o of (p.offers || [])) {
      if (!m[o.model]) m[o.model] = { offer: o, providerName: p.name || p.key, providerKey: p.key }
    }
  }
  return m
})

// Filtered models for the 模型 tab
const filteredModels = computed(() => {
  const search = modelSearch.value.toLowerCase()
  const cap = modelCapFilter.value
  return models.value.filter(m => {
    if (search) {
      const name = (m.display_name || m.slug || '').toLowerCase()
      const provider = (offerMap.value[m.slug]?.providerName || '').toLowerCase()
      if (!name.includes(search) && !provider.includes(search)) return false
    }
    if (cap) {
      const caps = m.capabilities || []
      if (!caps.includes(cap)) return false
    }
    return true
  })
})

const groupedModels = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const m of filteredModels.value) {
    const series = m.series || '其他'
    if (!groups[series]) groups[series] = []
    groups[series].push(m)
  }
  const order = ['gpt', 'claude', 'qwen', 'gemini', 'deepseek', 'llama', 'codestral', 'glm']
  const sorted: { series: string; models: any[] }[] = []
  for (const s of order) {
    if (groups[s]) sorted.push({ series: s, models: groups[s] })
  }
  if (groups['其他']) sorted.push({ series: '其他', models: groups['其他'] })
  for (const [k, v] of Object.entries(groups)) {
    if (!order.includes(k) && k !== '其他') sorted.push({ series: k, models: v })
  }
  return sorted
})

// Providers tab state
const presets = ref<any[]>([])
const availablePresets = computed(() => {
  return presets.value.filter(pr => !providers.value.find(p => p.key === pr.key))
})

// Usage history data
const dailyStats = ref<DailyRow[]>([])
const recentRecords = ref<UsageRec[]>([])
const modelUsageRows = ref<any[]>([])
const usageRange = ref(7)
const usageStart = ref('')
const usageEnd = ref('')

function todayStr(): string {
  const d = new Date()
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')
}

function daysAgo(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')
}

const rangePresets = [
  { label: '近 7 天', days: 7 },
  { label: '近 14 天', days: 14 },
  { label: '近 30 天', days: 30 },
  { label: '近 90 天', days: 90 },
]

async function loadUsageByRange(days: number) {
  usageRange.value = days
  usageStart.value = daysAgo(days)
  usageEnd.value = todayStr()
  try {
    dailyStats.value = (await GetUsageDailyStats(days)) || []
    modelUsageRows.value = (await GetUsageByModel(usageStart.value, usageEnd.value)) || []
  } catch (e: any) {}
}

async function loadCustomRange() {
  try {
    dailyStats.value = (await GetUsageDailyStats(usageRange.value)) || []
    modelUsageRows.value = (await GetUsageByModel(usageStart.value, usageEnd.value)) || []
  } catch (e: any) {}
}

const usageTotal = computed(() => {
  let input = 0, output = 0, cacheRead = 0, cacheWrite = 0, cost = 0, requests = 0
  for (const r of modelUsageRows.value) {
    input += r.input_tokens || 0
    output += r.output_tokens || 0
    cacheRead += r.cache_read || 0
    cacheWrite += r.cache_write || 0
    cost += r.cost || 0
    requests += r.request_count || 0
  }
  return { input, output, cacheRead, cacheWrite, cost, requests }
})

let statsInterval: any = null
let statusInterval: any = null

async function refreshStatus() {
  try {
    status.value = await GetStatus()
  } catch (e: any) {
    console.error('Failed to get status:', e)
  }
}

async function refreshConfig() {
  try {
    config.value = await GetConfig()
    models.value = (await ListModels()) || []
    providers.value = (await ListProviders()) || []
    routes.value = config.value.routes || []
  } catch (e: any) {
    console.error('Failed to get config:', e)
  }
}

async function refreshUsage() {
  try {
    usage.value = await GetUsageStats()
  } catch (e: any) {}
}

async function refreshUsageHistory() {
  try {
    dailyStats.value = (await GetUsageDailyStats(7)) || []
    recentRecords.value = (await GetUsageRecentRecords(50)) || []
  } catch (e: any) {}
}

async function handleStart() {
  loading.value = true
  statusError.value = ''
  try {
    await StartMoonBridge()
    await refreshStatus()
    startStatsPolling()
  } catch (e: any) {
    statusError.value = e.message || String(e)
  } finally {
    loading.value = false
  }
}

async function handleStop() {
  loading.value = true
  try {
    await StopMoonBridge()
    stopStatsPolling()
  } catch (e: any) {
    statusError.value = e.message || String(e)
  } finally {
    loading.value = false
    await refreshStatus()
  }
}

async function handleSwitchRoute(alias: string) {
  const { SwitchModel } = await import('@wailsjs/go/main/App')
  try {
    await SwitchModel(alias)
    await refreshStatus()
    await refreshConfig()
  } catch (e: any) {
    statusError.value = e.message || String(e)
  }
}

async function handleAddProviderFromPreset(preset: any) {
  const existing = providers.value.find(p => p.key === preset.key)
  if (existing) {
    editingProviderKey.value = existing.key
    selectedPreset.value = {
      key: existing.key,
      base_url: existing.base_url,
      api_key: existing.api_key || '',
      protocol: existing.protocol,
      version: existing.version || '2023-06-01',
      offers: existing.offers || [],
    }
    showProviderEditor.value = true
  } else {
    selectedPreset.value = preset
    editingProviderKey.value = null
    showProviderEditor.value = true
  }
}

function startStatsPolling() {
  stopStatsPolling()
  statsInterval = setInterval(() => {
    refreshUsage()
  }, 2000)
  // Poll status to detect tray-initiated model switches
  statusInterval = setInterval(() => {
    refreshStatus()
  }, 3000)
}

function stopStatsPolling() {
  if (statsInterval) {
    clearInterval(statsInterval)
    statsInterval = null
  }
  if (statusInterval) {
    clearInterval(statusInterval)
    statusInterval = null
  }
}

async function handleProviderSaved(provider: any) {
  showProviderEditor.value = false
  selectedPreset.value = null
  editingProviderKey.value = null
  await refreshConfig()
  await refreshStatus()
  // Force providers list to update reactively
  providers.value = [...providers.value]
}

function handleCancelEditor() {
  showProviderEditor.value = false
  selectedPreset.value = null
  editingProviderKey.value = null
}

function handleEditProvider(key: string) {
  const existing = providers.value.find(p => p.key === key)
  if (existing) {
    editingProviderKey.value = key
    selectedPreset.value = {
      key: existing.key,
      base_url: existing.base_url,
      api_key: existing.api_key || '',
      protocol: existing.protocol,
      version: existing.version || '2023-06-01',
      offers: existing.offers || [],
    }
    showProviderEditor.value = true
  }
}

async function handleClearToday() {
  try {
    await ClearUsageToday()
    await refreshUsage()
    await refreshUsageHistory()
  } catch (e: any) {}
}

async function handleClearAll() {
  try {
    await ClearUsageAll()
    await refreshUsage()
    await refreshUsageHistory()
  } catch (e: any) {}
}

async function handleDeleteProvider(key: string) {
  const { DeleteProvider } = await import('@wailsjs/go/main/App')
  try {
    await DeleteProvider(key)
    await refreshConfig()
  } catch (e: any) {
    statusError.value = e.message || String(e)
  }
}

onMounted(async () => {
  // Load presets
  try {
    presets.value = await GetProviderPresets()
  } catch (e: any) {
    console.error('Failed to load presets:', e)
  }
  try {
    await refreshStatus()
  } catch (e: any) { console.error('[App] refreshStatus failed:', e) }
  try {
    await refreshConfig()
  } catch (e: any) { console.error('[App] refreshConfig failed:', e) }
  try {
    await refreshUsage()
  } catch (e: any) { console.error('[App] refreshUsage failed:', e) }
  try {
    await loadUsageByRange(7)
  } catch (e: any) { console.error('[App] loadUsageByRange failed:', e) }
  if (status.value.running) {
    startStatsPolling()
  }
  EventsOn('model-changed', () => {
    refreshStatus()
    refreshConfig()
  })
})

onUnmounted(() => {
  stopStatsPolling()
  EventsOff('model-changed')
})

// Template helper functions
function formatTokens(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(2) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}
function formatTime(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
function barWidth(d: any): number {
  const total = d.input_tokens + d.output_tokens
  const stats = dailyStats.value || []
  const max = Math.max(...stats.map((x: any) => x.input_tokens + x.output_tokens), 1)
  return Math.max((total / max) * 100, 2)
}
function capClass(cap: string): string {
  const map: Record<string, string> = {
    vision: 'cap-vision', reasoning: 'cap-reasoning', coding: 'cap-coding',
    long_context: 'cap-long-context', web_search: 'cap-web-search',
  }
  return map[cap] || ''
}
</script>

<template>
  <div class="app-container">
    <Sidebar
      :active-tab="activeTab"
      :provider-count="providers.length"
      :model-count="models.length"
      @tab-change="activeTab = $event"
    />
    <main class="main-panel">
      <div class="tab-content">
        <!-- 模型 + 服务状态 -->
        <div v-show="activeTab === 'models'" class="models-panel">
          <!-- Status bar -->
          <div class="status-bar">
            <div class="status-left">
              <div :class="['status-indicator', status.running ? 'running' : 'stopped']">
                <svg v-if="status.running" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="20 6 9 17 4 12"/>
                </svg>
                <svg v-else viewBox="0 0 24 24" fill="currentColor">
                  <rect x="6" y="6" width="12" height="12" rx="2"/>
                </svg>
              </div>
              <div>
                <span class="status-label">{{ status.running ? '运行中' : '已停止' }}</span>
                <span class="status-detail">端口 {{ status.port }} · 路由 {{ status.current_route }}</span>
              </div>
            </div>
            <div class="status-actions">
              <button class="btn btn-start" :disabled="loading || status.running" @click="handleStart">
                <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5,3 19,12 5,21"/></svg>
                {{ loading ? '启动中...' : '启动' }}
              </button>
              <button class="btn btn-stop" :disabled="loading || !status.running" @click="handleStop">
                <svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
                {{ loading ? '停止中...' : '停止' }}
              </button>
            </div>
          </div>

          <!-- Error -->
          <div v-if="statusError" class="error-banner">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
            </svg>
            {{ statusError }}
          </div>

          <!-- Model search + filter -->
          <div class="models-header">
            <input v-model="modelSearch" class="model-search" placeholder="搜索模型名称或服务提供商…" />
            <div class="cap-filters">
              <button v-for="cap in ALL_CAPS" :key="cap.id" :class="['cap-filter-btn', { active: modelCapFilter === cap.id }]" @click="modelCapFilter = modelCapFilter === cap.id ? '' : cap.id">
                {{ cap.label }}
              </button>
            </div>
          </div>

          <!-- Model cards grouped by series -->
          <div v-for="group in groupedModels" :key="group.series" class="model-group">
            <h3 class="group-header">{{ group.series }}</h3>
            <div class="model-list">
              <ModelCard v-for="m in group.models" :key="m.slug" :model="m" :offer="offerMap[m.slug]?.offer || null" :provider-name="offerMap[m.slug]?.providerName || ''" :is-active="m.slug === config.default_route" @set-default="handleSwitchRoute(m.slug)" />
            </div>
          </div>
          <div v-if="filteredModels.length === 0" class="no-data">暂无匹配的模型</div>
        </div>

        <!-- 服务 -->
        <div v-show="activeTab === 'providers'" class="providers-panel">
          <!-- Configured providers -->
          <section class="providers-section">
            <h3 class="section-label">已配置的服务</h3>
            <div v-for="p in providers" :key="p.key" class="provider-row">
              <div class="provider-main" @click="handleEditProvider(p.key)">
                <div :class="['provider-status-dot', p.api_key ? 'ready' : 'pending']"></div>
                <div class="provider-info">
                  <span class="provider-key">{{ p.key }}</span>
                  <span class="provider-url">{{ p.base_url }}</span>
                </div>
                <div class="provider-meta">
                  <span class="provider-protocol">{{ p.protocol }}</span>
                  <span class="provider-model-count">{{ (p.offers || []).length }} 模型</span>
                </div>
              </div>
              <button class="delete-provider-btn" @click.stop="handleDeleteProvider(p.key)" title="删除">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                </svg>
              </button>
            </div>
            <div v-if="providers.length === 0" class="empty-state">
              还没有配置服务，请从下方添加
            </div>
          </section>

          <!-- Available presets -->
          <section v-if="availablePresets.length > 0" class="presets-section">
            <h3 class="section-label">可添加的服务</h3>
            <div class="preset-grid">
              <div
                v-for="preset in availablePresets"
                :key="preset.key"
                class="preset-card"
                @click="handleAddProviderFromPreset(preset)"
              >
                <div class="preset-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                  </svg>
                </div>
                <span class="preset-name">{{ preset.name }}</span>
                <span class="preset-models">{{ (preset.models || []).map((m: any) => m.model).join('、') }}</span>
              </div>
            </div>
          </section>

          <!-- Custom provider button -->
          <button class="add-custom-btn" @click="() => { selectedPreset = null; showProviderEditor = true }">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>
            </svg>
            添加自定义服务
          </button>
        </div>

        <!-- 用量记录 -->
        <div v-show="activeTab === 'usage'" class="usage-panel">
          <!-- Summary cards -->
          <div class="usage-summary">
            <div class="summary-card">
              <span class="summary-label">总请求</span>
              <span class="summary-value">{{ usageTotal.requests }}</span>
            </div>
            <div class="summary-card">
              <span class="summary-label">总输入</span>
              <span class="summary-value">{{ formatTokens(usageTotal.input) }}</span>
            </div>
            <div class="summary-card">
              <span class="summary-label">总输出</span>
              <span class="summary-value">{{ formatTokens(usageTotal.output) }}</span>
            </div>
            <div class="summary-card">
              <span class="summary-label">总费用</span>
              <span class="summary-value">&yen;{{ usageTotal.cost.toFixed(4) }}</span>
            </div>
          </div>

          <!-- Date range selector -->
          <div class="usage-range-bar">
            <span class="range-label">时间区间</span>
            <div class="range-presets">
              <button v-for="p in rangePresets" :key="p.days"
                :class="['range-btn', { active: usageRange === p.days }]"
                @click="loadUsageByRange(p.days)">
                {{ p.label }}
              </button>
            </div>
            <input type="date" v-model="usageStart" class="date-input" />
            <span class="range-sep">至</span>
            <input type="date" v-model="usageEnd" class="date-input" />
            <button class="range-query-btn" @click="loadCustomRange">查询</button>
            <span class="clear-spacer"></span>
            <button class="clear-btn" @click="handleClearToday">清除今日</button>
            <button class="clear-btn clear-btn-danger" @click="handleClearAll">清除全部</button>
          </div>

          <!-- Daily chart -->
          <div class="daily-chart-card">
            <h3 class="card-title">日用量趋势</h3>
            <div class="daily-bars">
              <div v-for="d in dailyStats" :key="d.date" class="daily-bar-row">
                <span class="daily-date">{{ d.date.slice(5) }}</span>
                <div class="daily-bar-track">
                  <div class="daily-bar-fill" :style="{ width: barWidth(d) + '%' }"></div>
                </div>
                <span class="daily-count">{{ formatTokens(d.input_tokens + d.output_tokens) }}</span>
                <span class="daily-cost">&yen;{{ d.cost.toFixed(4) }}</span>
              </div>
              <div v-if="dailyStats.length === 0" class="no-data">暂无数据</div>
            </div>
          </div>

          <!-- Model-by-model usage table -->
          <div class="model-usage-card">
            <h3 class="card-title">按模型统计</h3>
            <table class="model-usage-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>请求数</th>
                  <th>输入</th>
                  <th>输出</th>
                  <th>缓存</th>
                  <th>费用</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in modelUsageRows" :key="r.model">
                  <td><span class="model-badge">{{ r.model }}</span></td>
                  <td>{{ r.request_count || 0 }}</td>
                  <td>{{ formatTokens(r.input_tokens || 0) }}</td>
                  <td>{{ formatTokens(r.output_tokens || 0) }}</td>
                  <td>{{ formatTokens((r.cache_read || 0) + (r.cache_write || 0)) }}</td>
                  <td class="cost-cell">&yen;{{ (r.cost || 0).toFixed(4) }}</td>
                </tr>
                <tr v-if="modelUsageRows.length === 0">
                  <td colspan="6" class="no-data-cell">暂无用量数据</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- 日志 -->
        <LogViewer v-show="activeTab === 'logs'" />
      </div>
    </main>
    <ProviderEditor
      v-if="showProviderEditor"
      :key="editingProviderKey || 'custom'"
      :preset="selectedPreset"
      :editing-key="editingProviderKey"
      @saved="handleProviderSaved"
      @cancel="handleCancelEditor"
    />
  </div>
</template>

<style>
.app-container {
  display: flex;
  height: 100vh;
  width: 100vw;
}

.main-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--gray-50);
}

.tab-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
  min-height: 0;
}

/* Usage panel */
.usage-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.usage-summary {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.summary-card {
  background: white;
  border-radius: var(--radius);
  padding: 16px 20px;
  box-shadow: var(--shadow);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.summary-label {
  font-size: 12px;
  color: var(--gray-500);
  font-weight: 500;
}

.summary-value {
  font-size: 24px;
  font-weight: 700;
  color: var(--gray-900);
  font-variant-numeric: tabular-nums;
}

/* Range selector */
.usage-range-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  background: white;
  padding: 12px 16px;
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.range-label {
  font-size: 13px;
  color: var(--gray-500);
  font-weight: 500;
  margin-right: 4px;
}

.range-presets {
  display: flex;
  gap: 6px;
}

.range-btn {
  padding: 5px 12px;
  border: 1px solid var(--gray-200);
  background: white;
  color: var(--gray-600);
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.range-btn:hover {
  border-color: var(--purple-300);
  color: var(--purple-500);
}

.range-btn.active {
  border-color: var(--purple-500);
  background: var(--purple-50);
  color: var(--purple-600);
}

.date-input {
  padding: 4px 8px;
  border: 1px solid var(--gray-200);
  border-radius: 6px;
  font-size: 12px;
  color: var(--gray-700);
  background: white;
}

.date-input:focus {
  outline: none;
  border-color: var(--purple-500);
}

.range-sep {
  font-size: 12px;
  color: var(--gray-400);
}

.range-query-btn {
  padding: 5px 14px;
  border: none;
  background: var(--purple-500);
  color: white;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}

.range-query-btn:hover {
  background: var(--purple-600);
}

.clear-spacer { flex: 1; }

.clear-btn {
  padding: 6px 14px;
  border: 1px solid var(--gray-300);
  background: white;
  color: var(--gray-600);
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.clear-btn:hover {
  border-color: var(--purple-500);
  color: var(--purple-500);
}

.clear-btn-danger:hover {
  border-color: var(--red-500);
  color: var(--red-500);
  background: var(--red-100);
}

/* Daily chart */
.daily-chart-card {
  background: white;
  border-radius: var(--radius);
  padding: 16px 20px;
  box-shadow: var(--shadow);
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--gray-800);
  margin-bottom: 12px;
}

.daily-bars {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.daily-bar-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.daily-date {
  font-size: 12px;
  color: var(--gray-400);
  min-width: 40px;
  font-variant-numeric: tabular-nums;
}

.daily-bar-track {
  flex: 1;
  height: 8px;
  background: var(--gray-100);
  border-radius: 4px;
  overflow: hidden;
}

.daily-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--purple-500), var(--purple-400));
  border-radius: 4px;
  transition: width 0.3s;
}

.daily-count {
  font-size: 12px;
  color: var(--gray-600);
  min-width: 50px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.daily-cost {
  font-size: 12px;
  color: var(--gray-500);
  min-width: 60px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.no-data {
  text-align: center;
  color: var(--gray-400);
  padding: 20px;
  font-size: 13px;
}

/* Model usage table */
.model-usage-card {
  background: white;
  border-radius: var(--radius);
  padding: 16px 20px;
  box-shadow: var(--shadow);
}

.model-usage-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.model-usage-table thead th {
  text-align: left;
  padding: 8px 12px;
  border-bottom: 2px solid var(--gray-200);
  color: var(--gray-500);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
}

.model-usage-table tbody td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--gray-100);
  color: var(--gray-700);
}

.model-usage-table tbody tr:hover {
  background: var(--gray-50);
}

.model-badge {
  background: var(--purple-100);
  color: var(--purple-600);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
}

.cost-cell {
  color: var(--gray-900) !important;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

/* Models panel */
.models-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* Status bar */
.status-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: white;
  border-radius: 14px;
  padding: 14px 20px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.08);
}

.status-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.status-indicator {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.status-indicator.running {
  background: var(--green-100);
  color: var(--green-500);
}

.status-indicator.stopped {
  background: var(--red-100);
  color: var(--red-500);
}

.status-indicator svg {
  width: 18px;
  height: 18px;
}

.status-label {
  font-weight: 600;
  font-size: 14px;
  color: var(--gray-800);
  display: block;
}

.status-detail {
  font-size: 12px;
  color: var(--gray-400);
}

.status-actions {
  display: flex;
  gap: 8px;
}

.btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 18px;
  border: none;
  border-radius: 10px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: all 0.15s;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn svg {
  width: 14px;
  height: 14px;
}

.btn-start {
  background: var(--purple-500);
  color: white;
}

.btn-start:hover:not(:disabled) {
  background: var(--purple-600);
  box-shadow: 0 4px 12px rgba(124, 58, 237, 0.3);
}

.btn-stop {
  background: var(--gray-100);
  color: var(--gray-600);
}

.btn-stop:hover:not(:disabled) {
  background: var(--red-100);
  color: var(--red-500);
}

/* Error banner */
.error-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--red-100);
  border: 1px solid #FECACA;
  color: #DC2626;
  padding: 10px 16px;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 500;
}

.error-banner svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

/* Stats grid */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.stat-card {
  background: white;
  border-radius: 14px;
  padding: 18px 20px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.08);
  display: flex;
  align-items: center;
  gap: 14px;
  transition: box-shadow 0.15s;
}

.models-header {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.model-search {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--gray-200);
  border-radius: var(--radius-sm);
  font-size: 14px;
  background: white;
  box-shadow: var(--shadow);
}

.model-search:focus {
  outline: none;
  border-color: var(--purple-500);
  box-shadow: 0 0 0 3px var(--purple-100);
}

.cap-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.cap-filter-btn {
  padding: 4px 12px;
  border-radius: 12px;
  border: 1px solid var(--gray-200);
  background: white;
  color: var(--gray-500);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.cap-filter-btn:hover {
  border-color: var(--purple-300);
  color: var(--purple-600);
}

.cap-filter-btn.active {
  border-color: var(--purple-500);
  background: var(--purple-50);
  color: var(--purple-600);
}

.model-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.group-header {
  font-size: 13px;
  font-weight: 600;
  color: var(--gray-500);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--gray-200);
}

.model-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Providers panel */
.providers-panel {
  display: flex;
  flex-direction: column;
  gap: 24px;
  max-width: 720px;
}

.providers-section,
.presets-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.section-label {
  font-size: 12px;
  color: var(--gray-500);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
}

.provider-row {
  display: flex;
  align-items: center;
  gap: 8px;
  background: white;
  border-radius: var(--radius);
  padding: 12px 16px;
  box-shadow: var(--shadow);
  transition: all 0.15s;
}

.provider-row:hover {
  box-shadow: var(--shadow-md);
}

.provider-main {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  min-width: 0;
}

.provider-status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.provider-status-dot.ready {
  background: var(--green-500);
  box-shadow: 0 0 0 3px var(--green-100);
}

.provider-status-dot.pending {
  background: var(--amber-500);
  box-shadow: 0 0 0 3px var(--amber-100);
}

.provider-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.provider-key {
  font-size: 14px;
  font-weight: 600;
  color: var(--gray-800);
}

.provider-url {
  font-size: 12px;
  color: var(--gray-400);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.provider-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.provider-protocol {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 6px;
  background: var(--gray-100);
  color: var(--gray-500);
  font-weight: 500;
}

.provider-model-count {
  font-size: 12px;
  color: var(--gray-500);
}

.delete-provider-btn {
  background: none;
  border: none;
  color: var(--gray-300);
  cursor: pointer;
  padding: 6px;
  border-radius: 6px;
  transition: all 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.delete-provider-btn svg {
  width: 16px;
  height: 16px;
}

.delete-provider-btn:hover {
  color: var(--red-500);
  background: var(--red-100);
}

.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
}

.preset-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 16px;
  background: white;
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  cursor: pointer;
  transition: all 0.15s;
  border: 1px solid transparent;
}

.preset-card:hover {
  border-color: var(--purple-300);
  box-shadow: var(--shadow-md);
  transform: translateY(-1px);
}

.preset-icon {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: var(--purple-100);
  color: var(--purple-500);
  display: flex;
  align-items: center;
  justify-content: center;
}

.preset-icon svg {
  width: 18px;
  height: 18px;
}

.preset-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--gray-700);
}

.preset-models {
  font-size: 11px;
  color: var(--gray-400);
  text-align: center;
  line-height: 1.4;
  word-break: break-word;
}

.add-custom-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  background: white;
  border: 1px dashed var(--gray-300);
  color: var(--gray-500);
  padding: 14px 16px;
  border-radius: var(--radius);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.15s;
}

.add-custom-btn svg {
  width: 16px;
  height: 16px;
}

.add-custom-btn:hover {
  border-color: var(--purple-500);
  color: var(--purple-500);
  background: var(--purple-50);
}

.empty-state {
  color: var(--gray-400);
  font-size: 13px;
  text-align: center;
  padding: 24px;
  background: white;
  border-radius: var(--radius);
}
</style>
