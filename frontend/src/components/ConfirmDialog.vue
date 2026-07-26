<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { AlertTriangle, HelpCircle } from 'lucide-vue-next'
import { useConfirmHost } from '../composables/useConfirm'

/**
 * 全局确认弹窗宿主，挂载于 App.vue，配合 confirmAction() 使用。
 * 复用 .modal-mask / .modal / .modal-actions 设计语言，仅补充确认弹窗特有的排版。
 */

const { request, accept, cancel } = useConfirmHost()

const dialog = ref<HTMLElement | null>(null)
const confirmButton = ref<HTMLButtonElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)

/** 打开弹窗前的焦点元素，关闭后需要还原。 */
let previouslyFocused: HTMLElement | null = null

const isDanger = computed(() => request.value?.tone === 'danger')
const titleId = computed(() => `confirm-dialog-title-${request.value?.id ?? 0}`)
const descriptionId = computed(() => `confirm-dialog-desc-${request.value?.id ?? 0}`)
const hasDescription = computed(() => Boolean(request.value?.message || request.value?.detail))

watch(() => request.value, async (current, previous) => {
  if (current) {
    // 仅在「从无到有」时记录触发元素；弹窗被新请求替换时保留最初的触发元素。
    if (!previous) previouslyFocused = document.activeElement as HTMLElement | null
    await nextTick()
    // 危险操作默认聚焦「取消」，避免误触回车直接删除；普通确认默认聚焦确认按钮。
    const initial = isDanger.value ? cancelButton.value : confirmButton.value
    initial?.focus()
    return
  }
  if (previous) {
    const target = previouslyFocused
    previouslyFocused = null
    await nextTick()
    if (target?.isConnected) target.focus()
  }
})

/** 焦点陷阱：Tab / Shift+Tab 在弹窗内循环。 */
function trapFocus(event: KeyboardEvent) {
  const root = dialog.value
  if (!root) return
  const focusable = Array.from(
    root.querySelectorAll<HTMLElement>('button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'),
  ).filter((element) => element.offsetParent !== null || element === document.activeElement)
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement as HTMLElement | null
  if (event.shiftKey && (active === first || !root.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (active === last || !root.contains(active))) {
    event.preventDefault()
    first.focus()
  }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Tab') {
    trapFocus(event)
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    cancel()
    return
  }
  if (event.key === 'Enter') {
    // 焦点在按钮上时交给按钮自身的默认行为，避免「聚焦取消却触发确认」。
    if ((event.target as HTMLElement | null)?.tagName === 'BUTTON') return
    event.preventDefault()
    accept()
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="request" class="modal-mask confirm-mask" @click.self="cancel">
      <div
        :key="request.id"
        ref="dialog"
        class="modal confirm-modal"
        :class="{ 'confirm-modal-danger': isDanger }"
        role="alertdialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="hasDescription ? descriptionId : undefined"
        @keydown="onKeydown"
      >
        <div class="confirm-head">
          <span class="confirm-icon" aria-hidden="true">
            <AlertTriangle v-if="isDanger" :size="20" />
            <HelpCircle v-else :size="20" />
          </span>
          <h3 :id="titleId" class="confirm-title">{{ request.title }}</h3>
        </div>
        <div v-if="hasDescription" :id="descriptionId" class="confirm-body">
          <p v-if="request.message" class="confirm-message">{{ request.message }}</p>
          <p v-if="request.detail" class="confirm-detail">{{ request.detail }}</p>
        </div>
        <div class="modal-actions">
          <button ref="cancelButton" type="button" class="btn" @click="cancel">{{ request.cancelText || '取消' }}</button>
          <button
            ref="confirmButton"
            type="button"
            class="btn"
            :class="isDanger ? 'btn-danger-solid' : 'btn-primary'"
            @click="accept"
          >{{ request.confirmText || '确定' }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
