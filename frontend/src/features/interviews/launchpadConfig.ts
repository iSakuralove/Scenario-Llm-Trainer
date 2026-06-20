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
  difficulty: 'L2' | 'L3' | 'L4' | 'L5'
  questionType: InterviewQuestionType
  summary: string
  details: string[]
}

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
  { value: 'java', label: 'Java', group: '首期开放', note: 'L2 / L3 训练入口' },
  { value: 'database', label: '数据库', group: '首期开放', note: 'L2 / L3 训练入口' },
  { value: 'cache', label: '缓存', group: '首期开放', note: 'L2 / L3 训练入口' },
  { value: 'ai_llm', label: 'AI / LLM', group: '首期开放', note: 'L2 / L3 训练入口' },
]

export const interviewLaunchTracks: InterviewLaunchTrack[] = [
  {
    id: 'java-l2-principle',
    title: 'Java L2',
    domain: 'java',
    domainLabel: 'Java',
    difficulty: 'L2',
    questionType: 'principle',
    summary: '面向初级岗位的 Java 基础语法、集合和面向对象问答。',
    details: ['原理问答', '基础表达', '适合校招和 0-1 年经验'],
  },
  {
    id: 'java-l3-scenario',
    title: 'Java L3',
    domain: 'java',
    domainLabel: 'Java',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向进阶岗位的对象创建、异常处理和并发问题排查。',
    details: ['情景分析', '并发基础', '适合校招后与 1 年左右经验'],
  },
  {
    id: 'database-l2-principle',
    title: '数据库 L2',
    domain: 'database',
    domainLabel: '数据库',
    difficulty: 'L2',
    questionType: 'principle',
    summary: '面向初级岗位的索引、事务和表结构基础问答。',
    details: ['原理问答', '事务基础', '适合 0-1 年经验'],
  },
  {
    id: 'database-l3-scenario',
    title: '数据库 L3',
    domain: 'database',
    domainLabel: '数据库',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向进阶岗位的慢查询、索引和回滚方案排查。',
    details: ['情景分析', '慢查询定位', '回滚方案'],
  },
  {
    id: 'cache-l2-principle',
    title: '缓存 L2',
    domain: 'cache',
    domainLabel: '缓存',
    difficulty: 'L2',
    questionType: 'principle',
    summary: '面向初级岗位的缓存命中、过期和基础一致性问答。',
    details: ['原理问答', '缓存基础', '适合 0-1 年经验'],
  },
  {
    id: 'cache-l3-scenario',
    title: '缓存 L3',
    domain: 'cache',
    domainLabel: '缓存',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向进阶岗位的缓存击穿、穿透、雪崩与一致性排查。',
    details: ['情景分析', '缓存治理', '热点流量'],
  },
  {
    id: 'ai-llm-l2-principle',
    title: 'AI / LLM L2',
    domain: 'ai_llm',
    domainLabel: 'AI / LLM',
    difficulty: 'L2',
    questionType: 'principle',
    summary: '面向初级岗位的提示词、RAG 和模型使用基础问答。',
    details: ['原理问答', 'RAG 基础', '适合 0-1 年经验'],
  },
  {
    id: 'ai-llm-l3-scenario',
    title: 'AI / LLM L3',
    domain: 'ai_llm',
    domainLabel: 'AI / LLM',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向进阶岗位的 RAG 链路、提示词稳定性与模型应用治理分析。',
    details: ['情景分析', 'RAG 链路', '应用治理'],
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
