<script lang="ts" setup>
const props = defineProps<{
  activeTab: string
  providerCount: number
  modelCount: number
}>()

const emit = defineEmits<{
  'tab-change': [tab: string]
}>()

const navItems = [
  { id: 'models', label: '模型', icon: 'models' },
  { id: 'providers', label: '服务', icon: 'providers' },
  { id: 'usage', label: '用量记录', icon: 'usage' },
  { id: 'logs', label: '日志', icon: 'logs' },
]
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-header">
      <div class="logo-wrap">
        <svg class="logo-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/>
        </svg>
        <h2>Moon Bridge</h2>
      </div>
    </div>

    <nav class="nav-list">
      <button
        v-for="item in navItems"
        :key="item.id"
        :class="['nav-item', { active: props.activeTab === item.id }]"
        @click="emit('tab-change', item.id)"
      >
        <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <template v-if="item.icon === 'dashboard'">
            <rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/>
          </template>
          <template v-else-if="item.icon === 'models'">
            <path d="M4 7h16M4 12h16M4 17h16"/>
          </template>
          <template v-else-if="item.icon === 'providers'">
            <rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/>
          </template>
          <template v-else-if="item.icon === 'usage'">
            <path d="M21.21 15.89A10 10 0 1 1 8 2.83"/><path d="M22 12A10 10 0 0 0 12 2v10z"/>
          </template>
          <template v-else-if="item.icon === 'logs'">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
          </template>
        </svg>
        <span class="nav-label">{{ item.label }}</span>
        <span v-if="item.id === 'providers'" class="nav-badge">{{ providerCount }}</span>
        <span v-if="item.id === 'models'" class="nav-badge">{{ modelCount }}</span>
      </button>
    </nav>

    <div class="sidebar-footer">
      <span class="footer-text">v1.0.0</span>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 200px;
  background: var(--gray-900);
  display: flex;
  flex-direction: column;
  padding: 0;
  overflow: hidden;
  flex-shrink: 0;
}

.sidebar-header {
  padding: 20px 16px;
  border-bottom: 1px solid var(--gray-700);
}

.logo-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
}

.logo-icon {
  width: 24px;
  height: 24px;
  color: var(--purple-400);
}

.logo-wrap h2 {
  font-size: 15px;
  font-weight: 700;
  color: var(--gray-100);
}

.nav-list {
  flex: 1;
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: none;
  background: transparent;
  color: var(--gray-400);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border-radius: var(--radius-sm);
  transition: all 0.15s;
  text-align: left;
  width: 100%;
}

.nav-item:hover {
  background: var(--gray-800);
  color: var(--gray-200);
}

.nav-item.active {
  background: var(--purple-600);
  color: white;
}

.nav-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

.nav-label {
  flex: 1;
  white-space: nowrap;
}

.nav-badge {
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 10px;
  background: var(--gray-700);
  color: var(--gray-300);
  font-weight: 600;
  min-width: 20px;
  text-align: center;
}

.nav-item.active .nav-badge {
  background: rgba(255,255,255,0.2);
  color: white;
}

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--gray-700);
}

.footer-text {
  font-size: 11px;
  color: var(--gray-600);
}
</style>
