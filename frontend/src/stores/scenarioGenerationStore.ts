import { create } from 'zustand'
import { api } from '../api/client'
import type { ScenarioGenerationPayload, ScenarioGenerationJobResponse } from '../api/client'
import type { AIJob, CommunityPost, ScenarioGenerationConstraints, ScenarioQuestion } from '../types'
import { useAIStatusStore } from './aiStatusStore'

const SCENARIO_GENERATION_STORAGE_KEY = 'scenario-generation-active-job'
const SCENARIO_GENERATION_DRAFT_KEY = 'scenario-generation-draft'
const SCENARIO_GENERATION_POLL_MS = 900

interface StoredScenarioGenerationJob {
  jobId: string
  startedAt: number
}

export interface ScenarioGenerationDraft {
  domain: string
  difficulty: string
  scenarioType: string
  title: string
  description: string
  topicScope: string
  rootCauseHint: string
  evidenceHints: string
  clueHints: string
  advancedOpen: boolean
}

interface GenerationSummary {
  title: string
  provider: string
  fallbackUsed: boolean
}

interface GenerationFailureSummary {
  message: string
  provider: string
  model: string
  jobId: string
  stage: string
  fallbackUsed: boolean
}

class ScenarioGenerationJobFailedError extends Error {
  job: AIJob

  constructor(job: AIJob) {
    super(job.error_message || '题目生成失败')
    this.name = 'ScenarioGenerationJobFailedError'
    this.job = job
  }
}

interface ResumeCompletionContext {
  question: ScenarioQuestion
  job: AIJob
}

interface ResumeHandlers {
  onCompleted?: (context: ResumeCompletionContext) => Promise<void> | void
}

interface ScenarioListFilters {
  selectedDomain: string
  selectedDifficulty: string
  tagFilter: string
  page: number
}

interface ScenarioGenerationState {
  items: ScenarioQuestion[]
  total: number
  filters: ScenarioListFilters
  lastForked: CommunityPost | null
  forkingId: string
  isGenerationDialogOpen: boolean
  draft: ScenarioGenerationDraft
  isGenerating: boolean
  generationStartedAt: number | null
  generationElapsed: number
  generationJob: AIJob | null
  lastGenerated: GenerationSummary | null
  lastGenerationFailure: GenerationFailureSummary | null
  lastGenerationCanceled: boolean
  error: string
  formError: string
  attachPage: (handlers?: ResumeHandlers) => void
  detachPage: () => void
  hydrateList: (token: string) => Promise<void>
  refreshList: (token: string, overrides?: Partial<ScenarioListFilters>) => Promise<void>
  setFilters: (patch: Partial<ScenarioListFilters>) => void
  resetFilters: () => void
  openGenerationDialog: (defaults?: { domain?: string; difficulty?: string }) => void
  closeGenerationDialog: () => void
  forkScenario: (token: string, question: ScenarioQuestion) => Promise<void>
  setError: (message: string) => void
  updateDraft: (patch: Partial<ScenarioGenerationDraft> | ((draft: ScenarioGenerationDraft) => ScenarioGenerationDraft)) => void
  syncDraftDefaults: (defaults: { domain?: string; difficulty?: string }) => void
  clearFormError: () => void
  clearFeedback: () => void
  clear: () => void
  startGeneration: (token: string, fallbackDomain: string, fallbackDifficulty: string) => boolean
  resumeActiveJob: (token: string) => Promise<void>
  cancelGeneration: (token: string) => Promise<void>
}

const defaultGenerationDraft: ScenarioGenerationDraft = {
  domain: 'database',
  difficulty: 'L2',
  scenarioType: 'troubleshooting',
  title: '',
  description: '',
  topicScope: '',
  rootCauseHint: '',
  evidenceHints: '',
  clueHints: '',
  advancedOpen: false,
}

let activePollingJobId = ''
let attachedHandlers: ResumeHandlers | null = null
let pageAttached = false
let elapsedTimer: number | null = null
const defaultFilters: ScenarioListFilters = {
  selectedDomain: '',
  selectedDifficulty: '',
  tagFilter: '',
  page: 1,
}

function emptyFeedbackState() {
  return {
    isGenerating: false,
    generationStartedAt: null as number | null,
    generationElapsed: 0,
    generationJob: null as AIJob | null,
    lastGenerated: null as GenerationSummary | null,
    lastGenerationFailure: null as GenerationFailureSummary | null,
    lastGenerationCanceled: false,
    error: '',
    formError: '',
  }
}

export const useScenarioGenerationStore = create<ScenarioGenerationState>((set, get) => ({
  items: [],
  total: 0,
  filters: defaultFilters,
  lastForked: null,
  forkingId: '',
  isGenerationDialogOpen: false,
  draft: readGenerationDraft(),
  ...emptyFeedbackState(),

  attachPage: (handlers) => {
    pageAttached = true
    attachedHandlers = handlers ?? null
  },

  detachPage: () => {
    pageAttached = false
    attachedHandlers = null
    stopElapsedTimer()
    activePollingJobId = ''
  },

  hydrateList: async (token) => {
    await get().refreshList(token)
  },

  refreshList: async (token, overrides = {}) => {
    const state = get()
    const nextFilters = { ...state.filters, ...overrides }
    const res = await api.scenarios(token, {
      domain: nextFilters.selectedDomain,
      difficulty: nextFilters.selectedDifficulty,
      tag: nextFilters.tagFilter.trim(),
      page: nextFilters.page,
      page_size: 9,
    })
    const activeItems = (res.list ?? []).filter((item) => item.status === 'active')
    set({
      items: activeItems,
      total: activeItems.length === (res.list ?? []).length ? (res.total ?? 0) : activeItems.length,
      filters: nextFilters,
      error: '',
    })
  },

  setFilters: (patch) => {
    set((state) => ({
      filters: {
        ...state.filters,
        ...patch,
      },
    }))
  },

  resetFilters: () => {
    set({ filters: { ...defaultFilters } })
  },

  openGenerationDialog: (defaults) => {
    get().clearFormError()
    get().syncDraftDefaults({
      domain: defaults?.domain || get().filters.selectedDomain || defaultGenerationDraft.domain,
      difficulty: defaults?.difficulty || get().filters.selectedDifficulty || defaultGenerationDraft.difficulty,
    })
    set({ isGenerationDialogOpen: true })
  },

  closeGenerationDialog: () => {
    get().clearFormError()
    set({ isGenerationDialogOpen: false })
  },

  forkScenario: async (token, question) => {
    set({
      forkingId: question.id,
      lastForked: null,
      error: '',
    })
    try {
      const forked = await api.forkScenario(token, question.id)
      set({
        lastForked: forked,
        forkingId: '',
      })
    } catch (err) {
      set({
        forkingId: '',
        error: err instanceof Error ? err.message : '派生题目失败',
      })
      throw err
    }
  },

  setError: (message) => set({ error: message }),

  updateDraft: (patch) => {
    set((state) => {
      const draft = typeof patch === 'function' ? patch(state.draft) : { ...state.draft, ...patch }
      persistGenerationDraft(draft)
      return { draft }
    })
  },

  syncDraftDefaults: ({ domain, difficulty }) => {
    set((state) => {
      const draft = {
        ...state.draft,
        domain: state.draft.domain || domain || defaultGenerationDraft.domain,
        difficulty: state.draft.difficulty || difficulty || defaultGenerationDraft.difficulty,
      }
      persistGenerationDraft(draft)
      return { draft }
    })
  },

  clearFormError: () => set({ formError: '' }),

  clearFeedback: () => set({ lastGenerated: null, lastGenerationFailure: null, lastGenerationCanceled: false, error: '' }),

  clear: () => set({
    items: [],
    total: 0,
    filters: { ...defaultFilters },
    lastForked: null,
    forkingId: '',
    isGenerationDialogOpen: false,
    draft: readGenerationDraft(),
    ...emptyFeedbackState(),
  }),

  startGeneration: (token, fallbackDomain, fallbackDifficulty) => {
    const payload = buildScenarioGenerationPayload(get().draft, fallbackDomain, fallbackDifficulty)
    const validationError = validateScenarioGenerationDraft(payload)
    if (validationError) {
      set({ formError: validationError })
      return false
    }

    const startedAt = Date.now()
    set({
      isGenerating: true,
      generationStartedAt: startedAt,
      generationElapsed: 0,
      generationJob: null,
      lastGenerated: null,
      lastGenerationFailure: null,
      lastGenerationCanceled: false,
      isGenerationDialogOpen: false,
      error: '',
      formError: '',
    })
    startElapsedTimer()

    void (async () => {
      try {
        const created = await api.startScenarioGenerationJob(token, payload)
        if (!pageAttached) {
          storeGenerationJob({ jobId: created.job.id, startedAt })
          set({ generationJob: created.job })
          return
        }
        set({ generationJob: created.job })
        storeGenerationJob({ jobId: created.job.id, startedAt })
        await resumeScenarioGenerationJob(token, created.job.id, startedAt, created, set)
      } catch (err) {
        stopElapsedTimer()
        const failedJob = err instanceof ScenarioGenerationJobFailedError ? err.job : null
        set({
          isGenerating: false,
          generationStartedAt: null,
          generationJob: failedJob,
          lastGenerationFailure: buildGenerationFailureSummary(failedJob, err),
          error: '',
        })
      }
    })()

    return true
  },

  resumeActiveJob: async (token) => {
    const storedJob = readStoredGenerationJob()
    if (!storedJob) return
    await resumeScenarioGenerationJob(token, storedJob.jobId, storedJob.startedAt, undefined, set)
  },

  cancelGeneration: async (token) => {
    const state = get()
    const jobId = state.generationJob?.id ?? readStoredGenerationJob()?.jobId
    if (!jobId) return

    set({ error: '' })
    try {
      const canceled = await api.cancelAIJob(token, jobId)
      clearStoredGenerationJob(jobId)
      activePollingJobId = ''
      stopElapsedTimer()
      set({
        generationJob: canceled.job,
        isGenerating: false,
        generationStartedAt: null,
        lastGenerated: null,
        lastGenerationFailure: null,
        lastGenerationCanceled: true,
      })
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '停止生成失败' })
      throw err
    }
  },
}))

async function resumeScenarioGenerationJob(
  token: string,
  jobId: string,
  startedAt: number,
  initialResult: ScenarioGenerationJobResponse | undefined,
  set: (partial: Partial<ScenarioGenerationState>) => void,
) {
  if (!jobId || activePollingJobId === jobId) return

  activePollingJobId = jobId
  set({
    isGenerating: true,
    generationStartedAt: startedAt,
    generationElapsed: Math.max(1, Math.floor((Date.now() - startedAt) / 1000)),
    lastGenerated: null,
    lastGenerationFailure: null,
    lastGenerationCanceled: false,
    error: '',
  })
  startElapsedTimer()

  try {
    let jobResult = initialResult ?? await api.aiJob(token, jobId)
    set({ generationJob: jobResult.job })

    while (jobResult.job.status !== 'completed' && jobResult.job.status !== 'failed' && jobResult.job.status !== 'canceled') {
      await sleep(SCENARIO_GENERATION_POLL_MS)
      if (!pageAttached) {
        activePollingJobId = ''
        return
      }
      jobResult = await api.aiJob(token, jobId)
      set({ generationJob: jobResult.job })
    }

    if (jobResult.job.status === 'canceled') {
      clearStoredGenerationJob(jobId)
      stopElapsedTimer()
      set({
        isGenerating: false,
        generationStartedAt: null,
        lastGenerationCanceled: true,
        lastGenerated: null,
        lastGenerationFailure: null,
      })
      return
    }

    if (jobResult.job.status === 'failed') {
      clearStoredGenerationJob(jobId)
      throw new ScenarioGenerationJobFailedError(jobResult.job)
    }

    if (!jobResult.question) {
      clearStoredGenerationJob(jobId)
      throw new Error('棰樼洰鐢熸垚瀹屾垚浣嗘湭杩斿洖棰樼洰')
    }

    const provider = await resolveProvider(jobResult.job)
    await attachedHandlers?.onCompleted?.({ question: jobResult.question, job: jobResult.job })
    await useScenarioGenerationStore.getState().refreshList(token, {
      selectedDomain: jobResult.question.domain,
      selectedDifficulty: jobResult.question.difficulty,
      tagFilter: '',
      page: 1,
    })
    clearStoredGenerationJob(jobId)
    stopElapsedTimer()
    set({
      isGenerating: false,
      generationStartedAt: null,
      lastGenerated: {
        title: jobResult.question.title,
        provider,
        fallbackUsed: jobResult.job.fallback_used,
      },
      lastGenerationFailure: null,
      lastGenerationCanceled: false,
      error: '',
    })
  } catch (err) {
    clearStoredGenerationJob(jobId)
    stopElapsedTimer()
    set({
      isGenerating: false,
      generationStartedAt: null,
      lastGenerationFailure: buildGenerationFailureSummary(useScenarioGenerationStore.getState().generationJob, err),
      error: '',
    })
    throw err
  } finally {
    if (activePollingJobId === jobId) {
      activePollingJobId = ''
    }
  }
}

async function resolveProvider(job: AIJob) {
  if (job.provider) return job.provider
  const aiStatusState = useAIStatusStore.getState()
  const cachedProvider = aiStatusState.status?.provider
  if (cachedProvider) return cachedProvider
  const loaded = await aiStatusState.load()
  return loaded?.provider ?? ''
}

function startElapsedTimer() {
  stopElapsedTimer()
  elapsedTimer = window.setInterval(() => {
    const startedAt = useScenarioGenerationStore.getState().generationStartedAt
    if (!startedAt) return
    useScenarioGenerationStore.setState({
      generationElapsed: Math.max(1, Math.floor((Date.now() - startedAt) / 1000)),
    })
  }, 1000)
}

function stopElapsedTimer() {
  if (elapsedTimer !== null) {
    window.clearInterval(elapsedTimer)
    elapsedTimer = null
  }
}

function buildScenarioGenerationPayload(
  draft: ScenarioGenerationDraft,
  fallbackDomain: string,
  fallbackDifficulty: string,
): ScenarioGenerationPayload {
  const constraints: ScenarioGenerationConstraints = {}
  if (draft.title.trim()) constraints.title = draft.title.trim()
  if (draft.description.trim()) constraints.description = draft.description.trim()
  const topicScope = linesFromText(draft.topicScope)
  if (topicScope.length > 0) constraints.topic_scope = topicScope
  if (draft.rootCauseHint.trim()) constraints.root_cause_hint = draft.rootCauseHint.trim()
  const evidenceHints = linesFromText(draft.evidenceHints)
  if (evidenceHints.length > 0) constraints.evidence_hints = evidenceHints
  const clueHints = linesFromText(draft.clueHints)
  if (clueHints.length > 0) constraints.clue_hints = clueHints

  const payload: ScenarioGenerationPayload = {
    domain: draft.domain || fallbackDomain || defaultGenerationDraft.domain,
    difficulty: draft.difficulty || fallbackDifficulty || defaultGenerationDraft.difficulty,
    scenario_type: draft.scenarioType || defaultGenerationDraft.scenarioType,
  }
  if (Object.keys(constraints).length > 0) {
    payload.constraints = constraints
  }
  return payload
}

function validateScenarioGenerationDraft(payload: ScenarioGenerationPayload) {
  if (!payload.domain) return '请选择领域'
  if (!payload.difficulty) return '请选择生成难度'
  if (!payload.scenario_type) return '请选择题型'
  if (payload.constraints?.title && payload.constraints.title.length > 80) return '标题约束不能超过 80 个字符'
  if (payload.constraints?.description && payload.constraints.description.length > 240) return '描述约束不能超过 240 个字符'
  if ((payload.constraints?.topic_scope?.length ?? 0) > 6) return '细分主题最多 6 项'
  if ((payload.constraints?.evidence_hints?.length ?? 0) > 6) return '证据提示最多 6 项'
  if ((payload.constraints?.clue_hints?.length ?? 0) > 6) return '线索提示最多 6 项'
  return ''
}

function linesFromText(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function storeGenerationJob(job: StoredScenarioGenerationJob) {
  window.localStorage.setItem(SCENARIO_GENERATION_STORAGE_KEY, JSON.stringify(job))
}

function readStoredGenerationJob() {
  const raw = window.localStorage.getItem(SCENARIO_GENERATION_STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as Partial<StoredScenarioGenerationJob>
    if (!parsed.jobId || typeof parsed.startedAt !== 'number') {
      window.localStorage.removeItem(SCENARIO_GENERATION_STORAGE_KEY)
      return null
    }
    return { jobId: parsed.jobId, startedAt: parsed.startedAt }
  } catch {
    window.localStorage.removeItem(SCENARIO_GENERATION_STORAGE_KEY)
    return null
  }
}

function clearStoredGenerationJob(jobId?: string) {
  if (!jobId) {
    window.localStorage.removeItem(SCENARIO_GENERATION_STORAGE_KEY)
    return
  }
  const stored = readStoredGenerationJob()
  if (!stored || stored.jobId === jobId) {
    window.localStorage.removeItem(SCENARIO_GENERATION_STORAGE_KEY)
  }
}

function readGenerationDraft() {
  const raw = window.localStorage.getItem(SCENARIO_GENERATION_DRAFT_KEY)
  if (!raw) return defaultGenerationDraft
  try {
    return { ...defaultGenerationDraft, ...(JSON.parse(raw) as Partial<ScenarioGenerationDraft>) }
  } catch {
    return defaultGenerationDraft
  }
}

function persistGenerationDraft(draft: ScenarioGenerationDraft) {
  window.localStorage.setItem(SCENARIO_GENERATION_DRAFT_KEY, JSON.stringify(draft))
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, ms))
}

function buildGenerationFailureSummary(job: AIJob | null, err: unknown): GenerationFailureSummary {
  const message = err instanceof Error ? err.message : '题目生成失败'
  return {
    message,
    provider: job?.provider ?? '',
    model: job?.model ?? '',
    jobId: job?.id ?? '',
    stage: job?.stage ?? '',
    fallbackUsed: job?.fallback_used ?? false,
  }
}
