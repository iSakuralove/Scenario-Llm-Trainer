import { useEffect, useState } from 'react'
import { BookOpenCheck, CheckCircle2, ChevronDown, ChevronUp, Edit3, Plus, ShieldAlert, ShieldCheck, Trash2 } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { useAuthStore } from '../../stores/authStore'
import type { CommunityPost, ScenarioContent, SensitiveFinding, User, UserRole } from '../../types'
import { SegmentedOptions } from '../../components/common'
import { useToken } from '../../lib/auth'
import { domainLabel } from '../../lib/domain'
import { type CommunityFilter, useCommunityStore } from '../../stores/communityStore'
import './CommunityPage.css'

type CommunityView = 'instructor_reviewed' | 'instructor_rejected'

export function CommunityPage() {
  const token = useToken()
  const user = useAuthStore((state) => state.user)
  const [searchParams, setSearchParams] = useSearchParams()
  const posts = useCommunityStore((state) => state.posts)
  const overviewPosts = useCommunityStore((state) => state.overviewPosts)
  const title = useCommunityStore((state) => state.title)
  const rawContent = useCommunityStore((state) => state.rawContent)
  const reviewNotes = useCommunityStore((state) => state.reviewNotes)
  const structuredDrafts = useCommunityStore((state) => state.structuredDrafts)
  const postDrafts = useCommunityStore((state) => state.postDrafts)
  const isPublishing = useCommunityStore((state) => state.isPublishing)
  const reviewingId = useCommunityStore((state) => state.reviewingId)
  const savingDraftId = useCommunityStore((state) => state.savingDraftId)
  const deletingId = useCommunityStore((state) => state.deletingId)
  const error = useCommunityStore((state) => state.error)
  const previewStatus = useCommunityStore((state) => state.previewStatus)
  const previewStream = useCommunityStore((state) => state.previewStream)
  const previewSteps = useCommunityStore((state) => state.previewSteps)
  const hydratePosts = useCommunityStore((state) => state.hydratePosts)
  const hydrateOverview = useCommunityStore((state) => state.hydrateOverview)
  const publishPreview = useCommunityStore((state) => state.publishPreview)
  const instructorReviewAction = useCommunityStore((state) => state.instructorReview)
  const finalReviewAction = useCommunityStore((state) => state.finalReview)
  const saveDraftAction = useCommunityStore((state) => state.saveDraft)
  const submitDraftAction = useCommunityStore((state) => state.submitDraft)
  const deletePostAction = useCommunityStore((state) => state.deletePost)
  const updateReviewNote = useCommunityStore((state) => state.updateReviewNote)
  const updateStructuredDraftState = useCommunityStore((state) => state.updateStructuredDraft)
  const updatePostDraftState = useCommunityStore((state) => state.updatePostDraft)
  const setComposerField = useCommunityStore((state) => state.setComposerField)
  const clearError = useCommunityStore((state) => state.clearError)
  const filterOptions = communityStatusOptions(user?.role)
  const activeFilter = initialCommunityFilter(searchParams, user?.role)
  const activeFilterQuery = communityFilterQuery(activeFilter)

  useEffect(() => {
    void hydratePosts(token, activeFilterQuery).catch(() => {})
  }, [activeFilterQuery, hydratePosts, token])

  useEffect(() => {
    void hydrateOverview(token)
  }, [hydrateOverview, token])

  function changeCommunityFilter(value: string) {
    const next = communityFilterFromOptionValue(filterOptions, value)
    clearError()
    setSearchParams(communityFilterSearchParams(next), { replace: true })
  }

  async function publish() {
    await publishPreview(token, activeFilter, user?.id)
  }

  async function instructorReview(post: CommunityPost, decision: 'approve' | 'reject') {
    await instructorReviewAction(token, post, decision, activeFilter, user?.id)
  }

  async function finalReview(post: CommunityPost, decision: 'publish' | 'reject') {
    await finalReviewAction(token, post, decision, activeFilter, user?.id)
  }

  async function saveDraft(post: CommunityPost) {
    await saveDraftAction(token, post, activeFilter, user?.id)
  }

  async function submitDraft(post: CommunityPost) {
    await submitDraftAction(token, post, activeFilter, user?.id)
  }

  async function deletePost(post: CommunityPost) {
    await deletePostAction(token, post)
  }

  const statusOverview = buildStatusOverview(overviewPosts, user)
  const highlightedPosts = posts.slice(0, 2)

  return (
    <section className="page-stack community-page">
      <section className="community-hero">
        <div className="community-hero-head">
          <div>
            <span className="community-overline">CASE WORKSHOP</span>
            <div className="community-title-row">
              <span className="community-title-icon"><BookOpenCheck size={24} /></span>
              <div className="community-title-group">
                <h1>案例工坊</h1>
                <p>学员先提交真实故障案例，AI 进行结构化预审。讲师初审内容质量，管理员终审发布为正式情景题。</p>
              </div>
            </div>
          </div>
        </div>

        <div className="community-workbench">
          <section className="community-panel form-panel" data-testid="community-workbench">
            <div className="community-panel-header">
              <span className="community-panel-kicker">PUBLISH WORKBENCH</span>
              <div className="community-panel-title-row">
                <div>
                  <h2>把真实故障整理成可审核案例</h2>
                  <p>标题和原始描述尽量贴近真实现场。系统会先生成结构化预审，不会直接公开发布。</p>
                </div>
              </div>
            </div>

            <div className="community-stage-row" aria-label="案例提交流程">
              <span className="community-stage-pill">1 案例标题</span>
              <span className="community-stage-pill">2 原始描述</span>
              <span className="community-stage-pill is-accent">3 AI 预审</span>
            </div>

            <div className="community-form-grid">
              <label>
                <span className="community-form-label-text">案例标题</span>
                <div className="community-field">
                  <span className="community-field-caption">标题建议突出故障现象和影响范围</span>
                  <input value={title} onChange={(event) => setComposerField('title', event.target.value)} />
                </div>
              </label>

              <label>
                <span className="community-form-label-text">原始描述</span>
                <div className="community-field">
                  <span className="community-field-caption">建议写清：触发条件、异常表现、影响范围和排查动作</span>
                  <textarea value={rawContent} onChange={(event) => setComposerField('rawContent', event.target.value)} />
                </div>
              </label>
            </div>

            <div className="community-preview-panel">
              <strong>AI 预审会先生成这些结构化内容</strong>
              <div className="community-preview-grid">
                <div className="community-preview-card">
                  <strong>根因提要</strong>
                  <span>自动抽取关键异常与可能根因。</span>
                </div>
                <div className="community-preview-card">
                  <strong>证据线索</strong>
                  <span>整理日志、指标和排查证据。</span>
                </div>
                <div className="community-preview-card is-risk">
                  <strong>风险自检</strong>
                  <span>先检查敏感信息和内容可发布性。</span>
                </div>
              </div>
            </div>

            <div className="community-action-row">
              <button className="primary-button compact community-primary-action" onClick={() => void publish()} disabled={isPublishing}>
                <Plus size={16} />
                {isPublishing ? '生成中' : '发布预览'}
              </button>
              <div className="community-action-hint">
                <strong>提交后不会立刻公开</strong>
                <span>先进入 AI 结构化预审，再流转到讲师初审。</span>
              </div>
            </div>

            {(isPublishing || previewStatus) && (
              <div className={`stream-status ${error ? 'error' : ''}`} role="status" aria-live="polite" data-testid="community-preview-status">
                <strong>{previewStatus || 'AI 正在生成结构化预览'}</strong>
                {previewSteps.length > 0 && (
                  <ol className="stream-step-list">
                    {previewSteps.map((step) => (
                      <li className={`stream-step stream-step-${step.status}`} key={step.step}>{step.label}</li>
                    ))}
                  </ol>
                )}
                {previewStream && <span>{previewStream}</span>}
              </div>
            )}
          </section>

          <aside className="community-status-column">
            <section className="community-status-panel" data-testid="community-status-panel">
              <div className="community-status-header">
                <span className="community-panel-kicker">MY STATUS</span>
                <h2>我的案例状态</h2>
                <p>不需要翻历史记录，先看现在卡在哪一步。</p>
              </div>
              <div className="community-status-grid">
                <div className="community-status-card is-accent">
                  <strong>{statusOverview.primary.value}</strong>
                  <span>{statusOverview.primary.label}</span>
                </div>
                <div className="community-status-card is-accent">
                  <strong>{statusOverview.secondary.value}</strong>
                  <span>{statusOverview.secondary.label}</span>
                </div>
                <div className="community-status-card is-warning">
                  <strong>{statusOverview.tertiary.value}</strong>
                  <span>{statusOverview.tertiary.label}</span>
                </div>
                <div className="community-status-card is-neutral">
                  <strong>{statusOverview.quaternary.value}</strong>
                  <span>{statusOverview.quaternary.label}</span>
                </div>
              </div>
            </section>

            <section className="community-feedback-panel">
              <span className="community-panel-kicker">NEEDS REVISION</span>
              <h2>{statusOverview.feedbackTitle}</h2>
              {statusOverview.latestFeedback.length > 0 ? (
                <ul className="community-feedback-list">
                  {statusOverview.latestFeedback.map((item: string) => <li key={item}>{item}</li>)}
                </ul>
              ) : (
                <p>{statusOverview.emptyFeedback}</p>
              )}
            </section>

            <section className="community-recent-panel">
              <div className="community-recent-header">
                <span className="community-panel-kicker">RECENT CASES</span>
                <h2>最近提交</h2>
                <p>与你当前动作最相关的投稿会优先展示。</p>
              </div>
              <div className="community-recent-list">
                {highlightedPosts.length > 0 ? highlightedPosts.map((post) => (
                  <div className="community-recent-card" key={`summary-${post.id}`}>
                    <div className="community-recent-top">
                      <span className="community-summary-state">{communityCompactStatus(post.status)}</span>
                      <span className="community-recent-time">{formatDateTime(post.updated_at || post.created_at)}</span>
                    </div>
                    <strong>{post.title}</strong>
                    <p>{communityLeadSummary(post)}</p>
                  </div>
                )) : (
                  <div className="community-recent-card">
                    <strong>还没有最近投稿</strong>
                    <p>你发布案例后，这里会优先显示最近一次提交和当前流转状态。</p>
                  </div>
                )}
              </div>
            </section>
          </aside>
        </div>
      </section>

      <section className="community-feed-shell">
        <section className="community-feed-toolbar">
          <div>
            <strong>{communityQueueTitle(user?.role, activeFilter)}</strong>
            <span>{communityQueueDescription(user?.role)}</span>
          </div>
          {filterOptions.length > 1 && (
            <SegmentedOptions
              value={communityFilterOptionValue(activeFilter)}
              onChange={changeCommunityFilter}
              options={filterOptions.map((option) => ({ value: communityFilterOptionValue(option), label: option.label }))}
            />
          )}
        </section>

        {error && <span className="inline-error">{error}</span>}

        <div className="community-feed-list">
        {posts.map((post) => (
          <CommunityPostCard
            key={post.id}
            post={post}
            user={{ id: user?.id, role: user?.role }}
            postDrafts={postDrafts}
            structuredDrafts={structuredDrafts}
            savingDraftId={savingDraftId}
            deletingId={deletingId}
            reviewingId={reviewingId}
            reviewNotes={reviewNotes}
            updatePostDraftState={updatePostDraftState}
            updateStructuredDraftState={updateStructuredDraftState}
            updateReviewNote={updateReviewNote}
            saveDraft={saveDraft}
            submitDraft={submitDraft}
            deletePost={deletePost}
            instructorReview={instructorReview}
            finalReview={finalReview}
          />
        ))}
        {posts.length === 0 && (
          <div className="community-empty-state">
            <h2>暂无案例</h2>
            <p>{communityEmptyDescription(user?.role, activeFilter)}</p>
            <button className="ghost-button" type="button" onClick={() => void hydratePosts(token, activeFilterQuery)}>刷新队列</button>
          </div>
        )}
        </div>
      </section>
    </section>
  )
}

function defaultCommunityFilter(role?: UserRole): CommunityFilter {
  if (role === 'instructor') return { kind: 'status', value: 'pending_review', label: '初审队列' }
  if (role === 'admin') return { kind: 'status', value: 'instructor_approved', label: '终审队列' }
  return { kind: 'all', value: '', label: '我的发布' }
}

function initialCommunityFilter(searchParams: URLSearchParams, role?: UserRole): CommunityFilter {
  const options = communityStatusOptions(role)
  if (searchParams.get('scope') === 'all') {
    const matched = options.find((option) => option.kind === 'all')
    if (matched) return matched
  }
  const view = validCommunityView(searchParams.get('view'))
  if (view) {
    const matched = options.find((option) => option.kind === 'view' && option.value === view)
    if (matched) return matched
  }
  const status = validCommunityStatus(searchParams.get('status'))
  if (status) {
    const matched = options.find((option) => option.kind === 'status' && option.value === status)
    if (matched) return matched
  }
  return defaultCommunityFilter(role)
}

function communityStatusOptions(role?: UserRole): CommunityFilter[] {
  if (role === 'admin') {
    return [
      { kind: 'status', value: 'instructor_approved', label: '终审队列' },
      { kind: 'status', value: 'pending_review', label: '待初审' },
      { kind: 'status', value: 'draft', label: '我的草稿' },
      { kind: 'status', value: 'published', label: '已发布' },
      { kind: 'all', value: '', label: '全部' },
    ]
  }
  if (role === 'instructor') {
    return [
      { kind: 'status', value: 'pending_review', label: '初审队列' },
      { kind: 'view', value: 'instructor_reviewed', label: '我已初审' },
      { kind: 'view', value: 'instructor_rejected', label: '我已驳回' },
      { kind: 'status', value: 'draft', label: '我的草稿' },
      { kind: 'all', value: '', label: '全部' },
    ]
  }
  return [{ kind: 'all', value: '', label: '我的发布' }]
}

function communityFilterOptionValue(filter: CommunityFilter) {
  if (filter.kind === 'all') return ''
  return `${filter.kind}:${filter.value}`
}

function communityFilterFromOptionValue(options: CommunityFilter[], value: string): CommunityFilter {
  return options.find((option) => communityFilterOptionValue(option) === value) ?? options[0] ?? { kind: 'all', value: '', label: '全部' }
}

function communityFilterQuery(filter: CommunityFilter) {
  if (filter.kind === 'all') return '?scope=all'
  if (filter.kind === 'status') return `?status=${encodeURIComponent(filter.value)}`
  if (filter.kind === 'view') return `?view=${encodeURIComponent(filter.value)}`
  return ''
}

function communityFilterSearchParams(filter: CommunityFilter) {
  const params = new URLSearchParams()
  if (filter.kind === 'all') params.set('scope', 'all')
  if (filter.kind === 'status') params.set('status', filter.value)
  if (filter.kind === 'view') params.set('view', filter.value)
  return params
}

function communityQueueTitle(role: UserRole | undefined, filter: CommunityFilter) {
  if (role === 'student') return '我的发布'
  if (filter.kind === 'view') return filter.label
  if (filter.kind === 'status' && filter.value === 'draft') return '我的草稿'
  if (role === 'admin' && filter.kind === 'status' && filter.value === 'instructor_approved') return '终审队列'
  if (role === 'instructor' && filter.kind === 'status' && filter.value === 'pending_review') return '初审队列'
  return filter.kind === 'status' ? communityStatusLabel(filter.value) : '全部案例'
}

function communityQueueDescription(role?: UserRole) {
  if (role === 'admin') return '管理员终审讲师通过的案例，发布后进入排查工坊题库。'
  if (role === 'instructor') return '讲师检查 AI 结构化预览和案例质量，决定是否提交终审。'
  return '学员发布案例后进入待审核状态，可查看自己的发布记录。'
}

function buildStatusOverview(posts: CommunityPost[], user?: User | null) {
  const role = user?.role
  const matchedOwnPosts = posts.filter((post) => isOwnCommunityPost(post, user))
  const ownPosts = role === 'student' && matchedOwnPosts.length === 0 ? posts : matchedOwnPosts
  const feedbackCandidates = (role === 'student' ? ownPosts : posts)
    .map((post) => post.final_note || post.review_note || '')
    .map((note) => note.trim())
    .filter(Boolean)
    .slice(0, 2)

  if (role === 'instructor') {
    return {
      primary: { label: '待初审', value: posts.filter((post) => post.status === 'pending_review').length },
      secondary: { label: '我已初审', value: posts.filter((post) => historyHasActorAction(post, user?.id, 'instructor_approve')).length },
      tertiary: { label: '我已驳回', value: posts.filter((post) => historyHasActorAction(post, user?.id, 'instructor_reject')).length },
      quaternary: { label: '全部案例', value: posts.length },
      feedbackTitle: '最近审核备注',
      emptyFeedback: '当前没有新的审核备注，完成初审后可在这里回看自己最近的处理意见。',
      latestFeedback: feedbackCandidates,
    }
  }

  if (role === 'admin') {
    return {
      primary: { label: '终审队列', value: posts.filter((post) => post.status === 'instructor_approved').length },
      secondary: { label: '待初审', value: posts.filter((post) => post.status === 'pending_review').length },
      tertiary: { label: '已发布', value: posts.filter((post) => post.status === 'published').length },
      quaternary: { label: '全部案例', value: posts.length },
      feedbackTitle: '最近终审反馈',
      emptyFeedback: '当前没有新的终审备注，完成终审后可在这里回看最近的发布或驳回意见。',
      latestFeedback: feedbackCandidates,
    }
  }

  return {
    primary: { label: '我的草稿', value: ownPosts.filter((post) => post.status === 'draft').length },
    secondary: { label: '讲师处理中', value: ownPosts.filter((post) => post.status === 'pending_review').length },
    tertiary: { label: '退回待修改', value: ownPosts.filter((post) => post.status === 'instructor_rejected').length },
    quaternary: { label: '已转入题库', value: ownPosts.filter((post) => post.status === 'published').length },
    feedbackTitle: '最近驳回反馈',
    emptyFeedback: '当前没有新的驳回意见，提交后可在这里回看讲师和管理员反馈。',
    latestFeedback: feedbackCandidates,
  }
}

function communityLeadSummary(post: CommunityPost) {
  const moderation = post.moderation_summary?.safe_summary?.trim()
  const rootCause = effectiveStructuredContent(post).root_cause?.trim()
  const note = post.final_note?.trim() || post.review_note?.trim()
  return moderation || note || rootCause || post.raw_content.trim()
}

function communityCardSummary(post: CommunityPost) {
  const content = effectiveStructuredContent(post)
  const moderation = post.moderation_summary
  const evidence = content.key_evidence ?? []
  const reasons = moderation?.reasons ?? []
  const reviewNote = post.final_note?.trim() || post.review_note?.trim() || ''

  return {
    title: communitySummaryTitle(post.status),
    state: communitySummaryState(post.status),
    primary: moderation?.recommendation?.trim() || content.root_cause?.trim() || '等待结构化摘要',
    secondary: evidence.length > 0 ? `证据：${evidence.slice(0, 2).join('；')}` : '证据待补充',
    tertiary: reviewNote || (reasons.length > 0 ? `原因：${reasons.slice(0, 2).join('；')}` : ''),
  }
}

function communitySummaryTitle(status: string) {
  const labels: Record<string, string> = {
    draft: '当前工作区',
    pending_review: '下一步建议',
    instructor_approved: '终审准备',
    instructor_rejected: '退回修改',
    published: '已转入题库',
  }
  return labels[status] ?? '状态摘要'
}

function communityStatusTone(status: string) {
  if (status === 'published' || status === 'instructor_approved') return 'is-ready'
  if (status === 'instructor_rejected') return 'is-risk'
  return 'is-draft'
}

function communitySummaryState(status: string) {
  const labels: Record<string, string> = {
    draft: '阶段：草稿',
    pending_review: '阶段：处理中',
    instructor_approved: '阶段：待终审',
    instructor_rejected: '阶段：待修改',
    published: '阶段：已入题库',
  }
  return labels[status] ?? '阶段：处理中'
}

function communityCompactStatus(status: string) {
  const labels: Record<string, string> = {
    draft: '草稿中',
    pending_review: '处理中',
    instructor_approved: '待终审',
    instructor_rejected: '待修改',
    published: '已入题库',
  }
  return labels[status] ?? '处理中'
}

function historyHasActorAction(post: CommunityPost, actorID: string | undefined, action: string) {
  if (!actorID) return false
  return post.review_history.some((item) => item.actor_id === actorID && item.action === action)
}

function isOwnCommunityPost(post: CommunityPost, user?: User | null) {
  if (!user) return false
  if (post.user_id === user.id) return true
  if (post.author_username && post.author_username === user.username) return true
  return false
}

function communityStatusLabel(status: string) {
  const labels: Record<string, string> = {
    draft: '草稿',
    pending_review: '待讲师初审',
    instructor_approved: '待管理员终审',
    instructor_rejected: '讲师已驳回',
    published: '已发布题库',
  }
  return labels[status] ?? status
}

function communityEmptyDescription(role: UserRole | undefined, filter: CommunityFilter) {
  if (filter.kind === 'view') return '当前视图没有匹配的审核历史案例。'
  if (filter.kind === 'status' && filter.value === 'pending_review') {
    return role === 'instructor' ? '当前没有等待你初审的案例。' : '当前没有等待讲师初审的案例。'
  }
  if (filter.kind === 'status' && filter.value === 'instructor_approved') return '当前没有等待管理员终审的案例。'
  if (filter.kind === 'status' && filter.value === 'draft') return '当前没有草稿案例。'
  return '当前视图没有案例。'
}

function validCommunityStatus(value: string | null) {
  if (!value) return null
  return ['draft', 'pending_review', 'instructor_approved', 'instructor_rejected', 'published'].includes(value) ? value : null
}

function validCommunityView(value: string | null): CommunityView | null {
  if (value === 'instructor_reviewed' || value === 'instructor_rejected') return value
  return null
}

function shouldDefaultExpand(post: CommunityPost, role?: UserRole) {
  if (role === 'admin' || role === 'instructor') {
    return false
  }
  return post.status === 'draft'
}

function canInstructorReview(role: UserRole | undefined, post: CommunityPost) {
  return (role === 'instructor' || role === 'admin') && post.status === 'pending_review'
}

function canFinalReview(role: UserRole | undefined, post: CommunityPost) {
  return role === 'admin' && post.status === 'instructor_approved'
}

function canAuthorEdit(userID: string | undefined, post: CommunityPost) {
  return Boolean(userID && post.user_id === userID && post.status === 'draft')
}

function canAuthorDelete(userID: string | undefined, role: UserRole | undefined, post: CommunityPost) {
  if (post.status === 'published') return role === 'admin'
  if (role === 'admin') return true
  return Boolean(userID && post.user_id === userID && ['draft', 'pending_review', 'instructor_rejected'].includes(post.status))
}

function CommunityPostCard({
  post,
  user,
  postDrafts,
  structuredDrafts,
  savingDraftId,
  deletingId,
  reviewingId,
  reviewNotes,
  updatePostDraftState,
  updateStructuredDraftState,
  updateReviewNote,
  saveDraft,
  submitDraft,
  deletePost,
  instructorReview,
  finalReview,
}: {
  post: CommunityPost
  user: UserRoleHolder
  postDrafts: Record<string, PostDraft>
  structuredDrafts: Record<string, StructuredDraft>
  savingDraftId: string
  deletingId: string
  reviewingId: string
  reviewNotes: Record<string, string>
  updatePostDraftState: (post: CommunityPost, patch: Partial<PostDraft>) => void
  updateStructuredDraftState: (post: CommunityPost, patch: Partial<StructuredDraft>) => void
  updateReviewNote: (postId: string, value: string) => void
  saveDraft: (post: CommunityPost) => Promise<void>
  submitDraft: (post: CommunityPost) => Promise<void>
  deletePost: (post: CommunityPost) => Promise<void>
  instructorReview: (post: CommunityPost, decision: 'approve' | 'reject') => Promise<void>
  finalReview: (post: CommunityPost, decision: 'publish' | 'reject') => Promise<void>
}) {
  const [isExpanded, setExpanded] = useState(() => shouldDefaultExpand(post, user.role))
  const [isDescriptionExpanded, setDescriptionExpanded] = useState(false)
  const canEdit = canAuthorEdit(user.id, post) || canInstructorReview(user.role, post)
  const canDelete = canAuthorDelete(user.id, user.role, post)
  const canExpandDescription = post.raw_content.trim().length > 96
  const summary = communityCardSummary(post)
  const statusTone = communityStatusTone(post.status)

  return (
    <article className={`scenario-card community-post-card${isExpanded ? ' expanded' : ''}`} key={post.id}>
      <div className="community-post-main">
        <div className="community-post-content">
          <div className="community-post-header">
            <div className="community-post-author">
              <strong>{post.author_username || '未知作者'}</strong>
              <span>{formatDateTime(post.updated_at || post.created_at)}</span>
            </div>
            <div className="scenario-meta">
              <span>{domainLabel(post.domain)}</span>
              <span>{communityStatusLabel(post.status)}</span>
            </div>
          </div>
          <div className={`community-summary-card ${statusTone}`}>
            <div className="community-summary-top">
              <strong>{summary.title}</strong>
              <span className="community-summary-state">{summary.state}</span>
            </div>
            <div className="community-summary-main">
              <strong>{summary.primary}</strong>
              <div className="community-summary-meta">
                <span>{summary.secondary}</span>
                {summary.tertiary ? <span>{summary.tertiary}</span> : null}
              </div>
            </div>
            {post.tags.length > 0 && (
              <div className="community-summary-tags">
                {post.tags.slice(0, 3).map((tag) => <span key={`${post.id}-${tag}`}>{tag}</span>)}
              </div>
            )}
          </div>
          <div className="community-post-copy">
            <h3>{post.title}</h3>
            <p className={isDescriptionExpanded ? 'expanded' : undefined}>{post.raw_content}</p>
          </div>
          <div className="community-post-inline-actions">
            {canExpandDescription && (
              <button
                className="ghost-button compact"
                type="button"
                aria-expanded={isDescriptionExpanded}
                onClick={() => setDescriptionExpanded((current) => !current)}
              >
                {isDescriptionExpanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
                {isDescriptionExpanded ? '收起描述' : '展开描述'}
              </button>
            )}
            <button className="ghost-button compact" type="button" aria-expanded={isExpanded} onClick={() => setExpanded((current) => !current)}>
              {isExpanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
              {isExpanded ? '收起详情' : '展开详情'}
            </button>
            {canAuthorEdit(user.id, post) && (
              <button className="ghost-button compact" type="button" onClick={() => setExpanded(true)}>
                <Edit3 size={16} />继续编辑
              </button>
            )}
            {canDelete && (
              <button className="danger-button compact" disabled={deletingId === post.id} onClick={() => void deletePost(post)}>
                <Trash2 size={16} />{deletingId === post.id ? '删除中' : '删除案例'}
              </button>
            )}
          </div>
          {post.forked_from_scenario_id && <div className="assistant-note community-fork-note">Fork 来源：{post.forked_from_scenario_id}，作者可先编辑草稿再提交初审。</div>}
          <div className="review-notes community-post-notes community-review-meta-card">
            {post.review_note && <span>初审意见：{post.review_note}</span>}
            {post.final_note && <span>终审意见：{post.final_note}</span>}
            {post.converted_question_id && <span>已转题：{post.converted_question_id}</span>}
          </div>
        </div>
      </div>
      {isExpanded && (
        <section className="community-post-detail-panel">
          <div className="community-post-summary-stack">
            <SensitiveNotice post={post} />
            <CommunityModerationSummaryBlock post={post} />
            <StructuredPreview post={post} />
            <CommunityReviewMeta post={post} />
          </div>
        </section>
      )}
      {isExpanded && canEdit ? (
        <div className="community-edit-shell">
          <div className="community-edit-toggle">
            <div className="community-edit-toggle-copy">
              <strong>编辑与修订工作区</strong>
              <span>保留原有编辑能力，但改为在展开区集中处理。</span>
            </div>
          </div>
          {canAuthorEdit(user.id, post) && (
            <PostDraftEditor draft={postDrafts[post.id] ?? postDraftFromPost(post)} onChange={(patch) => updatePostDraftState(post, patch)} />
          )}
          <StructuredEditor
            draft={structuredDrafts[post.id] ?? structuredDraftFromPost(post)}
            onChange={(patch) => updateStructuredDraftState(post, patch)}
          />
        </div>
      ) : null}
      {isExpanded && canAuthorEdit(user.id, post) && (
        <div className="card-actions">
          <button className="ghost-button compact" disabled={savingDraftId === post.id} onClick={() => void saveDraft(post)}><Edit3 size={16} />保存草稿</button>
          <button className="primary-button compact" disabled={savingDraftId === post.id} onClick={() => void submitDraft(post)}><CheckCircle2 size={16} />提交初审</button>
        </div>
      )}
      {isExpanded && post.review_history.length > 0 && <ReviewHistoryList items={post.review_history} />}
      {isExpanded && canInstructorReview(user.role, post) && (
        <div className="community-review-action review-action-box">
          <p>讲师审核时重点看结构化提要、证据链完整度以及是否适合进入终审。</p>
          <textarea
            value={reviewNotes[post.id] ?? ''}
            onChange={(event) => updateReviewNote(post.id, event.target.value)}
            placeholder="填写初审意见，说明内容质量、是否适合转题..."
          />
          <div className="card-actions">
            <button className="ghost-button compact" disabled={reviewingId === post.id} onClick={() => void instructorReview(post, 'reject')}>驳回</button>
            <button className="primary-button compact" disabled={reviewingId === post.id} onClick={() => void instructorReview(post, 'approve')}><CheckCircle2 size={16} />初审通过</button>
          </div>
        </div>
      )}
      {isExpanded && canFinalReview(user.role, post) && (
        <div className="community-review-action review-action-box">
          <p>管理员终审后，案例将进入排查工坊题库，请在发布前确认可用性和脱敏结果。</p>
          <textarea
            value={reviewNotes[post.id] ?? ''}
            onChange={(event) => updateReviewNote(post.id, event.target.value)}
            placeholder="填写终审意见，发布后将进入排查工坊题库..."
          />
          <div className="card-actions">
            <button className="ghost-button compact" disabled={reviewingId === post.id} onClick={() => void finalReview(post, 'reject')}>终审驳回</button>
            <button className="primary-button compact" disabled={reviewingId === post.id} onClick={() => void finalReview(post, 'publish')}><ShieldCheck size={16} />发布为题</button>
          </div>
        </div>
      )}
    </article>
  )
}

type UserRoleHolder = {
  id?: string
  role?: UserRole
}

function SensitiveNotice({ post }: { post: CommunityPost }) {
  const check = post.sensitive_check
  const findings = check?.findings ?? []
  if (!check || (findings.length === 0 && check.status === 'clear')) return null
  return (
    <div className="sensitive-notice">
      <strong><ShieldAlert size={16} />敏感信息提示 · {sensitiveStatusLabel(check.status)}</strong>
      <div className="sensitive-meta">
        <span>来源：{sensitiveSourceLabel(check.source)}</span>
        <span>风险：{riskLevelLabel(check.risk_level)}</span>
        <span>{check.fallback_used ? '已启用规则回退' : '模型检测未回退'}</span>
        {check.blocked && <span className="sensitive-blocked">建议暂缓发布</span>}
      </div>
      {check.summary && <p>{check.summary}</p>}
      {findings.slice(0, 3).map((finding) => (
        <div className="sensitive-finding" key={`${finding.type}-${finding.field}-${finding.redacted_excerpt || finding.excerpt}`}>
          <span>
            {finding.type} · {finding.field} · {severityLabel(finding.severity)}
            {typeof finding.confidence === 'number' ? ` · 置信度 ${formatConfidence(finding.confidence)}` : ''}
          </span>
          <code>{safeFindingExcerpt(finding)}</code>
          <small>{finding.suggestion || '请删除真实密钥、密码、Token、手机号等敏感内容，改用脱敏占位符或现象描述。'}</small>
        </div>
      ))}
    </div>
  )
}

function CommunityModerationSummaryBlock({ post }: { post: CommunityPost }) {
  const summary = post.moderation_summary
  const lines = [
    summary?.safe_summary?.trim(),
    summary?.safe_risk_note?.trim(),
    summary?.safe_action_hint?.trim(),
    summary?.suggested_note?.trim(),
  ].filter(Boolean) as string[]
  const labels = (summary?.safe_labels ?? []).map((item) => item.trim()).filter(Boolean)
  const reasons = (summary?.reasons ?? []).map((item) => item.trim()).filter(Boolean)

  if (lines.length === 0 && labels.length === 0 && reasons.length === 0 && !summary?.recommendation) return null

  return (
    <div className="structured-preview community-moderation-summary">
      <span>CM 审核摘要</span>
      {(summary?.status || summary?.risk_level) && (
        <small>
          {summary?.status ? `状态：${moderationStatusLabel(summary.status)}` : '状态：未声明'}
          {summary?.risk_level ? ` · 风险：${riskLevelLabel(summary.risk_level)}` : ''}
        </small>
      )}
      {summary?.recommendation && <strong>{summary.recommendation}</strong>}
      {lines.map((line, index) => <strong key={`${post.id}-cm-summary-${index}`}>{line}</strong>)}
      {reasons.length > 0 && <small>原因：{reasons.join('；')}</small>}
      {labels.length > 0 && <small>标签：{labels.join('；')}</small>}
    </div>
  )
}

function CommunityReviewMeta({ post }: { post: CommunityPost }) {
  const items = [
    `审核状态：${communityStatusLabel(post.status)}`,
    post.review_note ? `初审意见：${post.review_note}` : '',
    post.final_note ? `终审意见：${post.final_note}` : '',
    post.converted_question_id ? `已转题：${post.converted_question_id}` : '',
  ].filter(Boolean)

  if (items.length === 0) return null

  return (
    <div className="structured-preview community-review-meta-card">
      <span>审核状态 / 审核意见</span>
      {items.map((item) => <small key={item}>{item}</small>)}
    </div>
  )
}

function safeFindingExcerpt(finding: SensitiveFinding) {
  return finding.redacted_excerpt || redactSensitiveText(finding.excerpt) || '已隐藏敏感片段'
}

function redactSensitiveText(value: string) {
  return value
    .replace(/[A-Za-z0-9_-]{24,}/g, (match) => `${match.slice(0, 4)}...${match.slice(-4)}`)
    .replace(/(password|passwd|token|secret|key)\s*[:=]\s*\S+/gi, '$1=***')
}

function sensitiveStatusLabel(status?: string) {
  if (status === 'risk') return '存在风险'
  if (status === 'needs_review') return '需复核'
  return '未发现风险'
}

function sensitiveSourceLabel(source?: string) {
  const labels: Record<string, string> = {
    model: '模型检测',
    rule: '规则检测',
    merged: '规则+模型',
    'rule+model': '规则+模型',
    rule_fallback: '规则回退',
  }
  return labels[source ?? ''] ?? source ?? '未声明'
}

function riskLevelLabel(level?: string) {
  const labels: Record<string, string> = {
    none: '无',
    low: '低',
    medium: '中',
    high: '高',
  }
  return labels[level ?? ''] ?? level ?? '未声明'
}

function severityLabel(severity: string) {
  return riskLevelLabel(severity)
}

function formatConfidence(value: number) {
  return `${Math.round(value * 100)}%`
}

function moderationStatusLabel(status?: string) {
  const labels: Record<string, string> = {
    pending: '待审核',
    reviewed: '已审核',
    safe_only: '仅安全摘要',
    blocked: '需人工处理',
  }
  return labels[status ?? ''] ?? status ?? '未声明'
}

interface StructuredDraft {
  rootCause: string
  keyEvidence: string
  standardProcedure: string
  architectureDiagram: string
  referenceLinks: string
}

interface PostDraft {
  title: string
  rawContent: string
  domain: string
  tags: string
}

function PostDraftEditor({ draft, onChange }: { draft: PostDraft; onChange: (patch: Partial<PostDraft>) => void }) {
  return (
    <div className="structured-editor community-edit-grid">
      <label>草稿标题<input value={draft.title} onChange={(event) => onChange({ title: event.target.value })} /></label>
      <label>现象描述<textarea value={draft.rawContent} onChange={(event) => onChange({ rawContent: event.target.value })} /></label>
      <label>专业域<input value={draft.domain} onChange={(event) => onChange({ domain: event.target.value })} /></label>
      <label>标签<input value={draft.tags} onChange={(event) => onChange({ tags: event.target.value })} /></label>
    </div>
  )
}

function StructuredEditor({ draft, onChange }: { draft: StructuredDraft; onChange: (patch: Partial<StructuredDraft>) => void }) {
  return (
    <div className="structured-editor community-edit-grid">
      <label>根因修订<textarea value={draft.rootCause} onChange={(event) => onChange({ rootCause: event.target.value })} /></label>
      <label>关键证据<textarea value={draft.keyEvidence} onChange={(event) => onChange({ keyEvidence: event.target.value })} /></label>
      <label>标准步骤<textarea value={draft.standardProcedure} onChange={(event) => onChange({ standardProcedure: event.target.value })} /></label>
      <label>架构图 Mermaid<textarea value={draft.architectureDiagram} onChange={(event) => onChange({ architectureDiagram: event.target.value })} /></label>
      <label>参考链接<textarea value={draft.referenceLinks} onChange={(event) => onChange({ referenceLinks: event.target.value })} /></label>
    </div>
  )
}

function StructuredPreview({ post }: { post: CommunityPost }) {
  const content = effectiveStructuredContent(post)
  const evidence = content.key_evidence ?? []
  return (
    <div className="structured-preview community-structured-preview">
      <span>{post.edited_structured_content ? '讲师编辑版' : 'AI 预览版'}</span>
      <strong>{content.root_cause || '尚无根因摘要'}</strong>
      {evidence.length > 0 && <small>证据：{evidence.slice(0, 2).join('；')}</small>}
    </div>
  )
}

function ReviewHistoryList({ items }: { items: CommunityPost['review_history'] }) {
  return (
    <div className="review-history">
      {items.map((item) => (
        <div key={item.id || `${item.action}-${item.created_at}`}>
          <strong>{reviewHistoryActionLabel(item.action)} · {communityStatusLabel(item.to_status)}</strong>
          <span>{formatDateTime(item.created_at)}{item.note ? ` · ${item.note}` : ''}</span>
        </div>
      ))}
    </div>
  )
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
    tags: (post.tags ?? []).join(','),
  }
}

function effectiveStructuredContent(post: CommunityPost): ScenarioContent {
  return post.edited_structured_content ?? post.ai_structured_content
}

function reviewHistoryActionLabel(action: string) {
  const labels: Record<string, string> = {
    instructor_approve: '讲师初审通过',
    instructor_reject: '讲师驳回',
    final_publish: '管理员发布',
    final_reject: '管理员驳回',
  }
  return labels[action] ?? action
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
