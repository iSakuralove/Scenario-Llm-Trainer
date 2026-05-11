package store

import (
	"time"

	"situational-teaching/backend/internal/domain"
)

func seedDiagnosticScenarios(now time.Time) []domain.ScenarioQuestion {
	return []domain.ScenarioQuestion{
		{
			ID:           "scenario-db-index",
			Title:        "订单列表查询突然变慢",
			Description:  "订单列表接口在一次筛选条件扩展后，95 分位耗时从 200ms 上升到 4s 左右，应用 CPU 和内存都正常，数据库连接数也稳定。请通过提问逐步定位根因。",
			Domain:       "database",
			Difficulty:   "L3",
			ScenarioType: "performance",
			Tags:         []string{"MySQL", "索引", "慢查询"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now,
			UpdatedAt:    now,
			Content: domain.ScenarioContent{
				RootCause:         "订单列表新增 status 与 created_at 筛选后，没有补上能覆盖该组合的联合索引，导致查询退化为全表扫描并回表。",
				RootCauseKeywords: []string{"联合索引", "全表扫描", "回表", "status", "created_at"},
				KeyEvidence: []string{
					"慢查询日志里 rows_examined 明显上升，且只有订单列表接口变慢。",
					"EXPLAIN 显示 type=ALL，possible_keys 为空。",
					"最近一次发布新增了 status 与时间范围筛选。",
					"现有索引只覆盖了 user_id，没有覆盖新的查询条件。",
				},
				StandardProcedure: []string{
					"先确认接口耗时是否集中在数据库阶段。",
					"查看慢查询日志与 SQL 执行计划。",
					"核对最近发布是否修改了筛选条件。",
					"检查现有索引是否覆盖 where 条件与排序字段。",
					"补建联合索引并验证回归。",
					"整理根因与验证结果。",
				},
				ArchitectureDiagram: "graph TD\nA[Web API] --> B[Order Service]\nB --> C[(MySQL orders)]\nB --> D[Redis Cache]\nC --> E[Slow Query Log]",
				ReferenceLinks:      []string{"MySQL EXPLAIN", "联合索引左前缀原则"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "db-s1", TriggerKeywords: []string{"慢查询日志", "rows_examined", "查询变慢"}, Content: "慢查询日志显示订单列表接口的 rows_examined 持续升高。", RecommendedNextAsk: "继续确认执行计划和最近是否有筛选条件变更。"},
						{ClueID: "db-s2", TriggerKeywords: []string{"CPU", "内存", "连接数"}, Content: "数据库 CPU、内存和连接数都稳定，没有明显锁等待峰值。", RecommendedNextAsk: "继续追查 SQL 计划和索引覆盖情况。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "db-d1", TriggerKeywords: []string{"EXPLAIN", "执行计划", "possible_keys"}, PrerequisiteClues: []string{"db-s1"}, Content: "EXPLAIN 显示 type=ALL，possible_keys 为空，说明查询在走全表扫描。", RecommendedNextAsk: "可以把根因收束到索引设计。"},
						{ClueID: "db-d2", TriggerKeywords: []string{"发布", "筛选条件", "status"}, PrerequisiteClues: []string{"db-d1"}, Content: "最近发布把列表筛选条件从 user_id 扩展为 user_id + status + created_at，但索引没有同步调整。", RecommendedNextAsk: "可以整理根因并给出建索引方案。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "db-x1", TriggerKeywords: []string{"网络", "超时", "DNS"}, Content: "该问题不是网络链路或 DNS 异常，数据库直连与健康检查都正常。", IsDistractor: true},
					},
				},
			},
		},
		{
			ID:           "scenario-network-timeout",
			Title:        "支付回调跨机房间歇性超时",
			Description:  "支付回调服务跨机房调用库存接口时，过去两小时内出现少量超时。单机房日志未见异常，重试后大多成功。请通过提问逐步定位问题链路。",
			Domain:       "network",
			Difficulty:   "L3",
			ScenarioType: "troubleshooting",
			Tags:         []string{"DNS", "VIP", "跨机房"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now.Add(-time.Minute),
			UpdatedAt:    now.Add(-time.Minute),
			Content: domain.ScenarioContent{
				RootCause:         "内部 DNS 在故障切换后仍短时间返回了已降级的 VIP，叠加健康检查摘除延迟，导致部分跨机房请求命中了不健康实例。",
				RootCauseKeywords: []string{"DNS", "VIP", "健康检查", "不健康实例", "摘除延迟"},
				KeyEvidence: []string{
					"超时主要集中在同一个 VIP 上，其他 VIP 命中正常。",
					"DNS 解析结果在不同机房不一致。",
					"健康检查摘除存在延迟，异常实例在窗口期仍能被流量命中。",
				},
				StandardProcedure: []string{
					"先按目标 VIP 聚合失败请求。",
					"对比不同机房的 DNS 解析结果。",
					"检查健康检查与负载均衡摘除时延。",
					"确认是否存在灰度或故障切换窗口。",
					"摘除异常实例并验证恢复。",
					"整理回退与修复说明。",
				},
				ArchitectureDiagram: "graph TD\nA[Payment Callback] --> B[Internal DNS]\nB --> C[VIP A]\nB --> D[VIP B]\nC --> E[Inventory Pool]\nD --> F[Degraded Instance]",
				ReferenceLinks:      []string{"DNS 缓存与解析一致性", "负载均衡健康检查"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "net-s1", TriggerKeywords: []string{"超时比例", "目标 VIP", "调用分布"}, Content: "超时请求大多集中在同一个 VIP，别的入口基本正常。", RecommendedNextAsk: "继续对比 DNS 解析结果和健康检查状态。"},
						{ClueID: "net-s2", TriggerKeywords: []string{"跨机房", "重试", "成功率"}, Content: "跨机房调用更容易超时，但重试后大多可以成功。", RecommendedNextAsk: "继续看解析与摘除链路。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "net-d1", TriggerKeywords: []string{"DNS", "解析结果", "不一致"}, PrerequisiteClues: []string{"net-s2"}, Content: "两个机房解析同一域名得到的 VIP 不一致，异常机房仍返回了降级实例。", RecommendedNextAsk: "继续确认健康检查和摘除延迟。"},
						{ClueID: "net-d2", TriggerKeywords: []string{"健康检查", "摘除", "90秒"}, PrerequisiteClues: []string{"net-d1"}, Content: "健康检查摘除存在约 90 秒延迟，故障实例在这段窗口期仍会被命中。", RecommendedNextAsk: "可以整理根因并说明修复思路。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "net-x1", TriggerKeywords: []string{"CPU", "内存", "GC"}, Content: "宿主机资源稳定，问题并非应用算力不足。", IsDistractor: true},
					},
				},
			},
		},
		{
			ID:           "scenario-k8s-io-throttle",
			Title:        "K8s 批处理 Pod 卡住但 CPU 不高",
			Description:  "一批 Kubernetes 批处理任务在某些节点上明显变慢，Pod 看起来一直 Running，但任务推进很慢，CPU 并没有打满。请通过提问逐步定位原因。",
			Domain:       "devops",
			Difficulty:   "L2",
			ScenarioType: "troubleshooting",
			Tags:         []string{"K8s", "iostat", "D状态"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now.Add(-2 * time.Minute),
			UpdatedAt:    now.Add(-2 * time.Minute),
			Content: domain.ScenarioContent{
				RootCause:         "批处理 Pod 所在节点的磁盘 IO 等待过高，进程频繁进入 D 状态，导致任务虽然没报错但整体推进速度极慢。",
				RootCauseKeywords: []string{"IO wait", "D 状态", "磁盘", "iostat", "节点"},
				KeyEvidence: []string{
					"load average 持续升高，但 CPU user/sys 并不高。",
					"进程里有大量 D 状态。",
					"iostat 显示 await 和 util 偏高。",
					"同样任务换到别的节点后明显恢复。",
				},
				StandardProcedure: []string{
					"先区分 CPU 饱和与 IO 等待。",
					"查看 load average、wa 和 D 状态进程。",
					"用 iostat 验证磁盘 await 与 util。",
					"对比不同节点是否存在差异。",
					"迁移任务或隔离异常节点。",
					"整理 IO 瓶颈定位结论。",
				},
				ArchitectureDiagram: "graph TD\nA[Batch Jobs] --> B[Node Disk]\nA --> C[Process Queue]\nB --> D[High await]\nD --> C",
				ReferenceLinks:      []string{"Linux load average", "iostat await"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "k8s-s1", TriggerKeywords: []string{"load average", "CPU不高", "wa"}, Content: "load average 持续升高，但 CPU 并没有明显打满，wa 却很高。", RecommendedNextAsk: "继续看进程状态和磁盘指标。"},
						{ClueID: "k8s-s2", TriggerKeywords: []string{"D状态", "Pod", "阻塞"}, Content: "批处理 Pod 里有不少进程处于 D 状态，说明在等待内核资源。", RecommendedNextAsk: "继续确认是不是磁盘 IO 问题。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "k8s-d1", TriggerKeywords: []string{"iostat", "await", "磁盘"}, PrerequisiteClues: []string{"k8s-s1"}, Content: "iostat 显示 await 和 util 接近打满，磁盘等待才是瓶颈。", RecommendedNextAsk: "继续对比节点差异。"},
						{ClueID: "k8s-d2", TriggerKeywords: []string{"PVC", "节点", "迁移"}, PrerequisiteClues: []string{"k8s-d1"}, Content: "同一任务换到另一台节点后明显恢复，问题与节点磁盘或 IO 路径有关。", RecommendedNextAsk: "可以整理根因并提出隔离方案。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "k8s-x1", TriggerKeywords: []string{"网络", "丢包", "DNS"}, Content: "网络连通性与 DNS 都正常，不是链路抖动。", IsDistractor: true},
					},
				},
			},
		},
		{
			ID:           "scenario-cache-key-release",
			Title:        "活动页发布后回源流量暴涨",
			Description:  "某次活动页发布之后，缓存命中率明显下降，数据库读流量和回源流量同步上涨，但应用本身没有报错。请通过提问逐步定位原因。",
			Domain:       "devops",
			Difficulty:   "L3",
			ScenarioType: "performance",
			Tags:         []string{"缓存", "发布", "回源"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now.Add(-3 * time.Minute),
			UpdatedAt:    now.Add(-3 * time.Minute),
			Content: domain.ScenarioContent{
				RootCause:         "发布时调整了缓存 key 维度，把原本稳定的 key 拆成了包含版本和渠道参数的新 key，导致命中率下降、回源暴涨。",
				RootCauseKeywords: []string{"缓存 key", "命中率", "回源", "发布", "版本"},
				KeyEvidence: []string{
					"问题从发布后开始，时间窗口与回源量上升重合。",
					"缓存命中率从 90% 以上跌到很低。",
					"数据库读流量同步上升，但服务没有错误日志。",
					"回滚后命中率立即恢复。",
				},
				StandardProcedure: []string{
					"先确认问题是否与发布窗口重合。",
					"对比发布前后的缓存命中率与回源量。",
					"检查缓存 key 是否新增了维度。",
					"核对回滚是否能恢复命中率。",
					"修复 key 规则并补充回归检查。",
					"整理缓存层根因与验证结果。",
				},
				ArchitectureDiagram: "graph TD\nA[Activity Page] --> B[Cache Layer]\nB --> C[Origin API]\nC --> D[(Database)]\nB --> E[Hit Rate Monitor]",
				ReferenceLinks:      []string{"缓存命中率监控", "回源流量分析"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "cache-s1", TriggerKeywords: []string{"发布时间", "发布后", "回源"}, Content: "问题从某次发布后开始，发布时间和回源量上升几乎完全重合。", RecommendedNextAsk: "继续看命中率和 key 维度。"},
						{ClueID: "cache-s2", TriggerKeywords: []string{"命中率", "缓存", "数据库读流量"}, Content: "缓存命中率明显下降，数据库读流量同步上升。", RecommendedNextAsk: "继续确认 key 是否被改动。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "cache-d1", TriggerKeywords: []string{"key", "维度", "参数"}, PrerequisiteClues: []string{"cache-s2"}, Content: "缓存 key 新增了版本与渠道参数，导致同一业务请求被拆成多个 key。", RecommendedNextAsk: "继续确认回滚或修复能否恢复命中率。"},
						{ClueID: "cache-d2", TriggerKeywords: []string{"回滚", "恢复", "版本"}, PrerequisiteClues: []string{"cache-d1"}, Content: "回滚后命中率立刻回升，说明问题出在缓存 key 规则，而不是数据库容量。", RecommendedNextAsk: "可以整理根因并给出修复方案。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "cache-x1", TriggerKeywords: []string{"数据库CPU", "慢查询", "索引"}, Content: "数据库本身并没有慢查询激增，核心问题在缓存层。", IsDistractor: true},
					},
				},
			},
		},
	}
}
