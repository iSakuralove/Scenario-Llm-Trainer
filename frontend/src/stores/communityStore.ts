import { create } from 'zustand'
import { api } from '../api/client'
import type { CommunityPost, ScenarioContent } from '../types'

type PreviewStepStatus = 'active' | 'done'
type CommunityView = 'instructor_reviewed' | 'instructor_rejected'

export type CommunityFilter =
  | { kind: 'status'; value: string; label: string }
  | { kind: 'view'; value: CommunityView; label: string }
  | { kind: 'all'; value: ''; label: string }

export interface StructuredDraft {
  rootCause: string
  keyEvidence: string
  standardProcedure: string
  architectureDiagram: string
  referenceLinks: string
}

export interface PostDraft {
  title: string
  rawContent: string
  domain: string
  tags: string
}

interface CommunityStoreState {
  posts: CommunityPost[]
  overviewPosts: CommunityPost[]
  title: string
  rawContent: string
  reviewNotes: Record<string, string>
  structuredDrafts: Record<string, StructuredDraft>
  postDrafts: Record<string, PostDraft>
  isPublishing: boolean
  reviewingId: string
  savingDraftId: string
  deletingId: string
  error: string
  previewStatus: string
  previewStream: string
  previewSteps: Array<{ step: string; label: string; status: PreviewStepStatus }>
  isHydratingPosts: boolean
  hydratePosts: (token: string, filterQuery: string) => Promise<void>
  hydrateOverview: (token: string) => Promise<void>
  publishPreview: (token: string, filter: CommunityFilter, userId?: string) => Promise<void>
  instructorReview: (token: string, post: CommunityPost, decision: 'approve' | 'reject', filter: CommunityFilter, userId?: string) => Promise<void>
  finalReview: (token: string, post: CommunityPost, decision: 'publish' | 'reject', filter: CommunityFilter, userId?: string) => Promise<void>
  saveDraft: (token: string, post: CommunityPost, filter: CommunityFilter, userId?: string) => Promise<void>
  submitDraft: (token: string, post: CommunityPost, filter: CommunityFilter, userId?: string) => Promise<void>
  deletePost: (token: string, post: CommunityPost) => Promise<void>
  updateReviewNote: (postId: string, value: string) => void
  updateStructuredDraft: (post: CommunityPost, patch: Partial<StructuredDraft>) => void
  updatePostDraft: (post: CommunityPost, patch: Partial<PostDraft>) => void
  setComposerField: (field: 'title' | 'rawContent', value: string) => void
  clearError: () => void
  clear: () => void
}

const defaultTitle = '真实故障案例：缓存命中率异常下降'
const defaultRawContent = '发布后缓存 key 规则发生变化，命中率下降，数据库读请求升高。'

export const useCommunityStore = create<CommunityStoreState>((set, get) => ({
  posts: [],
  overviewPosts: [],
  title: defaultTitle,
  rawContent: defaultRawContent,
  reviewNotes: {},
  structuredDrafts: {},
  postDrafts: {},
  isPublishing: false,
  reviewingId: '',
  savingDraftId: '',
  deletingId: '',
  error: '',
  previewStatus: '',
  previewStream: '',
  previewSteps: [],
  isHydratingPosts: false,

  hydratePosts: async (token, filterQuery) => {
    set({ isHydratingPosts: true })
    try {
      const res = await api.communityPosts(token, filterQuery)
      set({
        posts: (res.list ?? []).map(normalizeCommunityPost),
        error: '',
        isHydratingPosts: false,
      })
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '读取案例队列失败', isHydratingPosts: false })
      throw err
    }
  },

  hydrateOverview: async (token) => {
    try {
      const res = await api.communityPosts(token, '?scope=all')
      set({
        overviewPosts: (res.list ?? []).map(normalizeCommunityPost),
      })
    } catch {
      // Keep existing overview data when the summary request fails.
    }
  },

  publishPreview: async (token, filter, userId) => {
    const state = get()
    set({
      isPublishing: true,
      error: '',
      previewStatus: '创建案例草稿中',
      previewStream: '',
      previewSteps: [],
    })
    try {
      const post = normalizeCommunityPost(await api.createCommunityPostStream(token, {
        title: state.title,
        raw_content: state.rawContent,
        domain: 'database',
        tags: ['缓存', '变更'],
      }, {
        onStage: (stage) => {
          const label = communityPreviewStageLabel(stage.step, stage.message)
          set((current) => ({
            previewStatus: label,
            previewSteps: appendPreviewStep(current.previewSteps, stage.step || label, label),
          }))
        },
        onDelta: (chunk) => set((current) => ({ previewStream: current.previewStream + chunk })),
      }))

      set((current) => ({
        posts: postMatchesActiveFilter(post, filter, userId) ? [post, ...current.posts] : current.posts,
        overviewPosts: [post, ...current.overviewPosts.filter((item) => item.id !== post.id)],
        previewStatus: '结构化预览已生成',
        isPublishing: false,
        isHydratingPosts: false,
      }))
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : '结构化预览失败',
        previewStatus: '结构化预览失败，请重试',
        isPublishing: false,
      })
      throw err
    }
  },

  instructorReview: async (token, post, decision, filter, userId) => {
    set({ reviewingId: post.id, error: '' })
    try {
      const updated = normalizeCommunityPost(await api.instructorReviewPost(token, post.id, {
        decision,
        note: get().reviewNotes[post.id] ?? '',
        structured_content: decision === 'approve'
          ? draftToScenarioContent(get().structuredDrafts[post.id] ?? structuredDraftFromPost(post), post)
          : undefined,
      }))
      replaceOrRemovePost(set, updated, filter, userId)
      replaceOverviewPost(set, updated)
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '初审失败' })
      throw err
    } finally {
      set({ reviewingId: '' })
    }
  },

  finalReview: async (token, post, decision, filter, userId) => {
    set({ reviewingId: post.id, error: '' })
    try {
      const res = await api.finalReviewPost(token, post.id, {
        decision,
        note: get().reviewNotes[post.id] ?? '',
      })
      replaceOrRemovePost(set, normalizeCommunityPost(res.post), filter, userId)
      replaceOverviewPost(set, normalizeCommunityPost(res.post))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '终审失败' })
      throw err
    } finally {
      set({ reviewingId: '' })
    }
  },

  saveDraft: async (token, post, filter, userId) => {
    set({ savingDraftId: post.id, error: '' })
    try {
      const updated = await persistDraft(token, post, get().structuredDrafts[post.id] ?? structuredDraftFromPost(post), get().postDrafts[post.id] ?? postDraftFromPost(post))
      replaceOrRemovePost(set, updated, filter, userId)
      replaceOverviewPost(set, updated)
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '保存草稿失败' })
      throw err
    } finally {
      set({ savingDraftId: '' })
    }
  },

  submitDraft: async (token, post, filter, userId) => {
    set({ savingDraftId: post.id, error: '' })
    try {
      await persistDraft(token, post, get().structuredDrafts[post.id] ?? structuredDraftFromPost(post), get().postDrafts[post.id] ?? postDraftFromPost(post))
      const updated = normalizeCommunityPost(await api.submitCommunityPost(token, post.id))
      replaceOrRemovePost(set, updated, filter, userId)
      replaceOverviewPost(set, updated)
      set((current) => ({
        structuredDrafts: removeRecordKey(current.structuredDrafts, post.id),
        postDrafts: removeRecordKey(current.postDrafts, post.id),
      }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '提交初审失败' })
      throw err
    } finally {
      set({ savingDraftId: '' })
    }
  },

  deletePost: async (token, post) => {
    set({ deletingId: post.id, error: '' })
    try {
      await api.deleteCommunityPost(token, post.id)
      set((current) => ({
        posts: current.posts.filter((item) => item.id !== post.id),
        overviewPosts: current.overviewPosts.filter((item) => item.id !== post.id),
        structuredDrafts: removeRecordKey(current.structuredDrafts, post.id),
        postDrafts: removeRecordKey(current.postDrafts, post.id),
        reviewNotes: removeRecordKey(current.reviewNotes, post.id),
        error: '',
      }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : '删除案例失败' })
      throw err
    } finally {
      set({ deletingId: '' })
    }
  },

  updateReviewNote: (postId, value) => {
    set((state) => ({
      reviewNotes: { ...state.reviewNotes, [postId]: value },
    }))
  },

  updateStructuredDraft: (post, patch) => {
    set((state) => ({
      structuredDrafts: {
        ...state.structuredDrafts,
        [post.id]: { ...(state.structuredDrafts[post.id] ?? structuredDraftFromPost(post)), ...patch },
      },
    }))
  },

  updatePostDraft: (post, patch) => {
    set((state) => ({
      postDrafts: {
        ...state.postDrafts,
        [post.id]: { ...(state.postDrafts[post.id] ?? postDraftFromPost(post)), ...patch },
      },
    }))
  },

  setComposerField: (field, value) => {
    if (field === 'title') {
      set({ title: value })
      return
    }
    set({ rawContent: value })
  },

  clearError: () => set({ error: '' }),

  clear: () => set({
    posts: [],
    overviewPosts: [],
    title: defaultTitle,
    rawContent: defaultRawContent,
    reviewNotes: {},
    structuredDrafts: {},
    postDrafts: {},
    isPublishing: false,
    reviewingId: '',
    savingDraftId: '',
    deletingId: '',
    error: '',
    previewStatus: '',
    previewStream: '',
    previewSteps: [],
    isHydratingPosts: false,
  }),
}))

function persistDraft(token: string, post: CommunityPost, draft: StructuredDraft, meta: PostDraft) {
  return api.updateCommunityPost(token, post.id, {
    title: meta.title,
    raw_content: meta.rawContent,
    domain: meta.domain,
    tags: normalizeCommunityTags(linesFromText(meta.tags.replaceAll(',', '\n'))),
    structured_content: draftToScenarioContent(draft, post),
  }).then(normalizeCommunityPost)
}

function replaceOrRemovePost(
  set: (partial: Partial<CommunityStoreState> | ((state: CommunityStoreState) => Partial<CommunityStoreState>)) => void,
  updated: CommunityPost,
  filter: CommunityFilter,
  userId?: string,
) {
  set((state) => {
    if (!postMatchesActiveFilter(updated, filter, userId)) {
      return {
        posts: state.posts.filter((post) => post.id !== updated.id),
      }
    }
    return {
      posts: state.posts.map((post) => (post.id === updated.id ? updated : post)),
    }
  })
}

function replaceOverviewPost(
  set: (partial: Partial<CommunityStoreState> | ((state: CommunityStoreState) => Partial<CommunityStoreState>)) => void,
  updated: CommunityPost,
) {
  set((state) => {
    const exists = state.overviewPosts.some((post) => post.id === updated.id)
    return {
      overviewPosts: exists
        ? state.overviewPosts.map((post) => (post.id === updated.id ? updated : post))
        : [updated, ...state.overviewPosts],
    }
  })
}

function removeRecordKey<T>(record: Record<string, T>, key: string) {
  const next = { ...record }
  delete next[key]
  return next
}

function appendPreviewStep(
  steps: Array<{ step: string; label: string; status: PreviewStepStatus }>,
  step: string,
  label: string,
): Array<{ step: string; label: string; status: PreviewStepStatus }> {
  const normalizedStep = step || label
  const next = steps.map((item) => ({ ...item, status: 'done' as PreviewStepStatus }))
  const existing = next.find((item) => item.step === normalizedStep)
  if (existing) {
    existing.label = label
    existing.status = 'active'
    return next
  }
  return [...next, { step: normalizedStep, label, status: 'active' }]
}

function communityPreviewStageLabel(step: string, message: string) {
  const label = communityPreviewStageLabels[step]
  if (label) return label
  if (!message || /[�锟]/.test(message)) return '正在处理案例预览'
  return message
}

const communityPreviewStageLabels: Record<string, string> = {
  received: '已收到案例，正在准备结构化',
  llm: 'AI 正在生成结构化预览',
  schema_validated: '结构化结果已通过 Schema 校验',
  rule_sensitive_check: '规则检测正在检查敏感信息',
  model_sensitive_check: '模型检测正在识别敏感信息',
  fallback_sensitive_check: '模型检测不可用，已使用规则检测兜底',
  sanitized: '敏感字段已脱敏并生成处理建议',
  saving: '正在保存结构化预览',
  completed: '结构化预览已生成',
}

function postMatchesActiveFilter(post: CommunityPost, filter: CommunityFilter, userID?: string) {
  if (filter.kind === 'all') return true
  if (filter.kind === 'status') return post.status === filter.value
  return post.review_history.some((item) => item.actor_id === userID && item.action === communityViewAction(filter.value))
}

function communityViewAction(view: CommunityView) {
  const actions: Record<CommunityView, string> = {
    instructor_reviewed: 'instructor_approve',
    instructor_rejected: 'instructor_reject',
  }
  return actions[view]
}

function normalizeCommunityPost(post: CommunityPost): CommunityPost {
  return {
    ...post,
    tags: normalizeCommunityTags(post.tags ?? []),
    ai_structured_content: normalizeScenarioContent(post.ai_structured_content),
    edited_structured_content: post.edited_structured_content ? normalizeScenarioContent(post.edited_structured_content) : undefined,
    moderation_summary: normalizeModerationSummary(post.moderation_summary),
    sensitive_check: {
      status: post.sensitive_check?.status ?? 'clear',
      sanitized: post.sensitive_check?.sanitized ?? false,
      findings: (post.sensitive_check?.findings ?? []).map(normalizeSensitiveFinding),
      checked_at: post.sensitive_check?.checked_at ?? '',
      source: post.sensitive_check?.source ?? 'rule',
      risk_level: post.sensitive_check?.risk_level ?? inferSensitiveRiskLevel(post.sensitive_check?.findings ?? []),
      blocked: post.sensitive_check?.blocked ?? false,
      fallback_used: post.sensitive_check?.fallback_used ?? false,
      summary: post.sensitive_check?.summary ?? '',
    },
    review_history: (post.review_history ?? []).map((item) => ({
      ...item,
      content: item.content ? normalizeScenarioContent(item.content) : undefined,
    })),
  }
}

type SensitiveFindingList = NonNullable<NonNullable<CommunityPost['sensitive_check']>['findings']>
type SensitiveFindingItem = SensitiveFindingList[number]

function normalizeSensitiveFinding(finding: SensitiveFindingItem) {
  return {
    ...finding,
    source: finding.source ?? 'rule',
    confidence: typeof finding.confidence === 'number' ? finding.confidence : undefined,
    redacted_excerpt: finding.redacted_excerpt || finding.excerpt,
  }
}

function inferSensitiveRiskLevel(findings: SensitiveFindingList) {
  if (findings.some((finding) => finding.severity === 'high')) return 'high'
  if (findings.some((finding) => finding.severity === 'medium')) return 'medium'
  if (findings.length > 0) return 'low'
  return 'none'
}

function normalizeModerationSummary(summary?: CommunityPost['moderation_summary']): CommunityPost['moderation_summary'] {
  if (!summary) return undefined
  return {
    status: summary.status ?? '',
    risk_level: summary.risk_level ?? '',
    recommendation: summary.recommendation ?? '',
    safe_summary: summary.safe_summary ?? '',
    safe_risk_note: summary.safe_risk_note ?? '',
    safe_action_hint: summary.safe_action_hint ?? '',
    safe_labels: summary.safe_labels ?? [],
    suggested_note: summary.suggested_note ?? '',
    reasons: summary.reasons ?? [],
    flagged: summary.flagged ?? false,
  }
}

function normalizeScenarioContent(content?: ScenarioContent): ScenarioContent {
  return {
    root_cause: content?.root_cause ?? '',
    root_cause_keywords: content?.root_cause_keywords ?? [],
    key_evidence: content?.key_evidence ?? [],
    standard_procedure: content?.standard_procedure ?? [],
    reveal_strategy: {
      surface_clues: content?.reveal_strategy?.surface_clues ?? [],
      deep_clues: content?.reveal_strategy?.deep_clues ?? [],
      distractors: content?.reveal_strategy?.distractors ?? [],
    },
    architecture_diagram: content?.architecture_diagram ?? '',
    reference_links: content?.reference_links ?? [],
  }
}

function structuredDraftFromPost(post: CommunityPost): StructuredDraft {
  const content = effectiveStructuredContent(post)
  return {
    rootCause: content.root_cause ?? '',
    keyEvidence: (content.key_evidence ?? []).join('\n'),
    standardProcedure: (content.standard_procedure ?? []).join('\n'),
    architectureDiagram: content.architecture_diagram ?? '',
    referenceLinks: (content.reference_links ?? []).join('\n'),
  }
}

function postDraftFromPost(post: CommunityPost): PostDraft {
  return {
    title: post.title,
    rawContent: post.raw_content,
    domain: post.domain,
    tags: normalizeCommunityTags(post.tags ?? []).join(','),
  }
}

function draftToScenarioContent(draft: StructuredDraft, post: CommunityPost): ScenarioContent {
  const base = effectiveStructuredContent(post)
  return {
    ...base,
    root_cause: draft.rootCause.trim(),
    key_evidence: linesFromText(draft.keyEvidence),
    standard_procedure: linesFromText(draft.standardProcedure),
    architecture_diagram: draft.architectureDiagram.trim(),
    reference_links: linesFromText(draft.referenceLinks),
  }
}

function effectiveStructuredContent(post: CommunityPost): ScenarioContent {
  return post.edited_structured_content ?? post.ai_structured_content
}

function linesFromText(value: string) {
  return value.split('\n').map((item) => item.trim()).filter(Boolean)
}

function normalizeCommunityTags(tags: string[]) {
  const blocked = new Set(['娲剧敓'])
  return Array.from(new Set(
    tags
      .map((tag) => tag.trim())
      .filter((tag) => tag && !blocked.has(tag)),
  ))
}
