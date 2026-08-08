import { gridAPI } from '../../api'
import { errorMessage } from '../../utils/errorMessage'
import { listOf, type GridCellAssignment } from './grid'
import { gridFrameLabel } from './labels'

export function createWorkbenchGridActions(deps: Record<string, any>) {
  const { busy, gridPrompt, gridRows, gridCols, gridMode, episode, dramaId, gridSelectionError, selectedShotIds, resetGridOutput, getGridContextVersion, gridImage, gridHistoryId, gridCells, gridCellsVerified, gridCellErrors, setGridAssignments, gridCellTarget, assigningGridCell, storyboards, show, refreshAssets } = deps

async function buildGridPrompt(): Promise<boolean> {
  busy.value = 'grid-prompt'
  try {
    const res = await gridAPI.prompt({
      rows: gridRows.value,
      cols: gridCols.value,
      mode: gridMode.value,
      episode_id: episode.value.id,
      drama_id: dramaId.value,
    })
    gridPrompt.value = res.grid_prompt || ''
    show('宫格提示词已生成')
    return Boolean(gridPrompt.value.trim())
  } catch (e: any) {
    show(e.message)
    return false
  } finally {
    busy.value = ''
  }
}

async function generateGrid() {
  if (gridSelectionError.value) {
    show(gridSelectionError.value)
    return
  }
  if (!gridPrompt.value.trim()) {
    if (!await buildGridPrompt() || !gridPrompt.value.trim()) return
  }
  busy.value = 'grid-gen'
  try {
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    resetGridOutput()
    busy.value = 'grid-gen'
    const contextVersion = getGridContextVersion()
    const res = await gridAPI.generate({
      prompt: gridPrompt.value,
      drama_id: dramaId.value,
      episode_id: episode.value.id,
      mode: gridMode.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
    })
    if (getGridContextVersion() !== contextVersion) return
    const img = res.image || res
    const hist = res.history
    gridImage.value = img.image_url || ''
    gridHistoryId.value = hist?.id || null
    if (img.id && !gridImage.value) {
      // poll image status
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000))
        if (getGridContextVersion() !== contextVersion) return
        const st = await gridAPI.status(img.id)
        if (getGridContextVersion() !== contextVersion) return
        if (st.image_url) {
          gridImage.value = st.image_url
          break
        }
        if (st.status === 'failed') throw new Error(st.error_msg || 'grid failed')
      }
      if (!gridImage.value) throw new Error('宫格图生成超时，请稍后在历史记录中查看任务结果')
    }
    if (!gridImage.value) throw new Error('生成服务未返回可用的宫格图')
    show('宫格图生成完成')
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function splitGrid() {
  if (!gridImage.value) {
    show('请先生成宫格图')
    return
  }
  if (gridSelectionError.value) {
    show(gridSelectionError.value)
    return
  }
  busy.value = 'grid-split'
  try {
    const contextVersion = getGridContextVersion()
    const historyID = gridHistoryId.value
    const gridCapacity = gridRows.value * gridCols.value
    const shotCapacity = gridMode.value === 'first_last' ? Math.floor(gridCapacity / 2) : gridCapacity
    const selected = selectedShotIds.value.slice(0, shotCapacity)
    const storyboardIds = gridMode.value === 'first_last' ? [...selected, ...selected] : selected
    const res = await gridAPI.split({
      image_url: gridImage.value,
      rows: gridRows.value,
      cols: gridCols.value,
      storyboard_ids: storyboardIds,
      frame_type: gridMode.value === 'first_last' ? 'first_last' : 'first_frame',
      history_id: gridHistoryId.value || undefined,
    })
    if (getGridContextVersion() !== contextVersion || gridHistoryId.value !== historyID) return
    gridCells.value = res.cells || []
    gridCellsVerified.value = res.cells_verified === true
    gridCellErrors.value = {}
    setGridAssignments(listOf(res.assignments) as GridCellAssignment[])
    show(`已切分 ${res.count} 格并写回分镜`)
    await refreshAssets()
  } catch (e: any) {
    show(e.message)
  } finally {
    busy.value = ''
  }
}

async function assignGridCell(index: number) {
  const target = gridCellTarget(index)
  if (!gridHistoryId.value) {
    show('请先载入或切分已保存的宫格')
    return
  }
  if (!target.storyboard_id) {
    show('请选择目标镜头')
    return
  }
  const historyID = gridHistoryId.value
  const contextVersion = getGridContextVersion()
  assigningGridCell.value = index
  gridCellErrors.value = { ...gridCellErrors.value, [index]: '' }
  try {
    const result = await gridAPI.assignCell(historyID, {
      cell_index: index,
      storyboard_id: target.storyboard_id,
      frame_type: target.frame_type,
    })
    if (getGridContextVersion() !== contextVersion || gridHistoryId.value !== historyID) return
    setGridAssignments(listOf(result.assignments).length ? result.assignments : [result])
    const storyboard = storyboards.value.find((item: any) => item.id === target.storyboard_id)
    show(`切片 ${index + 1} 已写入镜头 #${storyboard?.storyboard_number || target.storyboard_id} ${gridFrameLabel(target.frame_type)}`)
    await refreshAssets()
  } catch (error) {
    if (getGridContextVersion() !== contextVersion || gridHistoryId.value !== historyID) return
    const message = errorMessage(error, '切片分配失败')
    gridCellErrors.value = { ...gridCellErrors.value, [index]: message }
  } finally {
    if (getGridContextVersion() === contextVersion && gridHistoryId.value === historyID) assigningGridCell.value = null
  }
}

  return { buildGridPrompt, generateGrid, splitGrid, assignGridCell }
}
