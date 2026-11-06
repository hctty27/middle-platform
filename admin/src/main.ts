// 管理平台入口：初始化 Vue 应用并注册 Element Plus 组件库。
import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import './style.css'

createApp(App).use(ElementPlus).mount('#app')
