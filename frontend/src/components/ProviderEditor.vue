<script lang="ts" setup>
import { ref, onMounted, watch } from 'vue'

const props = defineProps<{
  preset: any | null
  editingKey: string | null
}>()

const emit = defineEmits<{
  saved: [provider: any]
  cancel: []
}>()

const ALL_CAPS = ['vision', 'reasoning', 'coding', 'long_context', 'web_search']
const capLabels: Record<string, string> = {
  vision: '视觉',
  reasoning: '推理',
  coding: '编码',
  long_context: '长上下文',
  web_search: '联网',
}

interface ModelForm {
  name: string
  series: string
  capabilities: string[]
  billing_mode: string
  pricing_tier: string
  input: number
  output: number
  cache_write: number
  cache_read: number
  per_request_cost: number
}

const form = ref({
  key: '',
  base_url: '',
  api_key: '',
  protocol: 'anthropic',
  version: '2023-06-01',
  models: [] as ModelForm[]
})

const showKey = ref(false)
const saving = ref(false)
const error = ref('')
const isEdit = ref(false)

function populateForm() {
  if (props.editingKey) {
    isEdit.value = true
    if (props.preset) {
      form.value.key = props.preset.key
      form.value.base_url = props.preset.base_url
      form.value.api_key = props.preset.api_key || ''
      form.value.protocol = props.preset.protocol
      form.value.version = props.preset.version || '2023-06-01'
      form.value.models = (props.preset.offers || []).map((m: any) => ({
        name: m.model || '',
        series: m.series || '',
        capabilities: m.capabilities || [],
        billing_mode: m.pricing?.billing_mode || 'token',
        pricing_tier: m.pricing_tier || 'standard',
        input: m.pricing?.input || 0,
        output: m.pricing?.output || 0,
        cache_write: m.pricing?.cache_write || 0,
        cache_read: m.pricing?.cache_read || 0,
        per_request_cost: m.pricing?.per_request_cost || 0,
      }))
      if (form.value.models.length === 0) form.value.models.push(newModel())
    }
  } else if (props.preset) {
    isEdit.value = false
    form.value.key = props.preset.key
    form.value.base_url = props.preset.base_url
    form.value.protocol = props.preset.protocol
    form.value.version = props.preset.version || '2023-06-01'
    form.value.models = (props.preset.models || []).map((m: any) => ({
      name: m.model || '',
      series: m.series || '',
      capabilities: m.capabilities || [],
      billing_mode: m.pricing?.billing_mode || 'token',
      pricing_tier: m.pricing_tier || 'standard',
      input: m.pricing?.input || 0,
      output: m.pricing?.output || 0,
      cache_write: m.pricing?.cache_write || 0,
      cache_read: m.pricing?.cache_read || 0,
      per_request_cost: m.pricing?.per_request_cost || 0,
    }))
  } else {
    form.value.models = [newModel()]
  }
}

onMounted(populateForm)
watch(() => [props.editingKey, props.preset], populateForm)

function newModel(): ModelForm {
  return {
    name: '', series: '', capabilities: [], billing_mode: 'token', pricing_tier: 'standard',
    input: 0, output: 0, cache_write: 0, cache_read: 0, per_request_cost: 0,
  }
}

function addModel() {
  form.value.models.push(newModel())
}

function removeModel(idx: number) {
  form.value.models.splice(idx, 1)
}

function toggleCap(m: ModelForm, cap: string) {
  const i = m.capabilities.indexOf(cap)
  if (i >= 0) m.capabilities.splice(i, 1)
  else m.capabilities.push(cap)
}

async function handleSave() {
  const validModels = form.value.models.filter(m => m.name.trim())
  if (!form.value.key || !form.value.base_url || !form.value.api_key) {
    error.value = '服务标识、地址和 API 密钥为必填项。'
    return
  }
  error.value = ''
  saving.value = true
  try {
    const { AddProvider, UpdateProvider } = await import('@wailsjs/go/main/App')
    const offers = validModels.map(m => {
      const pricing: any = { billing_mode: m.billing_mode }
      if (m.billing_mode === 'token') {
        pricing.input = m.input; pricing.output = m.output
        pricing.cache_write = m.cache_write; pricing.cache_read = m.cache_read
      } else if (m.billing_mode === 'per_request') {
        pricing.per_request_cost = m.per_request_cost
      }
      return {
        model: m.name,
        pricing_tier: m.pricing_tier,
        pricing,
        series: m.series || '',
        capabilities: m.capabilities || [],
      }
    })
    const providerData = {
      key: form.value.key,
      base_url: form.value.base_url,
      api_key: form.value.api_key,
      protocol: form.value.protocol,
      version: form.value.version,
      offers: offers,
    } as any

    if (isEdit.value && props.editingKey) {
      await UpdateProvider(props.editingKey, providerData)
    } else {
      await AddProvider(providerData)
    }
    // Reset local state so re-open triggers fresh onMounted
    isEdit.value = false
    emit('saved', { ...form.value })
  } catch (e: any) {
    error.value = e.message || String(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="modal-overlay" @click.self="emit('cancel')">
    <div class="modal modal-lg">
      <div class="modal-header">
        <h2>{{ isEdit ? '编辑服务' : (preset ? '添加服务' : '自定义服务') }}</h2>
        <button class="close-btn" @click="emit('cancel')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
      <div class="modal-body">
        <div v-if="error" class="error-msg">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
          {{ error }}
        </div>

        <!-- Basic fields -->
        <div class="form-group">
          <label>服务标识</label>
          <input v-model="form.key" :disabled="isEdit" placeholder="例如: deepseek, qwen" />
        </div>

        <div class="form-group">
          <label>API 地址</label>
          <input v-model="form.base_url" placeholder="https://api.deepseek.com/anthropic" />
        </div>

        <div class="form-group">
          <label>API 密钥</label>
          <div class="key-input-wrap">
            <input
              :type="showKey ? 'text' : 'password'"
              v-model="form.api_key"
              placeholder="sk-..."
            />
            <button class="toggle-key" @click="showKey = !showKey">
              <svg v-if="!showKey" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>
              </svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/>
              </svg>
            </button>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label>协议</label>
            <select v-model="form.protocol">
              <option value="anthropic">Anthropic</option>
              <option value="openai-chat">OpenAI Chat</option>
              <option value="openai-response">OpenAI Responses</option>
              <option value="google-genai">Google GenAI</option>
            </select>
          </div>
        </div>

        <!-- Model list with enhanced rows -->
        <div class="form-group">
          <label>模型列表</label>
          <div v-for="(m, idx) in form.models" :key="idx" class="model-entry">
            <div class="model-entry-header">
              <span class="model-entry-num">#{{ idx + 1 }}</span>
              <input v-model="m.name" class="model-entry-name" placeholder="模型名称" />
              <input v-model="m.series" class="model-entry-series" placeholder="系列" />
            </div>
            <div class="model-entry-body">
              <div class="body-row">
                <span class="body-label">能力</span>
                <div class="cap-toggles">
                  <button v-for="cap in ALL_CAPS" :key="cap"
                    :class="['cap-toggle', { active: m.capabilities.includes(cap) }]"
                    @click="toggleCap(m, cap)">
                    {{ capLabels[cap] }}
                  </button>
                </div>
              </div>
              <div class="body-row">
                <span class="body-label">计费</span>
                <select v-model="m.billing_mode" class="billing-select">
                  <option value="token">按 Token</option>
                  <option value="per_request">按请求</option>
                  <option value="free">免费</option>
                </select>
                <span class="body-label">档位</span>
                <select v-model="m.pricing_tier" class="tier-select">
                  <option value="free">免费</option>
                  <option value="economy">经济</option>
                  <option value="standard">标准</option>
                  <option value="premium">高级</option>
                  <option value="ultra">旗舰</option>
                </select>
              </div>
              <div v-if="m.billing_mode === 'token'" class="body-row pricing-row">
                <label>输入 <input type="number" v-model.number="m.input" step="0.1" min="0" /></label>
                <label>输出 <input type="number" v-model.number="m.output" step="0.1" min="0" /></label>
                <label>缓存写 <input type="number" v-model.number="m.cache_write" step="0.1" min="0" /></label>
                <label>缓存读 <input type="number" v-model.number="m.cache_read" step="0.1" min="0" /></label>
              </div>
              <div v-if="m.billing_mode === 'per_request'" class="body-row pricing-row">
                <label>每次请求费用 (¥) <input type="number" v-model.number="m.per_request_cost" step="0.0001" min="0" /></label>
              </div>
            </div>
            <button class="remove-btn" @click="removeModel(idx)" :disabled="form.models.length === 1" title="删除模型">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
              </svg>
            </button>
          </div>
          <button class="add-model-btn" @click="addModel">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            添加模型
          </button>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn-cancel" @click="emit('cancel')">取消</button>
        <button class="btn-save" :disabled="saving" @click="handleSave">
          {{ saving ? '保存中...' : '保存' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  animation: fadeIn 0.15s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.modal {
  background: white;
  border-radius: 16px;
  width: 480px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 20px 60px rgba(0,0,0,0.2);
  animation: slideUp 0.2s ease;
}

.modal-lg {
  width: 560px;
}

@keyframes slideUp {
  from { transform: translateY(20px); opacity: 0; }
  to { transform: translateY(0); opacity: 1; }
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--gray-100);
}

.modal-header h2 {
  font-size: 16px;
  font-weight: 600;
  color: var(--gray-900);
}

.close-btn {
  background: none;
  border: none;
  color: var(--gray-400);
  cursor: pointer;
  padding: 4px;
  border-radius: 6px;
  transition: all 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.close-btn svg {
  width: 20px;
  height: 20px;
}

.close-btn:hover {
  color: var(--gray-600);
  background: var(--gray-100);
}

.modal-body {
  padding: 20px 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  overflow-y: auto;
}

.error-msg {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--red-100);
  color: #DC2626;
  padding: 10px 14px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 500;
}

.error-msg svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-group label {
  font-size: 12px;
  color: var(--gray-500);
  font-weight: 500;
}

.form-group input,
.form-group select {
  width: 100%;
  background: var(--gray-50);
  border: 1px solid var(--gray-200);
  color: var(--gray-800);
  padding: 8px 10px;
  border-radius: 8px;
  font-size: 13px;
  transition: all 0.15s;
}

.form-group input:focus,
.form-group select:focus {
  outline: none;
  border-color: var(--purple-500);
  box-shadow: 0 0 0 3px var(--purple-100);
  background: white;
}

.form-group input:disabled {
  background: var(--gray-100);
  color: var(--gray-400);
}

.key-input-wrap {
  display: flex;
  gap: 6px;
}

.key-input-wrap input {
  flex: 1;
}

.toggle-key {
  background: var(--gray-50);
  border: 1px solid var(--gray-200);
  color: var(--gray-400);
  padding: 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.toggle-key svg {
  width: 18px;
  height: 18px;
}

.toggle-key:hover {
  color: var(--gray-600);
  border-color: var(--gray-300);
}

/* Model entries */
.model-entry {
  border: 1px solid var(--gray-200);
  border-radius: var(--radius);
  padding: 10px 12px;
  margin-bottom: 8px;
  background: var(--gray-50);
  position: relative;
}

.model-entry-header {
  display: flex;
  align-items: center;
  gap: 6px;
}

.model-entry-num {
  font-size: 12px;
  color: var(--gray-400);
  font-weight: 600;
  width: 20px;
  flex-shrink: 0;
}

.model-entry-name {
  flex: 1;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 13px;
  font-weight: 500;
  min-width: 0;
}

.model-entry-name:focus {
  outline: none;
  border-color: var(--purple-500);
  box-shadow: 0 0 0 2px var(--purple-100);
}

.model-entry-series {
  width: 70px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 6px;
  padding: 6px 8px;
  font-size: 12px;
  flex-shrink: 0;
}

.model-entry-series:focus {
  outline: none;
  border-color: var(--purple-500);
}

.model-entry-body {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.body-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.body-label {
  font-size: 11px;
  color: var(--gray-400);
  font-weight: 500;
  flex-shrink: 0;
  width: 36px;
}

/* Capability toggles */
.cap-toggles {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.cap-toggle {
  padding: 3px 10px;
  border-radius: 12px;
  border: 1px solid var(--gray-200);
  background: white;
  color: var(--gray-400);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.cap-toggle:hover {
  border-color: var(--gray-300);
  color: var(--gray-600);
}

.cap-toggle.active {
  border-color: var(--purple-500);
  color: var(--purple-600);
  background: var(--purple-50);
}

/* Billing select */
.billing-select {
  width: 90px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
}

.tier-select {
  width: 70px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
}

/* Pricing inputs */
.pricing-row label {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--gray-500);
  flex: 1;
}

.pricing-row input {
  width: 60px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 4px;
  padding: 4px 6px;
  font-size: 12px;
}

.pricing-row input:focus {
  outline: none;
  border-color: var(--purple-500);
}

.remove-btn {
  position: absolute;
  bottom: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  background: var(--gray-50);
  border: 1px solid var(--gray-200);
  color: var(--gray-400);
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  padding: 0;
}

.remove-btn:hover:not(:disabled) {
  border-color: var(--red-500);
  color: var(--red-500);
  background: var(--red-50);
}

.remove-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.remove-btn svg {
  width: 14px;
  height: 14px;
}

.add-model-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  background: white;
  border: 1px dashed var(--gray-400);
  color: var(--gray-600);
  padding: 8px 12px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  transition: all 0.15s;
}

.add-model-btn svg {
  width: 14px;
  height: 14px;
}

.add-model-btn:hover {
  border-color: var(--purple-500);
  color: var(--purple-500);
  background: var(--purple-50);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 14px 20px;
  border-top: 1px solid var(--gray-100);
}

.btn-cancel {
  background: transparent;
  border: 1px solid var(--gray-200);
  color: var(--gray-500);
  padding: 8px 18px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.15s;
}

.btn-cancel:hover {
  border-color: var(--gray-300);
  color: var(--gray-700);
  background: var(--gray-50);
}

.btn-save {
  background: var(--purple-500);
  border: none;
  color: white;
  padding: 8px 24px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: all 0.15s;
}

.btn-save:hover:not(:disabled) {
  background: var(--purple-600);
  box-shadow: 0 4px 12px rgba(124, 58, 237, 0.3);
}

.btn-save:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
