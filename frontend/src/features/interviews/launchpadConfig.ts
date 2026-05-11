export type InterviewQuestionType = 'scenario_analysis' | 'principle'

export interface InterviewLevelOption {
  value: 'L3' | 'L4' | 'L5'
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
  difficulty: 'L3' | 'L4' | 'L5'
  questionType: InterviewQuestionType
  summary: string
  details: string[]
}

export const interviewLevels: InterviewLevelOption[] = [
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
  { value: 'database', label: '数据库', group: '基础能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'network', label: '网络', group: '基础能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'os', label: '操作系统', group: '基础能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'security', label: '安全', group: '工程实践', note: 'deepseek-v4-flash 基线题库' },
  { value: 'devops', label: 'DevOps', group: '工程实践', note: 'deepseek-v4-flash 基线题库' },
  { value: 'backend', label: '后端工程', group: '工程实践', note: 'deepseek-v4-flash 基线题库' },
  { value: 'distributed', label: '分布式系统', group: '系统能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'cloud-native', label: '云原生', group: '系统能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'mq-cache', label: '缓存与消息队列', group: '系统能力', note: 'deepseek-v4-flash 基线题库' },
  { value: 'observability', label: '可观测性', group: '稳定性', note: 'deepseek-v4-flash 基线题库' },
  { value: 'performance', label: '性能优化', group: '稳定性', note: 'deepseek-v4-flash 基线题库' },
  { value: 'architecture', label: '架构设计', group: '系统能力', note: 'deepseek-v4-flash 基线题库' },
]

export const interviewLaunchTracks: InterviewLaunchTrack[] = [
  {
    id: 'database-l3-scenario',
    title: '数据库 L3',
    domain: 'database',
    domainLabel: '数据库',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向初级工程师的慢查询、索引和回滚方案排查。',
    details: ['情景分析', '最多 3 轮追问', '适合校招和初级岗位'],
  },
  {
    id: 'network-l3-scenario',
    title: '网络 L3',
    domain: 'network',
    domainLabel: '网络',
    difficulty: 'L3',
    questionType: 'scenario_analysis',
    summary: '面向初级工程师的跨服务调用超时定位。',
    details: ['情景分析', '链路定位', 'DNS/VIP/健康检查'],
  },
  {
    id: 'os-l3-principle',
    title: '操作系统 L3',
    domain: 'os',
    domainLabel: '操作系统',
    difficulty: 'L3',
    questionType: 'principle',
    summary: '面向初级工程师的 load、IO wait 与进程状态分析。',
    details: ['原理问答', 'Linux 基础', '系统负载诊断'],
  },
  {
    id: 'security-l4-scenario',
    title: '安全 L4',
    domain: 'security',
    domainLabel: '安全',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的访问密钥泄露与遏制处置分析。',
    details: ['情景分析', '密钥轮换', '风险面收敛'],
  },
  {
    id: 'devops-l4-scenario',
    title: 'DevOps L4',
    domain: 'devops',
    domainLabel: 'DevOps',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的发布失败、回滚与流水线修复分析。',
    details: ['情景分析', 'CI/CD', '回滚演练'],
  },
  {
    id: 'backend-l4-scenario',
    title: '后端工程 L4',
    domain: 'backend',
    domainLabel: '后端工程',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的接口幂等、重试与库存一致性分析。',
    details: ['情景分析', '并发控制', '一致性处理'],
  },
  {
    id: 'distributed-l4-scenario',
    title: '分布式系统 L4',
    domain: 'distributed',
    domainLabel: '分布式系统',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的分布式事务补偿与消息乱序排查。',
    details: ['情景分析', '补偿机制', '消息顺序'],
  },
  {
    id: 'cloud-native-l4-scenario',
    title: '云原生 L4',
    domain: 'cloud-native',
    domainLabel: '云原生',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的 Kubernetes 滚动发布故障与集群自愈分析。',
    details: ['情景分析', 'Kubernetes', '发布稳定性'],
  },
  {
    id: 'mq-cache-l4-scenario',
    title: '缓存与消息队列 L4',
    domain: 'mq-cache',
    domainLabel: '缓存与消息队列',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的缓存击穿与消息积压联合排查。',
    details: ['情景分析', '缓存策略', '积压治理'],
  },
  {
    id: 'observability-l4-scenario',
    title: '可观测性 L4',
    domain: 'observability',
    domainLabel: '可观测性',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的告警风暴、指标漂移与链路定位分析。',
    details: ['情景分析', '告警降噪', '指标与 Trace'],
  },
  {
    id: 'performance-l4-scenario',
    title: '性能优化 L4',
    domain: 'performance',
    domainLabel: '性能优化',
    difficulty: 'L4',
    questionType: 'scenario_analysis',
    summary: '面向中级工程师的 P99 抖动、线程池和热点流量优化分析。',
    details: ['情景分析', '性能瓶颈', '容量治理'],
  },
  {
    id: 'architecture-l5-principle',
    title: '架构设计 L5',
    domain: 'architecture',
    domainLabel: '架构设计',
    difficulty: 'L5',
    questionType: 'principle',
    summary: '面向高级工程师的多活容灾、一致性与演进治理设计问答。',
    details: ['设计问答', '多活架构', '演进治理'],
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
  '每轮追问与回答记录',
  '可打印/导出 PDF 的复盘报告',
]
