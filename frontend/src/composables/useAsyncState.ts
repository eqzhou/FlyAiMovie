import { ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

/**
 * Runs async work with shared loading/error state. The request token prevents
 * an older request from clearing a newer request's shared state; callers that
 * mutate domain data still own their result-level stale-response guard.
 */
export function useAsyncState() {
  const loading = ref(false)
  const error = ref('')
  let requestID = 0

  async function run<T>(task: () => Promise<T>, fallback = '操作失败'): Promise<T | undefined> {
    const request = ++requestID
    loading.value = true
    error.value = ''
    try {
      return await task()
    } catch (reason) {
      if (request === requestID) error.value = errorMessage(reason, fallback)
      return undefined
    } finally {
      if (request === requestID) loading.value = false
    }
  }

  function reset() {
    requestID += 1
    loading.value = false
    error.value = ''
  }

  return { loading, error, run, reset }
}
