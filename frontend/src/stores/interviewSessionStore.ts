import { create } from 'zustand'
import { api } from '../api/client'
import type { AgentTrace, Asset, InterviewEvaluation, InterviewQuestion, InterviewSession, VoiceQualityResult } from '../types'

const STREAM_PREVIEW_MAX = 280

interface InterviewSessionState {
  sessionId: string
  question: InterviewQuestion | null
  session: InterviewSession | null
  isLoading: boolean
  lastEvaluation: InterviewEvaluation | null
  agentTrace: AgentTrace | null
  agentStageMessages: string[]
  isSubmitting: boolean
  submitStatus: string
  streamPreview: string
  submitError: string
  voiceAsset: Asset | null
  voiceStatus: string
  voiceQuality: VoiceQualityResult | null
  voiceTranscript: string
  voiceConfirmed: boolean
  voiceDuration?: number
  uploadProgress: number
  isVoiceBusy: boolean
  submitType: 'text' | 'voice'
  voiceSessionExpired: boolean
  hydrate: (token: string, sessionId: string, optimistic?: { question?: InterviewQuestion | null; session?: InterviewSession | null }) => Promise<void>
  submitAnswer: (
    token: string,
    sessionId: string,
    payload: {
      answer: string
      voiceAsset: Asset | null
      voiceQuality: VoiceQualityResult | null
      voiceTranscript: string
      voiceDuration?: number
    },
  ) => Promise<{ session_status: string }>
  uploadVoiceFile: (token: string, sessionId: string, file: File) => Promise<string>
  confirmVoiceTranscript: () => void
  updateVoiceEditedState: (statusMessage?: string) => void
  rejectVoiceDraft: (reason: string) => void
  setSubmitFeedback: (patch: {
    submitStatus?: string
    submitError?: string
    streamPreview?: string
  }) => void
  clearVoiceDraft: () => void
  clear: () => void
}

function emptyState() {
  return {
    sessionId: '',
    question: null,
    session: null,
    isLoading: false,
    lastEvaluation: null,
    agentTrace: null,
    agentStageMessages: [] as string[],
    isSubmitting: false,
    submitStatus: '',
    streamPreview: '',
    submitError: '',
    voiceAsset: null as Asset | null,
    voiceStatus: '',
    voiceQuality: null as VoiceQualityResult | null,
    voiceTranscript: '',
    voiceConfirmed: false,
    voiceDuration: undefined as number | undefined,
    uploadProgress: 0,
    isVoiceBusy: false,
    submitType: 'text' as const,
    voiceSessionExpired: false,
  }
}

export const useInterviewSessionStore = create<InterviewSessionState>((set, get) => ({
  ...emptyState(),

  hydrate: async (token, sessionId, optimistic) => {
    const hasOptimistic = Boolean(optimistic?.question && optimistic?.session)
    set(() => ({
      ...emptyState(),
      sessionId,
      question: optimistic?.question ?? null,
      session: optimistic?.session ?? null,
      isLoading: true,
    }))
    try {
      const detail = await api.interviewSessionDetail(token, sessionId)
      set((state) => ({
        ...state,
        sessionId,
        question: detail.question,
        session: detail.session,
        isLoading: false,
      }))
    } catch (err) {
      set((state) => ({
        ...state,
        isLoading: false,
        submitError: hasOptimistic ? '' : (err instanceof Error ? err.message : '读取面试会话失败'),
      }))
      throw err
    }
  },

  submitAnswer: async (token, sessionId, payload) => {
    const answer = payload.answer.trim()
    if (!answer) {
      return { session_status: '' }
    }
    if (get().submitType === 'voice' && !get().voiceConfirmed) {
      set((state) => ({
        ...state,
        submitError: '请先确认语音转写文本，再提交评分',
        submitStatus: '等待确认转写文本',
      }))
      return { session_status: '' }
    }

    set((state) => ({
      ...state,
      isSubmitting: true,
      submitStatus: '提交答案中',
      streamPreview: '',
      submitError: '',
      agentTrace: null,
      agentStageMessages: [],
    }))

    try {
      const streamHandlers = {
        onStage: (stage: { step: string; message: string }) => {
          set((state) => ({
            ...state,
            submitStatus: stage.message,
            agentStageMessages: stage.message && !state.agentStageMessages.includes(stage.message)
              ? [...state.agentStageMessages, stage.message]
              : state.agentStageMessages,
          }))
        },
        onDelta: (chunk: string) => {
          set((state) => ({
            ...state,
            streamPreview: `${state.streamPreview}${chunk}`.slice(0, STREAM_PREVIEW_MAX),
          }))
        },
      }

      const response = get().submitType === 'voice' && payload.voiceAsset && payload.voiceQuality?.status !== 'rejected'
        ? await api.submitVoiceInterviewStream(token, sessionId, {
          content: answer,
          transcript: payload.voiceTranscript || answer,
          asset_id: payload.voiceAsset.id,
          duration_seconds: payload.voiceDuration,
          source: inferVoiceSource(answer, payload.voiceTranscript),
          confirmed_transcript: true,
        }, streamHandlers)
        : get().session?.status.startsWith('follow_up')
          ? await api.answerFollowupStream(token, sessionId, answer, streamHandlers)
          : await api.submitInterviewStream(token, sessionId, answer, streamHandlers)

      set((state) => ({
        ...state,
        session: response.session,
        lastEvaluation: response.evaluation,
        agentTrace: response.evaluation.agent_trace ?? null,
        submitStatus: '本轮评分已生成',
        streamPreview: buildSafeEvaluationPreview(response.evaluation),
        isSubmitting: false,
        voiceAsset: null,
        voiceStatus: '',
        voiceQuality: null,
        voiceTranscript: '',
        voiceConfirmed: false,
        voiceDuration: undefined,
        uploadProgress: 0,
        submitType: 'text',
      }))

      return { session_status: response.session_status }
    } catch (err) {
      set((state) => ({
        ...state,
        isSubmitting: false,
        submitError: err instanceof Error ? err.message : '评分生成失败，请重试',
        submitStatus: '评分生成失败，请重试',
      }))
      throw err
    }
  },

  uploadVoiceFile: async (token, sessionId, file) => {
    set((state) => ({
      ...state,
      voiceAsset: null,
      voiceQuality: null,
      voiceTranscript: '',
      voiceConfirmed: false,
      voiceDuration: undefined,
      uploadProgress: 15,
      isVoiceBusy: true,
      voiceStatus: '上传语音资源',
      voiceSessionExpired: false,
      submitError: '',
    }))
    try {
      const asset = await api.uploadVoiceAsset(token, file)
      set((state) => ({ ...state, voiceAsset: asset, uploadProgress: 65, voiceStatus: '生成转写草稿' }))
      const transcript = await api.transcribeInterviewVoice(token, sessionId, { asset_id: asset.id })
      const nextStatus = transcript.quality.status === 'needs_review'
        ? '已生成转写草稿，请确认后提交'
        : `已生成转写草稿：${asset.filename || '语音答案'}`
      set((state) => ({
        ...state,
        uploadProgress: 100,
        voiceQuality: transcript.quality,
        voiceTranscript: transcript.transcript,
        voiceDuration: transcript.duration_seconds,
        voiceStatus: transcript.quality.status === 'rejected'
          ? '已生成转写，但质检未通过；可修改为文本回答后提交'
          : nextStatus,
        submitType: transcript.quality.status === 'rejected' ? 'text' : 'voice',
        isVoiceBusy: false,
      }))
      return transcript.transcript
    } catch (err) {
      const message = err instanceof Error ? err.message : '语音上传失败'
      const unavailable = isInterviewSessionUnavailableError(message)
      set((state) => ({
        ...state,
        isVoiceBusy: false,
        uploadProgress: 0,
        voiceStatus: unavailable ? '' : message,
        voiceQuality: unavailable ? null : rejectedVoiceQuality(message),
        voiceSessionExpired: unavailable,
        submitError: unavailable ? '面试会话已失效，请重新开始面试' : state.submitError,
        submitStatus: unavailable ? '面试会话已失效，请重新开始面试' : state.submitStatus,
      }))
      throw err
    }
  },

  confirmVoiceTranscript: () => set((state) => ({
    ...state,
    voiceConfirmed: true,
    submitError: '',
    submitStatus: '',
    voiceStatus: '转写文本已确认，可提交评分',
  })),

  updateVoiceEditedState: (statusMessage) => set((state) => ({
    ...state,
    voiceConfirmed: false,
    submitError: '',
    submitStatus: state.submitType === 'voice' ? '等待确认转写文本' : state.submitStatus,
    voiceStatus: state.submitType === 'voice' ? (statusMessage || '转写文本已修改，请重新确认') : state.voiceStatus,
  })),

  rejectVoiceDraft: (reason) => set((state) => ({
    ...state,
    voiceAsset: null,
    voiceStatus: reason,
    voiceQuality: rejectedVoiceQuality(reason),
    voiceTranscript: '',
    voiceConfirmed: false,
    voiceDuration: undefined,
    uploadProgress: 0,
    isVoiceBusy: false,
    submitType: 'text',
    voiceSessionExpired: false,
  })),

  setSubmitFeedback: (patch) => set((state) => ({
    ...state,
    submitStatus: patch.submitStatus ?? state.submitStatus,
    submitError: patch.submitError ?? state.submitError,
    streamPreview: patch.streamPreview ?? state.streamPreview,
  })),

  clearVoiceDraft: () => set((state) => ({
    ...state,
    voiceAsset: null,
    voiceStatus: '',
    voiceQuality: null,
    voiceTranscript: '',
    voiceConfirmed: false,
    voiceDuration: undefined,
    uploadProgress: 0,
    isVoiceBusy: false,
    submitType: 'text',
  })),

  clear: () => set(emptyState()),
}))

function rejectedVoiceQuality(reason: string): VoiceQualityResult {
  return {
    detected_language: 'unknown',
    stt_confidence: 0,
    topic_relevance_score: 0,
    keyword_hits: [],
    transcript_suggestions: [],
    reasons: [reason],
    status: 'rejected',
  }
}

function inferVoiceSource(content: string, transcript: string): 'voice_transcript' | 'voice_edited' {
  const normalizedContent = content.trim()
  const normalizedTranscript = transcript.trim()
  if (!normalizedTranscript || normalizedContent === normalizedTranscript) return 'voice_transcript'
  const anchor = normalizedTranscript.slice(0, Math.min(18, normalizedTranscript.length))
  return anchor && normalizedContent.includes(anchor) ? 'voice_transcript' : 'voice_edited'
}

function evaluationPlainText(evaluation: InterviewEvaluation) {
  const parts = [`总分 ${evaluation.total_score} 分`]
  if (evaluation.highlights?.length) {
    parts.push(`亮点：${evaluation.highlights.join('；')}`)
  }
  if (evaluation.deficiencies?.length) {
    parts.push(`待改进：${evaluation.deficiencies.join('；')}`)
  }
  if (evaluation.follow_up_triggered && evaluation.follow_up_question) {
    parts.push(`追问：${evaluation.follow_up_question}`)
  }
  return parts.join('\n').slice(0, STREAM_PREVIEW_MAX)
}

function buildSafeEvaluationPreview(evaluation: InterviewEvaluation) {
  const preview = evaluationPlainText(evaluation)
  if (evaluation.agent_trace?.steps?.length) {
    return `${preview}\nAgent 已执行 ${evaluation.agent_trace.steps.length} 个安全步骤`.slice(0, STREAM_PREVIEW_MAX)
  }
  return preview
}

function isInterviewSessionUnavailableError(message: string) {
  const normalized = message.trim().toLowerCase()
  return normalized === 'interview session not found' || normalized === 'interview session is already completed'
}
