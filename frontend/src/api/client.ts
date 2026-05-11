import type {
  ApiEnvelope,
  AIJob,
  AIConfig,
  AIStatus,
  AuditEvent,
  Asset,
  CommunityPost,
  InterviewQuestion,
  InterviewSession,
  VoiceQualityResult,
  CheckinResult,
  LearningPlan,
  ReviewCalendar,
  ScenarioContent,
  ScenarioGenerationConstraints,
  ScenarioMessage,
  ScenarioQuestion,
  ScenarioScore,
  ScenarioSession,
  ScenarioSessionDetailResponse,
  ScenarioEvaluation,
  PromptTemplate,
  SystemStatus,
  User,
  UserRole,
  InterviewSessionDetailResponse,
} from '../types'

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1'
const REQUEST_TIMEOUT_MS = 90000

export interface AuthResponse {
  user: User
  access_token: string
  refresh_token: string
}

export interface ScenarioMessageResponse {
  message: ScenarioMessage
  response_meta: ScenarioMessage['response_meta']
  session_status: string
  session: ScenarioSession
}

export interface ScenarioGenerationJobResponse {
  job: AIJob
  question_id?: string
  question?: ScenarioQuestion
}

export interface ScenarioGenerationPayload {
  domain: string
  difficulty: string
  scenario_type: string
  constraints?: ScenarioGenerationConstraints
}

export interface ScenarioListParams {
  domain?: string
  difficulty?: string
  tag?: string
  page?: number
  page_size?: number
}

export interface StreamEvent {
  event: string
  data: unknown
}

export interface StreamStage {
  step: string
  message: string
}

type InterviewSubmitResponse = { evaluation: InterviewSession['evaluations'][number]; session_status: string; session: InterviewSession }

function buildApiURL(path: string) {
  return `${API_BASE}${path}`
}

async function request<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
  const headers = new Headers(options.headers)
  if (!(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  const upstreamSignal = options.signal
  const abort = () => controller.abort()
  upstreamSignal?.addEventListener('abort', abort, { once: true })

  try {
    const response = await fetch(buildApiURL(path), { ...options, headers, signal: controller.signal })
    const body = await readApiEnvelope<T>(response)
    if (!response.ok || body.code >= 400) {
      throw new Error(body.message || '请求失败')
    }
    return body.data
  } catch (err) {
    throw normalizeFetchError(err)
  } finally {
    window.clearTimeout(timeout)
    upstreamSignal?.removeEventListener('abort', abort)
  }
}

async function requestStream<T>(
  path: string,
  options: RequestInit,
  token: string,
  onEvent: (event: StreamEvent) => void,
): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('Accept', 'text/event-stream')
  if (!(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  headers.set('Authorization', `Bearer ${token}`)

  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  const upstreamSignal = options.signal
  const abort = () => controller.abort()
  upstreamSignal?.addEventListener('abort', abort, { once: true })

  try {
    const response = await fetch(buildApiURL(path), { ...options, headers, signal: controller.signal })
    if (!response.ok) {
      throw new Error(await readErrorMessage(response, '流式请求失败'))
    }
    if (isJSONResponse(response)) {
      const body = await readApiEnvelope<T>(response)
      if (body.code >= 400) {
        throw new Error(body.message || '请求失败')
      }
      return body.data
    }
    if (!response.body) {
      throw new Error('浏览器不支持流式响应')
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let finalPayload: T | null = null

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
      let boundary = buffer.indexOf('\n\n')
      while (boundary >= 0) {
        const rawEvent = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const parsed = parseSSEEvent(rawEvent)
        const data = parsed.data ? JSON.parse(parsed.data) as unknown : null
        onEvent({ event: parsed.event, data })
        if (parsed.event === 'finish') {
          finalPayload = data as T
        }
        if (parsed.event === 'error') {
          throw new Error(eventMessage(data))
        }
        boundary = buffer.indexOf('\n\n')
      }
    }
    if (!finalPayload) {
      throw new Error('流式响应缺少完成事件')
    }
    return finalPayload
  } catch (err) {
    throw normalizeFetchError(err)
  } finally {
    window.clearTimeout(timeout)
    upstreamSignal?.removeEventListener('abort', abort)
  }
}

async function requestScenarioMessageStream(
  token: string,
  sessionId: string,
  content: string,
  onDelta: (chunk: string) => void,
  onStage?: (stage: StreamStage) => void,
): Promise<ScenarioMessageResponse> {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  try {
    const response = await fetch(buildApiURL(`/scenarios/sessions/${sessionId}/messages`), {
      method: 'POST',
      headers: {
        Accept: 'text/event-stream',
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ content }),
      signal: controller.signal,
    })
    if (!response.ok) {
      throw new Error(await readErrorMessage(response, '流式消息请求失败'))
    }
    if (!response.body) {
      throw new Error('浏览器不支持流式响应')
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let finalPayload: ScenarioMessageResponse | null = null

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      let boundary = buffer.indexOf('\n\n')
      while (boundary >= 0) {
        const rawEvent = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const parsed = parseSSEEvent(rawEvent)
        if (parsed.event === 'delta') {
          const data = JSON.parse(parsed.data) as { chunk?: string }
          if (data.chunk) onDelta(data.chunk)
        }
        if (parsed.event === 'stage') {
          const data = JSON.parse(parsed.data) as Partial<StreamStage>
          if (data.step || data.message) onStage?.({ step: String(data.step ?? ''), message: String(data.message ?? '') })
        }
        if (parsed.event === 'finish') {
          finalPayload = JSON.parse(parsed.data) as ScenarioMessageResponse
        }
        boundary = buffer.indexOf('\n\n')
      }
    }
    if (!finalPayload) {
      throw new Error('流式响应缺少完成事件')
    }
    return finalPayload
  } catch (err) {
    throw normalizeFetchError(err)
  } finally {
    window.clearTimeout(timeout)
  }
}

async function readApiEnvelope<T>(response: Response): Promise<ApiEnvelope<T>> {
  const text = await response.text()
  if (!text) {
    return { code: response.status, message: response.statusText || '请求失败' } as ApiEnvelope<T>
  }
  try {
    return JSON.parse(text) as ApiEnvelope<T>
  } catch {
    return { code: response.status, message: text } as ApiEnvelope<T>
  }
}

async function readErrorMessage(response: Response, fallback: string) {
  const text = await response.text()
  if (!text) return fallback
  try {
    const body = JSON.parse(text) as Partial<ApiEnvelope<unknown>>
    return body.message || text
  } catch {
    return text
  }
}

function isJSONResponse(response: Response) {
  return (response.headers.get('content-type') ?? '').toLowerCase().includes('application/json')
}

function normalizeFetchError(err: unknown): Error {
  if (err instanceof DOMException && err.name === 'AbortError') {
    return new Error('请求超时，请稍后重试或检查 AI Provider 配置', { cause: err })
  }
  if (err instanceof TypeError && isNetworkFetchError(err.message)) {
    return new Error('无法连接后端 API，请确认服务已启动后刷新页面重试', { cause: err })
  }
  return err instanceof Error ? err : new Error('请求失败')
}

function isNetworkFetchError(message: string) {
  const normalized = message.toLowerCase()
  return normalized.includes('failed to fetch') || normalized.includes('networkerror') || normalized.includes('load failed') || normalized.includes('fetch')
}

function parseSSEEvent(rawEvent: string) {
  const lines = rawEvent.split('\n')
  let event = 'message'
  const dataLines: string[] = []
  for (const line of lines) {
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim()
    }
    if (line.startsWith('data:')) {
      dataLines.push(line.slice('data:'.length).trimStart())
    }
  }
  return { event, data: dataLines.join('\n') }
}

function eventMessage(data: unknown) {
  if (typeof data === 'object' && data && 'message' in data) {
    return String((data as { message?: unknown }).message ?? '流式请求失败')
  }
  return '流式请求失败'
}

function eventChunk(data: unknown) {
  if (typeof data === 'object' && data && 'chunk' in data) {
    if ('displayable' in data && (data as { displayable?: unknown }).displayable === false) {
      return ''
    }
    return String((data as { chunk?: unknown }).chunk ?? '')
  }
  return ''
}

function eventStageMessage(data: unknown) {
  if (typeof data === 'object' && data && 'message' in data) {
    return String((data as { message?: unknown }).message ?? '')
  }
  return ''
}

function eventStageStep(data: unknown) {
  if (typeof data === 'object' && data && 'step' in data) {
    return String((data as { step?: unknown }).step ?? '')
  }
  return ''
}

function handleStreamProgress(
  onStage?: (stage: StreamStage) => void,
  onDelta?: (chunk: string) => void,
) {
  return ({ event, data }: StreamEvent) => {
    if (event === 'stage') {
      const message = eventStageMessage(data)
      const step = eventStageStep(data)
      if (message || step) onStage?.({ step, message })
    }
    if (event === 'delta') {
      const chunk = eventChunk(data)
      if (chunk) onDelta?.(chunk)
    }
  }
}

function scenarioListQuery(params: ScenarioListParams = {}) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      query.set(key, String(value))
    }
  }
  const text = query.toString()
  return text ? `?${text}` : ''
}

function normalizeScenarioEvaluation(evaluation?: ScenarioEvaluation): ScenarioEvaluation | undefined {
  if (!evaluation) return evaluation
  if (!evaluation.scoring_report) {
    return {
      ...evaluation,
      missing_points: evaluation.missing_points ?? [],
      standard_procedure: evaluation.standard_procedure ?? [],
    }
  }
  return {
    ...evaluation,
    missing_points: evaluation.missing_points ?? [],
    standard_procedure: evaluation.standard_procedure ?? [],
    scoring_report: {
      ...evaluation.scoring_report,
      matched_documents: evaluation.scoring_report.matched_documents ?? [],
      evidence_events: evaluation.scoring_report.evidence_events ?? [],
      penalties: evaluation.scoring_report.penalties ?? [],
      score_explanation: evaluation.scoring_report.score_explanation ?? '',
    },
  }
}

function normalizeScenarioSession(session: ScenarioSession): ScenarioSession {
  return {
    ...session,
    revealed_clue_ids: session.revealed_clue_ids ?? [],
    evaluation_result: normalizeScenarioEvaluation(session.evaluation_result),
  }
}

export const api = {
  aiStatus: () => request<AIStatus>('/system/ai'),

  systemStatus: (token: string) => request<SystemStatus>('/system/status', {}, token),

  createAsset: (token: string, payload: { kind: string; filename: string; mime_type: string; size: number; checksum?: string }) =>
    request<Asset>('/assets', { method: 'POST', body: JSON.stringify(payload) }, token),

  uploadVoiceAsset: (token: string, file: File) => {
    const body = new FormData()
    body.set('kind', 'voice')
    body.set('file', file)
    return request<Asset>('/assets', { method: 'POST', body }, token)
  },

  asset: (token: string, assetId: string) => request<Asset>(`/assets/${assetId}`, {}, token),

  assetContentBlob: async (token: string, assetId: string) => {
    const response = await fetch(buildApiURL(`/assets/${assetId}?content=1`), {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    })
    if (!response.ok) {
      throw new Error(await readErrorMessage(response, '语音资源读取失败'))
    }
    return response.blob()
  },

  login: (identifier: string, password: string) =>
    request<AuthResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ identifier, password }),
    }),

  register: (username: string, email: string, password: string) =>
    request<AuthResponse>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ username, email, password }),
    }),

  refresh: (refreshToken: string) =>
    request<AuthResponse>('/auth/refresh', {
      method: 'POST',
      body: JSON.stringify({ refresh_token: refreshToken }),
    }),

  me: (token: string) => request<User>('/users/me', {}, token),

  updateProfile: (token: string, targetLevel: string, preferredDomains: string[]) =>
    request<User>(
      '/users/me/profile',
      {
        method: 'PUT',
        body: JSON.stringify({ target_level: targetLevel, preferred_domains: preferredDomains }),
      },
      token,
    ),

  updatePassword: (token: string, newPassword: string) =>
    request<{ user: User }>(
      '/users/me/password',
      {
        method: 'POST',
        body: JSON.stringify({ new_password: newPassword }),
      },
      token,
    ),

  dashboard: (token: string) =>
    request<{
      user: User
      stats: User['profile']['total_stats']
      capability_radar: Record<string, number>
      weak_points: User['profile']['weak_points']
      recommendations: ScenarioQuestion[]
      learning_plan: LearningPlan
      review_calendar: ReviewCalendar
    }>('/users/me/dashboard', {}, token),

  history: (token: string) =>
    request<{ scenarios: ScenarioSession[]; interviews: InterviewSession[]; community_posts: CommunityPost[] }>('/users/me/history', {}, token),

  learningPlan: (token: string) => request<LearningPlan>('/users/me/learning-plan', {}, token),

  reviewCalendar: (token: string) => request<ReviewCalendar>('/users/me/review-calendar', {}, token),

  checkin: (token: string) =>
    request<{ checkin: CheckinResult; user: User }>('/users/me/checkin', { method: 'POST' }, token),

  scenarios: (token: string, params: ScenarioListParams = {}) =>
    request<{ list: ScenarioQuestion[]; total: number }>(`/scenarios${scenarioListQuery(params)}`, {}, token),

  scenarioDetail: (token: string, id: string) => request<ScenarioQuestion>(`/scenarios/${id}`, {}, token),

  generateScenario: (token: string, payload: ScenarioGenerationPayload) =>
    request<{ question_id: string; status: string; question: ScenarioQuestion; provider: string; validated: boolean; fallback_used: boolean }>(
      '/scenarios/generate',
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  startScenarioGenerationJob: (token: string, payload: ScenarioGenerationPayload) =>
    request<ScenarioGenerationJobResponse>(
      '/scenarios/generate/jobs',
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  aiJob: (token: string, jobId: string) => request<ScenarioGenerationJobResponse>(`/ai/jobs/${jobId}`, {}, token),

  cancelAIJob: (token: string, jobId: string) => request<ScenarioGenerationJobResponse>(`/ai/jobs/${jobId}/cancel`, { method: 'POST' }, token),

  createScenarioSession: (token: string, id: string) =>
    request<{ session_id: string; status: string; question_snapshot: ScenarioQuestion }>(
      `/scenarios/${id}/sessions`,
      { method: 'POST' },
      token,
    ),

  scenarioSessionDetail: (token: string, sessionId: string) =>
    request<ScenarioSessionDetailResponse>(`/scenarios/sessions/${sessionId}`, {}, token),

  forkScenario: (token: string, id: string) =>
    request<CommunityPost>(`/scenarios/${id}/fork`, { method: 'POST' }, token),

  sendScenarioMessage: (token: string, sessionId: string, content: string) =>
    request<ScenarioMessageResponse>(
      `/scenarios/sessions/${sessionId}/messages`,
      { method: 'POST', body: JSON.stringify({ content }) },
      token,
    ),

  sendScenarioMessageStream: requestScenarioMessageStream,

  submitInterviewStream: (
    token: string,
    sessionId: string,
    content: string,
    onEvent: { onStage?: (stage: StreamStage) => void; onDelta?: (chunk: string) => void },
  ) =>
    requestStream<InterviewSubmitResponse>(
      `/interviews/sessions/${sessionId}/submit`,
      { method: 'POST', body: JSON.stringify({ content, type: 'text' }) },
      token,
      handleStreamProgress(onEvent.onStage, onEvent.onDelta),
    ),

  submitVoiceInterviewStream: (
    token: string,
    sessionId: string,
    payload: { content: string; transcript: string; asset_id: string; duration_seconds?: number; source?: 'voice_transcript' | 'voice_edited'; confirmed_transcript?: boolean },
    onEvent: { onStage?: (stage: StreamStage) => void; onDelta?: (chunk: string) => void },
  ) =>
    requestStream<InterviewSubmitResponse>(
      `/interviews/sessions/${sessionId}/submit`,
      { method: 'POST', body: JSON.stringify({ ...payload, type: 'voice' }) },
      token,
      handleStreamProgress(onEvent.onStage, onEvent.onDelta),
    ),

  answerFollowupStream: (
    token: string,
    sessionId: string,
    content: string,
    onEvent: { onStage?: (stage: StreamStage) => void; onDelta?: (chunk: string) => void },
  ) =>
    requestStream<InterviewSubmitResponse>(
      `/interviews/sessions/${sessionId}/followup/answer`,
      { method: 'POST', body: JSON.stringify({ content, type: 'text' }) },
      token,
      handleStreamProgress(onEvent.onStage, onEvent.onDelta),
    ),

  submitScenarioAnswer: async (token: string, sessionId: string, answer: string) => {
    const data = await request<{
      evaluation_id: string
      status: string
      result: ScenarioEvaluation
      score: ScenarioScore
    }>(`/scenarios/sessions/${sessionId}/answer`, { method: 'POST', body: JSON.stringify({ answer }) }, token)
    return {
      ...data,
      result: normalizeScenarioEvaluation(data.result) ?? data.result,
    }
  },

  quitScenarioSession: (token: string, sessionId: string) =>
    request<{ status: string; session: ScenarioSession }>(
      `/scenarios/sessions/${sessionId}/quit`,
      { method: 'POST' },
      token,
    ),

  scenarioReview: async (token: string, sessionId: string) => {
    const data = await request<{
      session: ScenarioSession
      messages: ScenarioMessage[]
      standard_answer: string
      standard_steps: string[]
      key_evidence: string[]
    }>(`/scenarios/sessions/${sessionId}/review`, {}, token)
    return {
      ...data,
      session: normalizeScenarioSession(data.session),
      messages: data.messages ?? [],
      standard_steps: data.standard_steps ?? [],
      key_evidence: data.key_evidence ?? [],
    }
  },

  createInterview: (token: string, payload: { domain: string; difficulty: string; question_type: string }) =>
    request<{ session_id: string; status: string; question: InterviewQuestion; session: InterviewSession }>(
      '/interviews/sessions',
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  interviewSessionDetail: (token: string, sessionId: string) =>
    request<InterviewSessionDetailResponse>(`/interviews/sessions/${sessionId}`, {}, token),

  deleteInterviewSession: (token: string, sessionId: string) =>
    request<{ deleted: boolean; id: string }>(`/interviews/sessions/${sessionId}`, { method: 'DELETE' }, token),

  submitInterview: (token: string, sessionId: string, content: string) =>
    request<{ evaluation: InterviewSession['evaluations'][number]; session_status: string; session: InterviewSession }>(
      `/interviews/sessions/${sessionId}/submit`,
      { method: 'POST', body: JSON.stringify({ content, type: 'text' }) },
      token,
    ),

  submitVoiceInterview: (
    token: string,
    sessionId: string,
    payload: { content: string; transcript: string; asset_id: string; duration_seconds?: number; source?: 'voice_transcript' | 'voice_edited'; confirmed_transcript?: boolean },
  ) =>
    request<{ evaluation: InterviewSession['evaluations'][number]; session_status: string; session: InterviewSession }>(
      `/interviews/sessions/${sessionId}/submit`,
      { method: 'POST', body: JSON.stringify({ ...payload, type: 'voice' }) },
      token,
    ),

  transcribeInterviewVoice: (
    token: string,
    sessionId: string,
    payload: { asset_id: string; transcript?: string; duration_seconds?: number },
  ) =>
    request<{ asset: Asset; transcript: string; duration_seconds: number; status: string; quality: VoiceQualityResult }>(
      `/interviews/sessions/${sessionId}/voice`,
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  answerFollowup: (token: string, sessionId: string, content: string) =>
    request<{ evaluation: InterviewSession['evaluations'][number]; session_status: string; session: InterviewSession }>(
      `/interviews/sessions/${sessionId}/followup/answer`,
      { method: 'POST', body: JSON.stringify({ content, type: 'text' }) },
      token,
    ),

  interviewReport: (token: string, sessionId: string) =>
    request<{
      session: InterviewSession
      question: InterviewQuestion
      radar_data: Array<{ dimension: string; score: number }>
      final_score: number
      final_report: string
    }>(`/interviews/sessions/${sessionId}/report`, {}, token),

  communityPosts: (token: string, query = '') => request<{ list: CommunityPost[] }>(`/community/posts${query}`, {}, token),

  createCommunityPost: (token: string, payload: { title: string; raw_content: string; domain: string; tags: string[] }) =>
    request<CommunityPost>('/community/posts', { method: 'POST', body: JSON.stringify(payload) }, token),

  createCommunityPostStream: (
    token: string,
    payload: { title: string; raw_content: string; domain: string; tags: string[] },
    onEvent: { onStage?: (stage: StreamStage) => void; onDelta?: (chunk: string) => void },
  ) =>
    requestStream<CommunityPost>(
      '/community/posts',
      { method: 'POST', body: JSON.stringify(payload) },
      token,
      handleStreamProgress(onEvent.onStage, onEvent.onDelta),
    ),

  communityPost: (token: string, postId: string) => request<CommunityPost>(`/community/posts/${postId}`, {}, token),

  updateCommunityPost: (
    token: string,
    postId: string,
    payload: { title?: string; raw_content?: string; domain?: string; tags?: string[]; structured_content?: ScenarioContent },
  ) =>
    request<CommunityPost>(`/community/posts/${postId}`, { method: 'PUT', body: JSON.stringify(payload) }, token),

  submitCommunityPost: (token: string, postId: string) =>
    request<CommunityPost>(`/community/posts/${postId}/submit`, { method: 'POST' }, token),

  deleteCommunityPost: (token: string, postId: string) =>
    request<{ deleted: boolean; id: string }>(`/community/posts/${postId}`, { method: 'DELETE' }, token),

  instructorReviewPost: (
    token: string,
    postId: string,
    payload: { decision: 'approve' | 'reject'; note: string; structured_content?: ScenarioContent },
  ) =>
    request<CommunityPost>(
      `/community/posts/${postId}/instructor-review`,
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  finalReviewPost: (token: string, postId: string, payload: { decision: 'publish' | 'reject'; note: string }) =>
    request<{ post: CommunityPost; question?: ScenarioQuestion }>(
      `/community/posts/${postId}/final-review`,
      { method: 'POST', body: JSON.stringify(payload) },
      token,
    ),

  adminUsers: (token: string) => request<{ list: User[] }>('/admin/users', {}, token),

  updateUserRole: (token: string, userId: string, role: UserRole) =>
    request<User>(`/admin/users/${userId}/role`, { method: 'PUT', body: JSON.stringify({ role }) }, token),

  adminPrompts: (token: string) => request<{ list: PromptTemplate[] }>('/admin/prompts', {}, token),

  updateAdminPrompt: (token: string, name: string, payload: { content?: string; render_engine?: string; reset_default?: boolean }) =>
    request<PromptTemplate>(`/admin/prompts/${name}`, { method: 'PUT', body: JSON.stringify(payload) }, token),

  adminAIConfig: (token: string) => request<AIConfig>('/admin/ai-config', {}, token),

  updateAdminAIConfig: (token: string, payload: AIConfig) =>
    request<AIConfig>('/admin/ai-config', { method: 'PUT', body: JSON.stringify(payload) }, token),

  adminAuditEvents: (token: string, limit = 30) =>
    request<{ list: AuditEvent[] }>(`/admin/audit-events?limit=${limit}`, {}, token),
}
