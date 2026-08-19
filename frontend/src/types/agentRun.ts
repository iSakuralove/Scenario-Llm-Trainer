export type ScenarioRunEventStatus = 'started' | 'running' | 'completed' | 'failed'

export type ScenarioRunEventKind =
  | 'user_message'
  | 'reasoning_summary_delta'
  | 'reasoning_summary_completed'
  | 'tool_started'
  | 'tool_result'
  | 'tool_completed'
  | 'response_summary'
  | 'mentor_buffered'
  | 'guard_passed'
  | 'proposal_approved'
  | 'reply_delta'
  | 'turn_completed'
  | 'turn_failed'

export interface ScenarioPublicReasoningSummary {
  stage: 'understanding_message' | 'checking_observations' | 'verifying_answer' | 'composing_reply'
  text: string
}

export interface ScenarioPublicAnswerComparison {
  tool: 'compare_answer' | string
  status: string
  user_points: string[]
  support_status: 'insufficiently_specific' | 'needs_more_evidence' | 'has_evidence_conflict' | 'evidence_consistent'
  next_action: string
}

export interface ScenarioToolEventPayload {
  name: string
  redacted_arguments: Record<string, string>
  duration_ms: number
  result?: ScenarioPublicAnswerComparison
}

export interface ScenarioRunEvent {
  request_id: string
  sequence: number
  kind: ScenarioRunEventKind
  status: ScenarioRunEventStatus
  text?: string
  summary?: string
  reasoning?: ScenarioPublicReasoningSummary
  tool?: ScenarioToolEventPayload
  error_code?: string
}
