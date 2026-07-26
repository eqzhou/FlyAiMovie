import { ref } from 'vue'

/**
 * 统一确认弹窗：Promise 化的 confirm() 替代品。
 *
 * 用法与原生 confirm() 基本一致，只是需要 await：
 *   if (!await confirmAction({ title: '删除项目', message: '确定删除「X」？', tone: 'danger' })) return
 *
 * 依赖 App.vue 中挂载的全局宿主组件 <ConfirmDialog />。
 */

export type ConfirmTone = 'default' | 'danger'

export interface ConfirmOptions {
  /** 弹窗标题，简述操作，例如「删除场景」。 */
  title: string
  /** 主要说明文案，通常包含被操作对象的名称。 */
  message?: string
  /** 次要说明，用于补充影响范围或不可撤销提示。 */
  detail?: string
  /** 确认按钮文案，默认「确定」。 */
  confirmText?: string
  /** 取消按钮文案，默认「取消」。 */
  cancelText?: string
  /** 语义色：danger 用于删除等破坏性操作，default 用于普通确认。 */
  tone?: ConfirmTone
}

interface ConfirmRequest extends ConfirmOptions {
  /** 每次请求的唯一 id，用于 aria 关联与 :key 强制重建。 */
  id: number
  resolve: (value: boolean) => void
}

let requestSequence = 0
const request = ref<ConfirmRequest | null>(null)

function settle(value: boolean) {
  const pending = request.value
  if (!pending) return
  // 先清空再 resolve，避免 resolve 的同步回调里再次打开弹窗时被这里覆盖。
  request.value = null
  pending.resolve(value)
}

/**
 * 打开确认弹窗，返回用户是否确认。
 * 若已有弹窗处于打开状态，旧弹窗会以「取消」结果关闭，保证 Promise 永远不会悬挂。
 */
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  settle(false)
  requestSequence += 1
  const id = requestSequence
  return new Promise<boolean>((resolve) => {
    request.value = { ...options, id, resolve }
  })
}

/** 供全局宿主组件 <ConfirmDialog /> 使用。 */
export function useConfirmHost() {
  return {
    request,
    accept: () => settle(true),
    cancel: () => settle(false),
  }
}
