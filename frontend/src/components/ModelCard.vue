<script lang="ts" setup>
const props = defineProps<{
  model: any
  offer: any | null
  providerName: string
  isActive: boolean
}>()

const emit = defineEmits<{
  'set-default': []
}>()

const capLabels: Record<string, string> = {
  vision: '视觉',
  reasoning: '推理',
  coding: '编码',
  long_context: '长上下文',
  web_search: '联网',
}

function capClass(cap: string): string {
  const map: Record<string, string> = {
    vision: 'cap-vision',
    reasoning: 'cap-reasoning',
    coding: 'cap-coding',
    long_context: 'cap-long-context',
    web_search: 'cap-web-search',
  }
  return map[cap] || ''
}

function tierLabel(tier: string): string {
  const map: Record<string, string> = {
    free: '免费',
    economy: '经济',
    standard: '标准',
    premium: '高级',
    ultra: '旗舰',
  }
  return map[tier] || ''
}

function tierClass(tier: string): string {
  const map: Record<string, string> = {
    free: 'tier-free',
    economy: 'tier-economy',
    standard: 'tier-standard',
    premium: 'tier-premium',
    ultra: 'tier-ultra',
  }
  return map[tier] || ''
}

function pricingText(): string {
  const offer = props.offer
  if (!offer || !offer.pricing) return '-'
  const p = offer.pricing
  if (p.billing_mode === 'per_request') {
    return `¥${p.per_request_cost?.toFixed(4) || '0.0022'}/次`
  }
  if (p.billing_mode === 'free') return '免费'
  const parts: string[] = []
  if (p.input > 0) parts.push(`¥${p.input}/M入`)
  if (p.output > 0) parts.push(`¥${p.output}/M出`)
  return parts.join(' · ') || '-'
}
</script>

<template>
  <div :class="['model-card', { active: isActive }]">
    <div class="model-card-left" @click="emit('set-default')">
      <div class="model-name-row">
        <span class="model-name">{{ model.display_name || model.slug }}</span>
        <span v-if="offer?.pricing_tier" :class="['tier-badge', tierClass(offer.pricing_tier)]">
          {{ tierLabel(offer.pricing_tier) }}
        </span>
      </div>
      <div class="model-meta">
        <span class="meta-provider">{{ providerName }}</span>
        <span v-if="model.context_window" class="meta-sep">·</span>
        <span v-if="model.context_window" class="meta-item">{{ (model.context_window / 1000).toFixed(0) }}K ctx</span>
        <span v-if="model.max_output_tokens" class="meta-sep">·</span>
        <span v-if="model.max_output_tokens" class="meta-item">{{ (model.max_output_tokens / 1024).toFixed(0) }}K out</span>
      </div>
    </div>
    <div class="model-card-right">
      <div class="model-caps">
        <span v-for="cap in (model.capabilities || [])" :key="cap" :class="['cap-pill', capClass(cap)]">
          {{ capLabels[cap] || cap }}
        </span>
      </div>
      <div class="model-pricing">{{ pricingText() }}</div>
    </div>
    <button v-if="!isActive" class="set-default-btn" @click="emit('set-default')" title="设为默认">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="20 6 9 17 4 12"/>
      </svg>
    </button>
    <span v-else class="active-badge">默认</span>
  </div>
</template>

<style scoped>
.model-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: white;
  border-radius: var(--radius);
  border: 1px solid var(--gray-200);
  transition: all 0.15s;
  min-height: 56px;
}

.model-card:hover {
  border-color: var(--purple-300);
  box-shadow: var(--shadow);
}

.model-card.active {
  border-color: var(--purple-500);
  background: var(--purple-50);
}

.model-card-left {
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

.model-name-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.model-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--gray-800);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.model-meta {
  font-size: 12px;
  color: var(--gray-400);
  display: flex;
  align-items: center;
  gap: 4px;
}

.meta-sep {
  color: var(--gray-300);
}

.model-card-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  flex-shrink: 0;
}

.model-caps {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  justify-content: flex-end;
}

.cap-pill {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  font-weight: 500;
  line-height: 1.4;
}

.cap-vision { background: var(--cap-vision-bg); color: var(--cap-vision-text); }
.cap-reasoning { background: var(--cap-reasoning-bg); color: var(--cap-reasoning-text); }
.cap-coding { background: var(--cap-coding-bg); color: var(--cap-coding-text); }
.cap-long-context { background: var(--cap-long-context-bg); color: var(--cap-long-context-text); }
.cap-web-search { background: var(--cap-web-search-bg); color: var(--cap-web-search-text); }

.model-pricing {
  font-size: 11px;
  color: var(--gray-500);
  text-align: right;
  white-space: nowrap;
}

.tier-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 8px;
  font-weight: 600;
  flex-shrink: 0;
}

.tier-free { background: var(--tier-free-bg); color: var(--tier-free-text); }
.tier-economy { background: var(--tier-economy-bg); color: var(--tier-economy-text); }
.tier-standard { background: var(--tier-standard-bg); color: var(--tier-standard-text); }
.tier-premium { background: var(--tier-premium-bg); color: var(--tier-premium-text); }
.tier-ultra { background: var(--tier-ultra-bg); color: var(--tier-ultra-text); }

.set-default-btn {
  background: transparent;
  border: 1px solid var(--gray-200);
  color: var(--gray-400);
  width: 28px;
  height: 28px;
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  flex-shrink: 0;
}

.set-default-btn svg {
  width: 14px;
  height: 14px;
}

.set-default-btn:hover {
  border-color: var(--green-500);
  color: var(--green-500);
  background: var(--green-100);
}

.active-badge {
  font-size: 11px;
  color: var(--purple-600);
  background: var(--purple-100);
  padding: 4px 10px;
  border-radius: 10px;
  font-weight: 600;
  flex-shrink: 0;
}
</style>
