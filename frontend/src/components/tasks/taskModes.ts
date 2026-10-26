// 任务模式注册表：任务类型（Syslog / UDP / TCP）× 模式（客户端/服务端）各对应一个独立任务组件。
// 模式在"新建会话"向导中确定，任务组件内不再提供模式切换。
// 后期扩展新任务类型（如 SNMP、HTTP）只需：
//   1. 在 tasks/ 下新建 XxxClientTask.vue / XxxServerTask.vue；
//   2. 在 TASK_MODES 中追加一项。
import type { Component } from 'vue'
// 图标使用 Element Plus 的 SVG 图标组件，而不是 emoji：Linux（麒麟 V10 等）
// 默认未安装彩色 emoji 字体时，emoji 会渲染成方框（豆腐块/破图）。
import { Connection, Document, Promotion } from '@element-plus/icons-vue'
import UdpClientTask from './UdpClientTask.vue'
import UdpServerTask from './UdpServerTask.vue'
import TcpClientTask from './TcpClientTask.vue'
import TcpServerTask from './TcpServerTask.vue'
import SyslogClientTask from './SyslogClientTask.vue'
import SyslogServerTask from './SyslogServerTask.vue'
import type { Protocol, SubMode } from '@/types'

export type TaskModeId = 'udp' | 'tcp' | 'syslog'

export interface TaskModeDef {
  id: TaskModeId
  label: string
  icon: Component
  /** 一句话说明的 i18n key（新建会话向导用） */
  descKey: string
  /** 根据子模式映射到后端协议 */
  protocol: (sub: SubMode) => Protocol
  /** 按模式拆分的独立任务组件 */
  components: { client: Component; server: Component }
}

export const TASK_MODES: TaskModeDef[] = [
  {
    id: 'syslog',
    label: 'Syslog',
    icon: Document,
    descKey: 'newSession.syslogDesc',
    protocol: (sub) => (sub === 'server' ? 'udp_server' : 'udp_client'),
    components: { client: SyslogClientTask, server: SyslogServerTask }
  },
  {
    id: 'udp',
    label: 'UDP',
    icon: Promotion,
    descKey: 'newSession.udpDesc',
    protocol: (sub) => (sub === 'server' ? 'udp_server' : 'udp_client'),
    components: { client: UdpClientTask, server: UdpServerTask }
  },
  {
    id: 'tcp',
    label: 'TCP',
    icon: Connection,
    descKey: 'newSession.tcpDesc',
    protocol: (sub) => (sub === 'server' ? 'tcp_server' : 'tcp_client'),
    components: { client: TcpClientTask, server: TcpServerTask }
  }
]

/** 根据 id 获取任务模式定义 */
export function getTaskMode(id: TaskModeId): TaskModeDef {
  return TASK_MODES.find((m) => m.id === id) ?? TASK_MODES[0]
}

/** 根据任务模式 + 子模式获取对应的独立任务组件 */
export function getTaskComponent(taskMode: TaskModeId, sub: SubMode): Component {
  const def = getTaskMode(taskMode)
  return sub === 'server' ? def.components.server : def.components.client
}

/** 从协议反推任务模式 */
export function taskModeFromProtocol(p: Protocol): TaskModeId {
  if (p.startsWith('tcp_')) return 'tcp'
  if (p.startsWith('udp_')) return 'udp'
  return 'udp'
}

/** 从协议反推子模式 */
export function subModeFromProtocol(p: Protocol): SubMode {
  return p.endsWith('_server') ? 'server' : 'client'
}
