import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import monacoEditorPluginModule from 'vite-plugin-monaco-editor'
import wails from '@wailsio/runtime/plugins/vite'
import { fileURLToPath, URL } from 'node:url'

// CJS 包导出需要取 .default
const monacoEditorPlugin = (monacoEditorPluginModule as any).default || monacoEditorPluginModule

// 浏览器 dev 调试用的最小后端 mock：按 methodID 返回可渲染 UI 的数据
// （methodID 来自 frontend/bindings/SuperSender/app.ts，重新生成 bindings 后需同步）
function mockByMethodID(id: number): unknown {
  const mockSession = {
    id: 'mock-session-1',
    name: 'Syslog 客户端',
    protocol: 'udp_client',
    taskMode: 'syslog',
    targetIP: '127.0.0.1',
    targetPort: 514,
    sourceIP: '',
    sourcePort: 0,
    targets: [],
    enableMulti: false,
    broadcast: false,
    payloadText: 'Hello SuperSender',
    filePath: '',
    useFile: false,
    readStrategy: 'sequential',
    templateID: '',
    paramEnabled: false,
    mode: 'interval',
    intervalMS: 1000,
    ratePerSec: 100,
    stopCondition: 'manual',
    stopCount: 0,
    stopDurationSec: 0,
    encoding: 'utf-8',
    sendEncoding: 'utf-8',
    recvEncoding: 'utf-8',
    syslog: { enabled: true, advanced: false, facility: 16, severity: 6, hostname: 'supersender', tag: 'app' },
    tcp: null,
    dialTimeoutMs: 3000,
    autoReconnect: false,
    reconnectIntervalMs: 1000,
    reconnectMax: 0,
    tcpNoDelay: true,
    tcpKeepAlive: true,
    keepAliveIntervalSec: 30,
    maxConnections: 0,
    echoBack: false,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString()
  }
  switch (id) {
    case 242974527: // StartSending
    case 4205933185: // StopSending
    case 3501013174: // UpdateSessionConfig
      return { success: true, message: '' }
    case 827108744: // ListSessions
      return [{ id: mockSession.id, name: mockSession.name, protocol: mockSession.protocol, status: 'idle', target: `${mockSession.targetIP}:${mockSession.targetPort}` }]
    case 2196084171: // GetSessionConfig
    case 2952361431: // CreateSession
      return mockSession
    case 2554697378: // GetSettings
      return { theme: 'light', language: 'zh' }
    case 479751508: // ListTemplates
      return []
    case 3220136254: // FacilityOptions
      return [{ label: 'local0 (16)', value: 16 }]
    case 499961044: // SeverityOptions
      return [{ label: 'info (6)', value: 6 }]
    case 3603263191: // GetRecentLogs
      return []
    case 2317610014: // GetReceiveBuffer
      return []
    case 336822092: // GetNICs
      return [{ name: '全部网卡', ip: '0.0.0.0' }]
    case 1089204084: // GetLocalIPs
      return ['127.0.0.1']
    case 2936437102: // CheckPortInUse
      return false
    case 879106687: // GetPortOccupant
      return ''
    case 3917273495: // GetSnapshot
      return { sessionID: mockSession.id, totalSent: 0, sessionSent: 0, received: 0, ratePerSec: 0, bytesPerSec: 0, durationSec: 0, totalBytesSent: 0, totalBytesRecv: 0, recvRatePerSec: 0, recvBytesPerSec: 0, clientCount: 0 }
    case 2797231138: // BuildSyslog
      return '<134> mock syslog preview'
    case 3057512208: // ReadTextFile
      return 'mock line 001: sample syslog message\nmock line 002: sample syslog message\nmock line 003: sample syslog message'
    default:
      return null
  }
}

// 统一信息配置（Go 与前端共用根目录下的同一份 app.config.json）
const appConfigPath = fileURLToPath(new URL('../app.config.json', import.meta.url))

// Vite 配置：内嵌 webview 友好，资源路径相对，避免 runtime 外链
export default defineConfig({
  plugins: [
    vue(),
    // Monaco Editor 本地化：worker 从 node_modules 打进 bundle，无需联网加载
    monacoEditorPlugin({
      languageWorkers: ['editorWorkerService']
    }),
    // Wails v3 runtime vite 插件：dev 模式暴露 @wailsio/runtime，prod 注入 bindings
    wails('./bindings'),
    // 开发模式下注入 Wails mock，便于在浏览器中直接调试前端布局
    {
      name: 'wails-mock-dev',
      apply: 'serve',
      configureServer(server) {
        // v3 runtime 通过 POST /wails/runtime 发起绑定调用，此处按 methodID 返回 mock 数据
        server.middlewares.use('/wails/runtime', (req, res) => {
          let body = ''
          req.on('data', (c) => (body += c))
          req.on('end', () => {
            let result: unknown = null
            try {
              const parsed = JSON.parse(body || '{}')
              const methodID = parsed?.args?.methodID
              result = mockByMethodID(methodID)
            } catch { /* 忽略解析失败，按 null 返回 */ }
            res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify(result ?? null))
          })
        })
      },
      transformIndexHtml() {
        // 给 @wailsio/runtime 注入 mock，避免浏览器 dev 模式下因无 Wails 容器报错
        return [
          {
            tag: 'script',
            injectTo: 'head',
            children: `
              (function(){
                const rt = window['@wailsio/runtime'] = window['@wailsio/runtime'] || {};
                rt.EventsOn = (name, cb) => { console.info('[mock] EventsOn:', name); return () => {}; };
                rt.EventsEmit = (name, data) => { console.info('[mock] EventsEmit:', name, data); };
                rt.EventsOff = (name) => { console.info('[mock] EventsOff:', name); };
              })();
            `
          }
        ]
      }
    }
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      // 用法：import appConfig from '@app-config'
      '@app-config': appConfigPath
    }
  },
  // 单 exe 打包使用相对路径，避免 Wails 内嵌 webview 找不到资源
  base: './',
  // Sass 使用新版 JS API，消除 legacy-js-api 弃用警告（Dart Sass 2.0 将移除 legacy API）
  css: {
    preprocessorOptions: {
      scss: {
        api: 'modern'
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // 关闭 sourcemap 减小体积；如需调试可改 true
    sourcemap: false,
    target: 'es2020',
    // Monaco 编辑器核心含全部编辑功能，压缩后约 3.9MB（gzip 约 1MB），本地内嵌加载无需网络，
    // 阈值按实际体积设置，其余业务代码已拆分到独立的 element-plus / 主包
    chunkSizeWarningLimit: 4096,
    rollupOptions: {
      output: {
        // 第三方大依赖按包路径分块（函数式避免把公共依赖再打包成环），编辑器与组件库各自独立，利于缓存
        manualChunks(id: string) {
          if (id.includes('node_modules/monaco-editor')) return 'monaco'
          if (id.includes('node_modules/element-plus') || id.includes('node_modules/@element-plus')) return 'element-plus'
        }
      }
    }
  },
  server: {
    // wails3 dev 在 dev 配置里指定端口，此处默认 9245 与模板对齐
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    host: '127.0.0.1'
  },
  optimizeDeps: {
    // 预构建 Element Plus 及其英文语言包，避免 dev 模式下首次切换语言触发依赖重优化并整页 reload
    include: ['element-plus', 'element-plus/es/locale/lang/en', 'vue', 'pinia']
  }
})
