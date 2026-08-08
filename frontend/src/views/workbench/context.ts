import { inject, provide, type InjectionKey } from 'vue'
import type {
  AIVoice,
  Asset,
  Character,
  Drama,
  Episode,
  GenerationJob,
  GridHistory,
  ProductionRun,
  PromptTemplate,
  Scene,
  Storyboard,
} from '../../api/types'

export type WorkbenchStatus = Record<string, any>
export type WorkbenchConfig = Record<string, any>
export type WorkbenchForm = Record<string, any>
export type WorkbenchStoryboard = Storyboard & { character_ids?: number[] }

export interface WorkbenchContext {
  drama: (Drama & { episodes?: Episode[] }) | null
  episode: Episode | null
  characters: Character[]
  scenes: Scene[]
  storyboards: WorkbenchStoryboard[]
  status: WorkbenchStatus | null
  voices: AIVoice[]
  configs: WorkbenchConfig[]
  promptTemplates: PromptTemplate[]
  gridHist: GridHistory[]
  assets: Asset[]
  jobs: GenerationJob[]
  productions: ProductionRun[]

  rawContent: string
  busy: string
  canEdit: boolean
  gridRows: number
  gridCols: number
  gridMode: string
  gridPrompt: string
  gridImage: string
  gridCells: string[]
  gridCellsVerified: boolean
  gridHistoryId: number | null
  gridAssignments: Array<Record<string, any>>
  gridCellTargets: Record<number, Record<string, any>>
  gridCellErrors: Record<number, string>
  assigningGridCell: number | null
  gridSelectionError: string
  selectedShotIds: number[]
  selectedStoryboardId: number | null
  selectedStoryboard: WorkbenchStoryboard | null
  selectedStoryboardFacts: Array<{ label: string; value: string }>
  assignCharId: number | null
  sceneTransfer: WorkbenchForm | null
  assetTargetShot: Record<number, number>
  assetTargetFrame: Record<number, string>
  pendingJobActionIDs: number[]

  runAgent: (type: string, message: string) => Promise<void>
  saveContent: () => Promise<void>
  selectGridMode: (mode: string) => void
  updateGridDimension: (field: 'rows' | 'cols', raw: number) => void
  buildGridPrompt: () => Promise<boolean>
  openGridPromptEditor: () => void
  generateGrid: () => Promise<void>
  splitGrid: () => Promise<void>
  gridAssignmentLabel: (index: number) => string
  gridCellTarget: (index: number) => Record<string, any>
  updateGridCellTarget: (index: number, patch: Record<string, any>) => void
  assignGridCell: (index: number) => Promise<void>
  loadGridHistory: (history: GridHistory) => void
  toggleShot: (id: number) => void
  batchCharImages: () => Promise<void>
  openCharacterLibraryImport: () => Promise<void>
  editCharacter: (character?: Character) => Promise<void>
  genCharImage: (character: Character) => Promise<void>
  uploadBoundImage: (kind: 'character' | 'scene', item: Character | Scene, event: Event) => Promise<void>
  assignVoice: (character: Character, voiceID: string, provider?: string) => Promise<void>
  voiceSample: (character: Character) => Promise<void>
  saveCharacterToLibrary: (character: Character) => Promise<void>
  removeCharacter: (character: Character) => Promise<void>
  editScene: (scene?: Scene) => Promise<void>
  genSceneImage: (scene: Scene) => Promise<void>
  transferScene: (scene: Scene, mode: 'copy' | 'move') => void
  confirmSceneTransfer: () => Promise<void>
  removeScene: (scene: Scene) => Promise<void>
  addStoryboard: () => Promise<void>
  batchFrames: (frameType?: string) => Promise<void>
  batchVideos: () => Promise<void>
  batchTTS: () => Promise<void>
  genFrame: (storyboard: WorkbenchStoryboard, frameType?: string) => Promise<void>
  genVideo: (storyboard: WorkbenchStoryboard) => Promise<void>
  genTTS: (storyboard: WorkbenchStoryboard) => Promise<void>
  composeShot: (storyboard: WorkbenchStoryboard) => Promise<void>
  editStoryboard: (storyboard: WorkbenchStoryboard) => Promise<void>
  openPromptEditor: (storyboard: WorkbenchStoryboard, field: string, label: string) => void
  moveStoryboard: (storyboard: WorkbenchStoryboard, direction: 'up' | 'down') => Promise<void>
  copyStoryboard: (storyboard: WorkbenchStoryboard) => Promise<void>
  removeStoryboard: (storyboard: WorkbenchStoryboard) => Promise<void>
  storyboardStatusLabel: (status: string) => string
  shotStatusDot: (storyboard: WorkbenchStoryboard) => string
  composeAll: () => Promise<void>
  mergeAll: () => Promise<void>
  applyAsset: (asset: Asset) => Promise<void>
  cancelJob: (job: GenerationJob) => Promise<void>
  retryJob: (job: GenerationJob) => Promise<void>
}

const workbenchContextKey: InjectionKey<WorkbenchContext> = Symbol('flyaimovie-workbench-context')

export function provideWorkbenchContext(context: WorkbenchContext) {
  provide(workbenchContextKey, context)
}

export function useWorkbenchContext() {
  const context = inject(workbenchContextKey)
  if (!context) throw new Error('Workbench context is not available')
  return context
}
