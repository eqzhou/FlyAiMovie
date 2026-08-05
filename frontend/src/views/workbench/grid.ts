// Grid-composer types and pure helpers used by the workbench grid stage.
//
// Everything here is free of component state so it can be unit-tested and
// reused without pulling in WorkbenchView's refs.

export type GridCellAssignment = {
  cell_index: number
  storyboard_id: number
  frame_type: string
}

export type GridCellTarget = {
  storyboard_id: number
  frame_type: string
}

/** Keeps only truthy entries of a value that should already be an array. */
export function listOf(value: unknown): any[] {
  return Array.isArray(value) ? value.filter(Boolean) : []
}

/**
 * Reads a list that the API may return either as an array or as a JSON string.
 * Malformed JSON yields an empty list rather than throwing, because these
 * fields are display inputs on an already-rendered page.
 */
export function parseJSONList<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value.filter(Boolean) as T[]
  if (typeof value !== 'string' || !value.trim()) return []
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? parsed.filter(Boolean) as T[] : []
  } catch {
    return []
  }
}

/**
 * Derives cell-to-storyboard assignments for a grid that has no stored ones.
 *
 * In first_last mode the storyboard list covers each shot twice, so the first
 * half of the cells map to first frames and the rest to last frames.
 */
export function defaultGridAssignments(history: any, cells: string[]): GridCellAssignment[] {
  const storyboardIds = parseJSONList<number>(history.storyboard_ids).map(Number).filter((id) => id > 0)
  if (history.mode === 'first_last') {
    const half = Math.floor(storyboardIds.length / 2)
    if (!half) return []
    return cells.slice(0, storyboardIds.length).map((_, index) => ({
      cell_index: index,
      storyboard_id: storyboardIds[index % half],
      frame_type: index >= Math.floor(cells.length / 2) ? 'last_frame' : 'first_frame',
    }))
  }
  return cells.slice(0, storyboardIds.length).map((_, index) => ({
    cell_index: index,
    storyboard_id: storyboardIds[index],
    frame_type: 'first_frame',
  }))
}
