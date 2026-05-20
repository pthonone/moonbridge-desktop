import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import {resolve} from 'path'

export default defineConfig({
  plugins: [
    vue(),
    {
      name: 'remove-crossorigin',
      transformIndexHtml: {
        enforce: 'post',
        transform(html) {
          return html.replaceAll(' crossorigin', '')
        }
      }
    }
  ],
  resolve: {
    alias: {
      '@wailsjs': resolve(__dirname, 'wailsjs')
    }
  },
  build: {
    minify: false
  }
})
