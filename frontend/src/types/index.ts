export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

export interface User {
  id: string
  username: string
  email: string
  role: UserRole
  profile: UserProfile
  created_at: string
}

export type UserRole = 'student' | 'instructor' | 'admin'

export interface UserProfile {
  target_level: string
  preferred_domains: string[]
  capability_radar: Record<string, number>
  weak_points: WeakPoint[]
  total_stats: TotalStats
  checkin_dates?: string[]
  last_checkin_date?: string
  updated_at: string
}

export interface WeakPoint {
  domain: string
  topic: string
  last_score: number
  suggested_questions: string[]
}

export interface TotalStats {
  scenarios_solved: number
  interviews_taken: number
  average_score: number
  streak_days: number
}

export interface ScenarioQuestion {
  id: string
  title: string
  description: string
  domain: string
  difficulty: string
  scenario_type: string
  tags: string[]
  content: ScenarioContent
  status: string
  source: string
  created_by: string
  creator_role?: UserRole | string
  version: number
  is_sanitized: boolean
}

export interface ScenarioGenerationConstraints {
  title?: string
  description?: string
  topic_scope?: string[]
  root_cause_hint?: string
  evidence_hints?: string[]
  clue_hints?: string[]
}

export interface ScenarioContent {
  root_cause?: string
  root_cause_keywords?: string[]
  key_evidence?: string[]
  standard_procedure?: string[]
  reveal_strategy: RevealStrategy
  architecture_diagram: string
  architecture_diagram_spec?: ArchitectureDiagramSpec
  diagram_status?: 'validated' | 'normalized' | 'fallback' | string
  diagram_warnings?: string[]
  reference_links: string[]
}

export interface ArchitectureDiagramSpec {
  direction?: string
  nodes?: ArchitectureDiagramNode[]
  edges?: ArchitectureDiagramEdge[]
}

export interface ArchitectureDiagramNode {
  id: string
  label: string
  kind?: string
  detail?: string
}

export interface ArchitectureDiagramEdge {
  from: string
  to: string
  label?: string
  style?: 'solid' | 'dotted' | string
}

export interface RevealStrategy {
  surface_clues: Clue[]
  deep_clues: Clue[]
  distractors: Clue[]
}

export interface Clue {
  clue_id: string
  trigger_keywords: string[]
  prerequisite_clues?: string[]
  content: string
  is_distractor: boolean
  recommended_next_ask?: string
}

export interface ScenarioSession {
  id: string
  user_id: string
  question_id: string
  status: string
  current_turn: number
  max_turns: number
  revealed_clue_ids: string[]
  user_answer?: string
  evaluation_result?: ScenarioEvaluation
  score?: ScenarioScore
  question_snapshot: ScenarioQuestion
  hint_level: number
  no_new_clue_streak: number
  conversation_summary?: string
  started_at: string
  last_active_at: string
}

export interface ScenarioMessage {
  id: string
  session_id: string
  turn_number: number
  role: string
  user_content: string
  assistant_content: string
  response_meta: ResponseMeta
  created_at: string
}

export interface ResponseMeta {
  response_type: string
  revealed_clue_id?: string
  hint_level: number
  is_answer_leak: boolean
  is_distractor: boolean
  is_sanitized: boolean
  provider?: string
  validated?: boolean
  fallback_used?: boolean
  safety_rewritten?: boolean
  semantic_decision?: string
  input_quality?: string
  agent_intent?: string
  root_similarity?: number
  clue_similarity?: number
  matched_clue_id?: string
  embedding_model?: string
  embedding_fallback_used?: boolean
  agent_trace?: AgentTrace
}

export interface ScenarioSessionDetailResponse {
  session: ScenarioSession
  messages: ScenarioMessage[]
}

export interface AgentTrace {
  run_id: string
  agent: string
  mode: string
  steps: AgentStep[]
  tool_count: number
  started_at: string
  finished_at: string
}

export interface AgentStep {
  name: string
  kind: string
  status: string
  summary: string
  metadata?: Record<string, string>
  started_at: string
  ended_at: string
}

export interface AIStatus {
  provider: string
  model: string
  base_url?: string
  fallback: boolean
  configured_provider?: string
  configured_model?: string
  init_error?: string
  stream_enabled?: boolean
  router_version?: string
  healthy?: boolean
  health?: string
  transport?: string
  last_trace_id?: string
  last_task?: string
  last_latency_ms?: number
  last_error_type?: string
  last_error?: string
  last_error_at?: string
  last_fallback_reason?: string
  last_fallback_error?: string
  capability?: ProviderCapability
  telemetry?: RouterTelemetry
  provider_pool?: ProviderPoolStatus
}

export interface ProviderCapability {
  provider: string
  model: string
  transport: string
  supports_streaming: boolean
  supports_json: boolean
  supports_tools: boolean
  temperature: boolean
  top_p: boolean
  top_k: boolean
  max_tokens: number
  cost_tier: string
  health: string
  supported_tasks: string[]
}

export interface ProviderPoolStatus {
  active_provider: string
  fallback_order: string[]
  degraded_count: number
  providers: ProviderPoolProvider[]
  recent_attempts: FallbackAttempt[]
  updated_at: string
}

export interface ProviderPoolProvider extends ProviderCapability {
  provider: string
  model: string
  status: string
  health: string
  priority: number
  enabled: boolean
  last_checked_at?: string
  last_error_type?: string
  last_error?: string
  call_count: number
  fallback_reason?: string
  rate_limit?: string | {
    enabled?: boolean
    status?: string
    limit?: number
    in_flight?: number
    remaining?: number
    reset_at?: string
    detail?: string
  }
}

export interface FallbackAttempt {
  provider: string
  model: string
  success: boolean
  error_type?: string
  fallback_reason?: string
  latency_ms?: number
  started_at: string
  completed_at?: string
}

export interface RouterTelemetry {
  total_calls: number
  successful_calls: number
  failed_calls: number
  fallback_calls: number
  stream_calls: number
  json_calls: number
  safety_rewrites: number
  validation_errors: number
  provider_calls: Record<string, number>
  task_calls: Record<string, number>
  recent_attempts?: FallbackAttempt[]
  recent_decisions: RouterDecision[]
  last_decision?: RouterDecision
  last_error?: string
  last_error_type?: string
  last_fallback_reason?: string
  last_fallback_error?: string
  last_error_at?: string
  updated_at: string
}

export interface RouterDecision {
  trace_id: string
  task: string
  provider: string
  model: string
  schema?: string
  prompt?: string
  prompt_template?: RouterPromptTemplate
  output_mode: string
  stream: boolean
  safety_policy: string
  fallback_chain: string[]
  fallback_attempts?: FallbackAttempt[]
  context: ContextWindow
  capability: ProviderCapability
  provider_health?: {
    provider?: string
    status?: string
    last_checked_at?: string
    consecutive_failures?: number
    last_error_type?: string
    last_error?: string
  }
  rate_limit?: ProviderPoolProvider['rate_limit']
  output: RouterOutputTelemetry
  validation: ValidationResult
  safety: SafetyVerdict
  started_at: string
  completed_at?: string
  latency_ms?: number
  status: string
  error_type?: string
  error_message?: string
}

export interface RouterOutputTelemetry {
  parse_status?: string
  repair_used?: boolean
}

export interface RouterPromptTemplate {
  name: string
  version: string
  task: string
  schema?: string
  managed_by: string
}

export interface ContextWindow {
  version: string
  strategy: string
  original_messages: number
  retained_messages: number
  summary_retained: boolean
  key_facts_retained?: string[]
  estimated_input_tokens: number
  max_input_tokens: number
  compressed: boolean
}

export interface ValidationResult {
  required: boolean
  schema?: string
  status: string
  detail?: string
}

export interface SafetyVerdict {
  policy: string
  status: string
  detail?: string
  blocked: boolean
  rewrite_used?: boolean
}

export interface AIJob {
  id: string
  user_id: string
  kind: string
  status: 'queued' | 'running' | 'completed' | 'failed' | 'canceled'
  stage: string
  progress: number
  error_message?: string
  provider?: string
  model?: string
  validated: boolean
  fallback_used: boolean
  result_question_id?: string
  created_at: string
  started_at?: string
  completed_at?: string
  updated_at: string
}

export interface LearningPlan {
  generated_at: string
  summary: string
  target_level: string
  focus_domains: string[]
  domain_insights: LearningDomainInsight[]
  recommendations: LearningRecommendation[]
  review_plan: ReviewPlanItem[]
}

export interface LearningDomainInsight {
  domain: string
  score: number
  level: string
  trend: string
  completed_count: number
  last_score?: number
  reason: string
}

export interface LearningRecommendation {
  id: string
  kind: string
  domain: string
  title: string
  description: string
  difficulty: string
  priority: number
  reason: string
  action_label: string
  action_path: string
  question?: ScenarioQuestion
}

export interface ReviewPlanItem {
  day_label: string
  domain: string
  focus: string
  actions: string[]
  estimated_minutes: number
  target_score: number
  question_ids: string[]
  source_kind?: string
  source_id?: string
  reason?: string
}

export interface ReviewCalendar {
  generated_at: string
  checkin_dates: string[]
  streak_days: number
  today_checked: boolean
  today: string
  review_plan: ReviewPlanItem[]
  focus_domains: string[]
  next_action: string
}

export interface CheckinResult {
  checked_in: boolean
  already_checked_in: boolean
  checkin_date: string
  streak_days: number
  next_action: string
}

export interface SystemStatus {
  generated_at: string
  services: SystemServiceStatus[]
  ai: AIStatus
  store?: StoreStatus
  ai_config?: AIConfig
  sensitive_detection?: SensitiveDetectionStatus
  prompt_templates?: PromptTemplateStatus[]
  schema_validators?: SchemaValidatorStatus[]
  rate_limit?: { enabled: boolean; detail: string }
  audit_summary?: AuditSummary
  agent_summary?: AgentSummary
  recent_ai_errors?: Array<{
    action: string
    resource_id: string
    created_at: string
  }>
  counts: {
    users: number
    scenarios: number
    active_scenarios: number
    community_posts: number
    pending_ugc: number
    generated_scenarios?: number
    ai_jobs?: number
  }
  demo_accounts: Array<{ role: string; username: string; purpose: string }>
  runbook: Array<{ title: string; command: string }>
}

export interface StoreStatus {
  mode: string
  persistent: boolean
  warning?: string
}

export interface AgentSummary {
  total_recent: number
  latest_agent: string
  latest_run_at?: string
  failed_recent: number
  safety_rewritten_recent: number
  flagged_recent?: number
  per_agent?: AgentSummaryItem[]
}

export interface AgentSummaryItem {
  agent: string
  total_recent?: number
  failed_recent?: number
  safety_rewritten_recent?: number
  flagged_recent?: number
  latest_run_at?: string
  latest_status?: string
}

export interface SchemaValidatorStatus {
  name: string
  schema_name?: string
  version?: string
  task?: string
  description?: string
  target: string
  status?: string
}

export interface SensitiveDetectionStatus {
  status: string
  provider?: string
  model?: string
  fallback_count?: number
  fallback_used?: boolean
  rule_enabled?: boolean
  model_enabled?: boolean
  schema?: string
  detail?: string
  checked_actions?: string[]
}

export interface SystemServiceStatus {
  name: string
  status: 'ok' | 'degraded' | 'fallback' | 'disabled' | 'missing' | string
  detail: string
}

export interface ScenarioEvaluation {
  is_correct: boolean
  match_degree: number
  missing_points: string[]
  standard_procedure: string[]
  scoring_report?: ScenarioScoringReport
}

export interface ScenarioScore {
  efficiency: number
  accuracy: number
  clue_usage: number
  total: number
}

export interface ScenarioScoringReport {
  overall_score: number
  root_cause_similarity: number
  evidence_chain_score: number
  procedure_coverage_score: number
  clue_usage_score: number
  reasoning_depth_score: number
  efficiency_score: number
  matched_documents: ScenarioMatchedDocument[]
  evidence_events: ScenarioEvidenceEvent[]
  penalties: string[]
  score_explanation: string
}

export interface ScenarioMatchedDocument {
  doc_type: string
  doc_key: string
  snippet: string
  score: number
}

export interface ScenarioEvidenceEvent {
  turn_number: number
  event_type: string
  text: string
  best_doc_type?: string
  best_doc_key?: string
  score: number
}

export interface InterviewQuestion {
  id: string
  title: string
  description: string
  domain: string
  difficulty: string
  question_type: string
  evaluation_dimensions: EvaluationDimension[]
  follow_up_strategies: FollowUpStrategy[]
}

export interface EvaluationDimension {
  name: string
  weight: number
  criteria: string
}

export interface FollowUpStrategy {
  trigger_condition: string
  question_template: string
  type: string
}

export interface InterviewSession {
  id: string
  user_id: string
  question_id: string
  status: string
  current_round: number
  max_rounds: number
  submissions: InterviewSubmission[]
  evaluations: InterviewEvaluation[]
  follow_up_question?: string
  final_score?: number
  final_report?: string
  started_at?: string
  ended_at?: string
}

export interface InterviewSessionDetailResponse {
  session: InterviewSession
  question: InterviewQuestion
}

export interface InterviewSubmission {
  round: number
  content: string
  type: 'text' | 'voice'
  source?: 'text' | 'voice_transcript' | 'voice_edited'
  quality_flag?: string
  asset_id?: string
  asset_url?: string
  asset?: Asset
  transcript?: string
  duration_seconds?: number
  voice_quality?: VoiceQualityResult
  submitted_at: string
}

export interface VoiceQualityResult {
  detected_language: string
  stt_confidence: number
  topic_relevance_score: number
  keyword_hits: string[]
  suggestions?: TranscriptSuggestion[]
  transcript_suggestions?: TranscriptSuggestion[]
  reasons: string[]
  status: 'draft_ready' | 'needs_review' | 'rejected' | string
}

export interface TranscriptSuggestion {
  original: string
  suggested: string
  reason?: string
}

export interface InterviewEvaluation {
  round: number
  total_score: number
  dimension_scores: Record<string, number>
  is_passed: boolean
  highlights: string[]
  deficiencies: string[]
  follow_up_triggered: boolean
  follow_up_question?: string
  follow_up_type?: string
  agent_trace?: AgentTrace
  created_at: string
}

export interface CommunityPost {
  id: string
  user_id: string
  author_username?: string
  title: string
  raw_content: string
  domain: string
  tags: string[]
  forked_from_scenario_id?: string
  ai_structured_content: ScenarioContent
  edited_structured_content?: ScenarioContent
  review_history: ReviewHistoryItem[]
  sensitive_check?: SensitiveCheckResult
  moderation_summary?: CommunityModerationSummary
  converted_question_id?: string
  status: string
  reviewed_by?: string
  reviewed_at?: string
  review_note?: string
  finalized_by?: string
  finalized_at?: string
  final_note?: string
  created_at: string
  updated_at?: string
}

export interface CommunityModerationSummary {
  status?: string
  risk_level?: string
  recommendation?: string
  safe_summary?: string
  safe_risk_note?: string
  safe_action_hint?: string
  safe_labels?: string[]
  suggested_note?: string
  reasons?: string[]
  flagged?: boolean
}

export interface ReviewHistoryItem {
  id: string
  actor_id: string
  action: string
  from_status?: string
  to_status: string
  note?: string
  content?: ScenarioContent
  created_at: string
}

export interface Asset {
  id: string
  user_id: string
  kind: string
  filename: string
  mime_type: string
  size: number
  storage_key: string
  url: string
  content_url?: string
  checksum?: string
  created_at: string
}

export interface PromptTemplate {
  name: string
  task: string
  default: string
  content: string
  render_engine?: string
  updated_by?: string
  updated_at: string
  is_modified: boolean
  validator: string
}

export interface PromptTemplateStatus {
  name: string
  task: string
  render_engine?: string
  updated_by?: string
  updated_at: string
  is_modified: boolean
  validator: string
  summary: string
  content_length: number
  default_length: number
}

export interface AIConfig {
  provider: string
  model: string
  base_url?: string
  temperature: number
  top_p: number
  top_k: number
  max_tokens: number
  stream_enabled: boolean
  fallback_model: string
  updated_by?: string
  updated_at: string
}

export interface SensitiveFinding {
  type: string
  field: string
  excerpt: string
  severity: string
  suggestion: string
  source?: string
  confidence?: number
  redacted_excerpt?: string
}

export interface SensitiveCheckResult {
  status: string
  sanitized: boolean
  findings: SensitiveFinding[]
  checked_at: string
  source?: string
  risk_level?: string
  blocked?: boolean
  fallback_used?: boolean
  summary?: string
}

export interface AuditEvent {
  id: string
  actor_id?: string
  action: string
  resource_type: string
  resource_id?: string
  ip_address?: string
  user_agent?: string
  metadata: Record<string, string>
  created_at: string
}

export interface AuditSummary {
  total_recent: number
  by_action: Record<string, number>
  latest: AuditEvent[]
}
