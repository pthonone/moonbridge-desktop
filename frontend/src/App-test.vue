<script lang="ts" setup>
import { ref, onMounted } from 'vue'
const mounted = ref(false)
onMounted(() => {
  mounted.value = true
})
</script>

<template>
  <div style="padding:40px;font-family:sans-serif;background:#F9FAFB;min-height:100vh">
    <h1 style="color:#1F2937">测试页面</h1>
    <p v-if="mounted" style="color:#22C55E;font-weight:bold">Vue 已挂载成功！</p>
    <p v-else style="color:#EF4444;font-weight:bold">Vue 未挂载</p>
    <p style="color:#6B7280">如果能看到这段文字，说明前端渲染正常。</p>
    <p style="color:#6B7280">如果看不到，说明是 CSS 或 HTML 加载问题。</p>
  </div>
</template>
