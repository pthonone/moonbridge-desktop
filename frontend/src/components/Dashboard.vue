<script lang="ts" setup>
const props = defineProps<{
  status: { running: boolean; port: number; current_route: string; error: string }
  usage: { input_tokens: number; output_tokens: number; cache_read: number; cache_write: number; total_cost: number; request_count: number; cache_hit_rate: number }
  config: any
  models: any[]
  dailyStats: any[]
  error: string
  loading: boolean
}>()

const emit = defineEmits<{
  start: []
  stop: []
  'switch-model': [alias: string]
}>()

function formatTokens(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(2) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}
</script>

<template>
  <div class="dashboard">
    <!-- Status Bar -->
    <div class="status-bar">
      <div class="status-left">
        <div :class="['status-indicator', status.running ? 'running' : 'stopped']">
          <svg v-if="status.running" class="status-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg>
          <svg v-else class="status-icon" viewBox="0 0 24 24" fill="currentColor">
            <rect x="6" y="6" width="12" height="12" rx="2"/>
          </svg>
        </div>
        <div>
          <span class="status-label">{{ status.running ? '运行中' : '已停止' }}</span>
          <span class="status-detail">端口 {{ status.port }} &middot; 路由 {{ status.current_route }}</span>
        </div>
      </div>
      <div class="status-actions">
        <button class="btn btn-start" :disabled="loading || status.running" @click="emit('start')">
          <svg class="btn-icon" viewBox="0 0 24 24" fill="currentColor"><polygon points="5,3 19,12 5,21"/></svg>
          {{ loading ? '启动中...' : '启动' }}
        </button>
        <button class="btn btn-stop" :disabled="loading || !status.running" @click="emit('stop')">
          <svg class="btn-icon" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
          {{ loading ? '停止中...' : '停止' }}
        </button>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="error-banner">
      <svg class="error-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
      </svg>
      {{ error }}
    </div>

    <!-- Stats Cards -->
    <div class="stats-grid">
      <div class="stat-card stat-input">
        <div class="stat-icon-wrap">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
          </svg>
        </div>
        <div class="stat-info">
          <span class="stat-label">输入 Tokens</span>
          <span class="stat-value">{{ formatTokens(usage.input_tokens) }}</span>
        </div>
      </div>
      <div class="stat-card stat-output">
        <div class="stat-icon-wrap">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
          </svg>
        </div>
        <div class="stat-info">
          <span class="stat-label">输出 Tokens</span>
          <span class="stat-value">{{ formatTokens(usage.output_tokens) }}</span>
        </div>
      </div>
      <div class="stat-card stat-cache">
        <div class="stat-icon-wrap">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>
          </svg>
        </div>
        <div class="stat-info">
          <span class="stat-label">缓存读取</span>
          <span class="stat-value">{{ formatTokens(usage.cache_read) }}</span>
        </div>
      </div>
      <div class="stat-card stat-requests">
        <div class="stat-icon-wrap">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
          </svg>
        </div>
        <div class="stat-info">
          <span class="stat-label">请求数</span>
          <span class="stat-value">{{ usage.request_count }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dashboard {
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

.status-icon {
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

.btn-icon {
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

.error-icon {
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

.stat-card:hover {
  box-shadow: 0 4px 12px rgba(0,0,0,0.1);
}

.stat-icon-wrap {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.stat-input .stat-icon-wrap { background: #EDE9FE; color: #7C3AED; }
.stat-output .stat-icon-wrap { background: #DBEAFE; color: #3B82F6; }
.stat-cache .stat-icon-wrap { background: #FEF3C7; color: #F59E0B; }
.stat-requests .stat-icon-wrap { background: #DCFCE7; color: #22C55E; }

.stat-icon-wrap svg {
  width: 20px;
  height: 20px;
}

.stat-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.stat-label {
  font-size: 12px;
  color: var(--gray-500);
  font-weight: 500;
}

.stat-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--gray-900);
  font-variant-numeric: tabular-nums;
}
</style>
