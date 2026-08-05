import { onUnmounted, ref } from 'vue'

export interface UsePollingOptions {
  /** Interval between ticks, in milliseconds. */
  intervalMs: number
  /**
   * Extra gate evaluated on every tick, after the visibility check.
   * Return false to skip a tick without stopping the timer.
   */
  shouldRun?: () => boolean
}

/**
 * Repeatedly runs `task` on an interval while the document is visible.
 *
 * Background tabs are skipped rather than polled, and the timer is always
 * cleared on unmount, so callers no longer track timer handles by hand.
 * A tick is skipped while the previous one is still in flight, which keeps a
 * slow request from stacking up overlapping calls.
 */
export function usePolling(task: () => void | Promise<void>, options: UsePollingOptions) {
  const timer = ref<number | null>(null)
  const running = ref(false)
  let inFlight = false

  function stop() {
    if (timer.value !== null) {
      window.clearInterval(timer.value)
      timer.value = null
    }
    running.value = false
  }

  async function tick() {
    if (inFlight || document.visibilityState !== 'visible') return
    if (options.shouldRun && !options.shouldRun()) return
    inFlight = true
    try {
      await task()
    } finally {
      inFlight = false
    }
  }

  function start() {
    stop()
    timer.value = window.setInterval(() => { void tick() }, options.intervalMs)
    running.value = true
  }

  onUnmounted(stop)

  return { start, stop, running }
}
