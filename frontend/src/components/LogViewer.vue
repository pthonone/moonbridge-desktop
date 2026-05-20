<script lang="ts" setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { EventsOn, EventsOff } from '@wailsjs/runtime/runtime'

interface LogEntry {
  timestamp: string
  level: string
  message: string
  raw: string
}

const logs = ref<LogEntry[]>([])
const filterLevel = ref('ALL')
const autoScroll = ref(true)
const logContainer = ref<HTMLElement | null>(null)

const MAX_LOGS = 500

function addLog(entry: LogEntry) {
  logs.value.push(entry)
  if (logs.value.length > MAX_LOGS) {
    logs.value = logs.value.slice(-MAX_LOGS)
  }
  if (autoScroll.value) {
    nextTick(() => {
      if (logContainer.value) {
        logContainer.value.scrollTop = logContainer.value.scrollHeight
      }
    })
  }
}

function filteredLogs() {
  if (filterLevel.value === 'ALL') return logs.value
  return logs.value.filter(l => l.level === filterLevel.value)
}

function levelColor(level: string): string {
  switch (level) {
    case 'ERROR': return '#EF4444'
    case 'WARN': return '#F59E0B'
    case 'INFO': return '#3B82F6'
    case 'DEBUG': return var_gray_400()
    default: return var_gray_400()
  }
}

function var_gray_400(): string { return '#9CA3AF' }

function levelBadgeClass(level: string): string {
  switch (level) {
    case 'ERROR': return 'badge-error'
    case 'WARN': return 'badge-warn'
    case 'INFO': return 'badge-info'
    case 'DEBUG': return 'badge-debug'
    default: return ''
  }
}

onMounted(() => {
  EventsOn('log-entry', (entry: LogEntry) => {
    addLog(entry)
  })
})

onUnmounted(() => {
  EventsOff('log-entry')
})
</script>

<template>
  <div class="log-viewer">
    <div class="log-header">
      <div class="log-title-wrap">
        <svg class="log-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
        </svg>
        <h3>活动日志</h3>
      </div>
      <div class="log-controls">
        <div class="filter-group">
          <button v-for="lvl in ['ALL', 'INFO', 'WARN', 'ERROR']" :key="lvl"
            :class="['filter-chip', filterLevel === lvl ? 'active' : '']"
            @click="filterLevel = lvl">
            {{ lvl === 'ALL' ? '全部' : lvl }}
          </button>
        </div>
        <label class="auto-scroll-label">
          <input type="checkbox" v-model="autoScroll" /> 自动滚动
        </label>
        <button class="clear-btn" @click="logs = []">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
          </svg>
        </button>
      </div>
    </div>
    <div ref="logContainer" class="log-content">
      <div
        v-for="(log, i) in filteredLogs()"
        :key="i"
        class="log-line"
      >
        <span class="log-level-badge" :class="levelBadgeClass(log.level)">{{ log.level }}</span>
        <span class="log-time">{{ log.timestamp }}</span>
        <span class="log-msg">{{ log.message }}</span>
      </div>
      <div v-if="logs.length === 0" class="log-empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
        </svg>
        <p>暂无日志</p>
        <span>启动 Moon Bridge 后可查看活动日志</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.log-viewer {
  background: white;
  border-radius: 14px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.08);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 200px;
}

.log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--gray-100);
}

.log-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}

.log-icon {
  width: 18px;
  height: 18px;
  color: var(--gray-400);
}

.log-header h3 {
  font-size: 13px;
  font-weight: 600;
  color: var(--gray-700);
}

.log-controls {
  display: flex;
  align-items: center;
  gap: 10px;
}

.filter-group {
  display: flex;
  gap: 4px;
}

.filter-chip {
  padding: 4px 10px;
  border: 1px solid var(--gray-200);
  background: transparent;
  color: var(--gray-500);
  border-radius: 6px;
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.filter-chip:hover {
  border-color: var(--gray-300);
  color: var(--gray-700);
}

.filter-chip.active {
  background: var(--purple-500);
  border-color: var(--purple-500);
  color: white;
}

.auto-scroll-label {
  font-size: 12px;
  color: var(--gray-400);
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

.clear-btn {
  background: transparent;
  border: 1px solid var(--gray-200);
  color: var(--gray-400);
  width: 28px;
  height: 28px;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  padding: 0;
}

.clear-btn svg {
  width: 14px;
  height: 14px;
}

.clear-btn:hover {
  border-color: var(--red-500);
  color: var(--red-500);
  background: var(--red-100);
}

.log-content {
  flex: 1;
  overflow-y: auto;
  padding: 8px 16px;
  font-family: 'Cascadia Code', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.7;
  min-height: 0;
}

.log-line {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 2px 0;
}

.log-level-badge {
  font-size: 10px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 4px;
  min-width: 42px;
  text-align: center;
  flex-shrink: 0;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
}

.badge-info { background: #DBEAFE; color: #2563EB; }
.badge-warn { background: #FEF3C7; color: #D97706; }
.badge-error { background: #FEE2E2; color: #DC2626; }
.badge-debug { background: var(--gray-100); color: var(--gray-500); }

.log-time {
  color: var(--gray-300);
  min-width: 65px;
  font-size: 11px;
}

.log-msg {
  color: var(--gray-600);
  word-break: break-all;
}

.log-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  color: var(--gray-400);
  padding: 40px 0;
}

.log-empty svg {
  width: 40px;
  height: 40px;
  opacity: 0.3;
}

.log-empty p {
  font-size: 14px;
  font-weight: 500;
}

.log-empty span {
  font-size: 12px;
}
</style>
