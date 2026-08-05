import { nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export type WorkbenchStage = 'script' | 'cast' | 'grid' | 'boards' | 'export'

export const workbenchStages: WorkbenchStage[] = ['script', 'cast', 'grid', 'boards', 'export']

function isStage(value: unknown): value is WorkbenchStage {
  return workbenchStages.includes(String(value) as WorkbenchStage)
}

/**
 * Owns which workbench stage is active, keeping it in sync with `?stage=`.
 *
 * The URL is the source of truth so a stage survives reload and can be linked
 * to; navigating also moves focus and scroll for keyboard and screen-reader
 * users, honouring prefers-reduced-motion.
 */
export function useWorkbenchStages() {
  const route = useRoute()
  const router = useRouter()

  const active = ref<WorkbenchStage>(isStage(route.query.stage) ? route.query.stage as WorkbenchStage : 'script')

  function select(stage: string) {
    if (!isStage(stage)) return
    active.value = stage
    router.replace({ query: { ...route.query, stage } })
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    window.scrollTo({ top: 0, behavior: reduceMotion ? 'auto' : 'smooth' })
    nextTick(() => document.getElementById(`workbench-stage-${stage}`)?.scrollIntoView({ block: 'nearest', inline: 'center', behavior: reduceMotion ? 'auto' : 'smooth' }))
  }

  /** Roving-tabindex arrow/Home/End handling for the stage tablist. */
  function handleKeydown(event: KeyboardEvent, index: number) {
    let targetIndex = index
    if (event.key === 'ArrowRight') targetIndex = (index + 1) % workbenchStages.length
    else if (event.key === 'ArrowLeft') targetIndex = (index - 1 + workbenchStages.length) % workbenchStages.length
    else if (event.key === 'Home') targetIndex = 0
    else if (event.key === 'End') targetIndex = workbenchStages.length - 1
    else return
    event.preventDefault()
    select(workbenchStages[targetIndex])
    nextTick(() => document.getElementById(`workbench-stage-${workbenchStages[targetIndex]}`)?.focus())
  }

  // Back/forward navigation and external query edits drive the active stage.
  watch(() => route.query.stage, (stage) => {
    const value = String(stage || '') as WorkbenchStage
    if (isStage(value) && value !== active.value) active.value = value
  })

  return { active, select, handleKeydown }
}
