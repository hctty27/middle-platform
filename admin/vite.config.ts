// 本地开发时分别代理配置中心、SDK Core、SDK Service 和健康检查请求。
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  // 使用相对资源路径，同一份构建产物可部署在任意子路径。
  base: './',
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api/service': 'http://127.0.0.1:8221',
      '/api/core': 'http://127.0.0.1:8082',
      '/api': 'http://127.0.0.1:8080',
      '/actuator': 'http://127.0.0.1:8080'
    }
  }
})
