// 前端入口：注册 Vue / Element Plus / Pinia，挂载到 #app
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import App from './App.vue'
import './styles/main.scss'

// Monaco Editor 完全本地化：
// - 从 monaco-editor 根入口引入，Vite 自动把 90+ 种语言拆成独立 chunk 懒加载，主 bundle 约 1.3MB
// - 通过 loader.config({ monaco }) 注入本地实例，跳过 @monaco-editor/loader 的 CDN 加载
import * as monaco from 'monaco-editor'
import { loader } from '@guolao/vue-monaco-editor'
loader.config({ monaco })

const app = createApp(App)
app.use(createPinia())
app.use(ElementPlus)
app.mount('#app')
