import type { InterviewDifficultyLevel, InterviewFocusArea } from '../../types'

export type InterviewQuestionType = 'scenario_analysis' | 'principle'

export interface InterviewLevelOption {
  value: 'L2' | 'L3' | 'L4' | 'L5'
  role: string
  audience: string
  focus: string
}

export interface InterviewDomainOption {
  value: string
  label: string
  group: string
  note: string
}

export interface InterviewLaunchTrack {
  id: string
  title: string
  domain: string
  domainLabel: string
  category: string
  difficulty: 'L2' | 'L3' | 'L4' | 'L5'
  questionType: InterviewQuestionType
  questionRole: 'opening' | 'followup' | 'mixed'
  tags: string[]
  availabilityState: 'available' | 'indexing' | 'fallback'
  vectorStatusSummary: string
  unavailableReason?: string
  summary: string
  details: string[]
}

export interface InterviewDifficultyLevelOption {
  value: InterviewDifficultyLevel
  label: string
  note: string
}

export interface InterviewFocusAreaOption {
  value: InterviewFocusArea
  label: string
  note: string
}

export const interviewDifficultyLevelOptions: InterviewDifficultyLevelOption[] = [
  { value: 'standard', label: '标准', note: '按岗位级别正常追问' },
  { value: 'foundation', label: '偏基础', note: '先确认概念和表达结构' },
  { value: 'challenge', label: '偏挑战', note: '强化边界、权衡与风险' },
]

export const interviewFocusAreaOptions: InterviewFocusAreaOption[] = [
  { value: 'technical_accuracy', label: '技术准确性', note: '概念、命令、机制和判断' },
  { value: 'logical_completeness', label: '逻辑完整性', note: '排查链路、因果关系和步骤' },
  { value: 'solution_feasibility', label: '方案可落地性', note: '验证、回滚、风险控制' },
  { value: 'depth_breadth', label: '深度与广度', note: '原理、边界情况和关联知识' },
  { value: 'expression_structure', label: '表达结构', note: '层次、重点和术语组织' },
]

export const defaultInterviewFocusAreas: InterviewFocusArea[] = [
  'technical_accuracy',
  'logical_completeness',
  'solution_feasibility',
]

export const interviewLevels: InterviewLevelOption[] = [
  {
    value: 'L2',
    role: '初级工程师',
    audience: '0-1年经验',
    focus: '基础概念、表达清晰度、任务拆解与简单排查',
  },
  {
    value: 'L3',
    role: '初级工程师',
    audience: '应届生/校招',
    focus: '基础数据结构与算法、编程基础、学习能力',
  },
  {
    value: 'L4',
    role: '中级工程师',
    audience: '1-3年经验',
    focus: '独立负责功能、代码质量、技术方案执行',
  },
  {
    value: 'L5',
    role: '高级工程师',
    audience: '3-5年以上',
    focus: '系统设计、技术规划、跨团队协作、辅导他人',
  },
]

export const interviewDomains: InterviewDomainOption[] = [
  { value: 'database', label: '数据库', group: '面试题', note: 'L3 情景分析' },
  { value: 'network', label: '网络', group: '面试题', note: 'L3 情景分析' },
  { value: 'os', label: '操作系统', group: '面试题', note: 'L3 原理问答' },
  { value: 'security', label: '安全', group: '面试题', note: 'L4 情景分析' },
  { value: 'devops', label: 'DevOps', group: '面试题', note: 'L4 情景分析' },
]

export const interviewLaunchTracks: InterviewLaunchTrack[] = [
  {
    id: 'interview-db-slow-query',
    title: '如何定位 MySQL 慢查询',
    domain: 'database',
    domainLabel: '数据库',
    category: 'database',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    questionRole: 'opening',
    tags: [],
    availabilityState: 'fallback',
    vectorStatusSummary: 'compatibility_seed',
    summary: '如何定位 MySQL 慢查询',
    details: ['情景分析', '慢查询定位', '索引与回滚'],
  },
  {
    id: 'interview-network-timeout',
    title: '如何排查跨服务调用超时',
    domain: 'network',
    domainLabel: '网络',
    category: 'network',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    questionRole: 'opening',
    tags: [],
    availabilityState: 'fallback',
    vectorStatusSummary: 'compatibility_seed',
    summary: '如何排查跨服务调用超时',
    details: ['情景分析', '链路定位', '超时边界'],
  },
  {
    id: 'interview-os-load',
    title: 'load average 高但 CPU 不高怎么排查',
    domain: 'os',
    domainLabel: '操作系统',
    category: 'os',
    difficulty: 'L3',
    questionType: 'principle',
    questionRole: 'opening',
    tags: [],
    availabilityState: 'fallback',
    vectorStatusSummary: 'compatibility_seed',
    summary: 'load average 高但 CPU 不高怎么排查',
    details: ['原理问答', 'D 状态进程', 'IO wait'],
  },
  {
    id: 'interview-security-ak-leak',
    title: '访问密钥泄露后如何遏制风险',
    domain: 'security',
    domainLabel: '安全',
    category: 'security',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    questionRole: 'opening',
    tags: [],
    availabilityState: 'fallback',
    vectorStatusSummary: 'compatibility_seed',
    summary: '访问密钥泄露后如何遏制风险',
    details: ['情景分析', '风险遏制', '密钥轮换'],
  },
  {
    id: 'interview-devops-release-rollback',
    title: '发布失败后如何回滚并恢复流水线',
    domain: 'devops',
    domainLabel: 'DevOps',
    category: 'devops',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    questionRole: 'opening',
    tags: [],
    availabilityState: 'fallback',
    vectorStatusSummary: 'compatibility_seed',
    summary: '发布失败后如何回滚并恢复流水线',
    details: ['情景分析', '故障回滚', '流水线恢复'],
  },
]

export const interviewFlowSteps = [
  { title: '选择轨道', description: '按当前岗位目标进入对应领域训练。' },
  { title: '结构化作答', description: '支持文本、Markdown 和语音转写后确认。' },
  { title: '五维评分', description: '后端规则决定是否追问并生成评分过程。' },
  { title: '报告复盘', description: '汇总最终分、雷达图、轮次对比和改进建议。' },
]

export const interviewScoreDimensions = [
  '技术准确性',
  '逻辑完整性',
  '方案可落地性',
  '深度与广度',
  '表达结构',
]

export const interviewReportOutputs = [
  '最终分与岗位级别匹配度',
  '五维能力雷达',
  '追问策略摘要',
  '每轮追问与回答记录',
  '建议补强方向',
  '可打印/导出 PDF 的复盘报告',
]
