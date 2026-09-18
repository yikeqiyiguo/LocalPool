import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时将 /api /agent 代理到 Go Server；构建产物输出到 Go 内嵌目录 internal/webui/dist
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/agent': { target: 'http://127.0.0.1:8080', changeOrigin: true }
    }
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true
  }
})
