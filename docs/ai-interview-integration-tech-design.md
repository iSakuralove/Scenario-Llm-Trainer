# AI-Interview 接入技术方案

## 1. 目标

基于当前仓库与 [`nzy0510/AI-Interview`](https://github.com/nzy0510/AI-Interview) 的实际实现，给出一套可落地的技术接入方案，重点回答：

- 为什么不能直接整仓并入
- 应该复用哪些能力
- 当前项目需要新增哪些模块
- 首期如何在不破坏现有系统的前提下完成接入

## 2. 对比结论

## 2.1 外部仓库能力画像

`AI-Interview` / `InterWise` 当前核心能力包括：

- 面试准备中心
- 文字面试
- 视频面试
- 简历画像
- 历史报告
- AI Mentor
- 用户自定义 LLM Provider 配置
- Question Bank Admin
- 动态 RAG 追问
- Qdrant 向量索引
- embedding-service
- 运营统计与额度保护

关键实现特征：

- 后端：`Spring Boot 3 + MyBatis-Plus + LangChain4j + MySQL + Redis + Qdrant`
- 前端：`Vue 3 + Element Plus`
- 题库术语：`Question Bank / Knowledge Atom / Published Atom / Context Atom`

## 2.2 当前仓库能力画像

当前项目已有：

- 用户体系
- 排查工坊
- 面试舱
- 案例工坊
- 学习仪表盘
- 系统状态页
- Go 后端统一 API
- React 前端统一工作台
- PostgreSQL 持久化
- 可选 pgvector / 内存向量索引
- 语音答案与回放

当前面试能力的技术特征：

- 面试题来自 `InterviewQuestion`
- 通过 `domain + difficulty + question_type` 选择题目
- 三轮追问
- 五维评分
- Agent 化反馈生成与安全重写
- 还没有独立题库发布态和动态检索决策层

## 2.3 为什么不能直接整仓接入

### 技术栈冲突

- 外部仓库：`Spring Boot + Vue + MySQL + Qdrant`
- 当前仓库：`Go + React + PostgreSQL (+ pgvector)`

直接接入会导致：

- 双后端
- 双前端
- 双数据库
- 双管理入口
- 双运维链路

### 领域重叠

两个项目都已经有：

- 用户
- 面试记录
- 报告
- 学习闭环入口
- 管理面板

直接并存会造成数据真相冲突。

### 运维成本过高

如果整仓接入，比赛环境至少会新增：

- MySQL
- Qdrant
- embedding-service
- 第二套后端部署
- 第二套路由/鉴权/配置

这不符合当前项目“简单、稳定、易维护”的原则。

## 3. 推荐架构决策

## 3.1 决策

采用 **能力迁移**，不采用 **运行时并入**。

该决策已在 ADR 中固化：

- [ADR 0001: Use Capability Migration For AI-Interview Integration](G:\计算机设计大赛\docs\adr\0001-ai-interview-capability-migration.md)

即：

- 学外部仓库的产品结构和关键机制
- 但实现落在当前项目现有技术栈中

## 3.2 技术落点

- 前端：继续用 React
- 后端：继续用 Go
- 数据真相：继续用 PostgreSQL
- 向量检索：首期优先复用现有 pgvector / vectorStore 能力
- AI 调用：继续复用当前 `backend/internal/ai`
- 管理权限：继续复用当前 admin 体系

## 4. 首期新增模块

## 4.1 新领域对象

建议新增以下领域对象，而不是直接复用当前 `InterviewQuestion` 承担所有角色。

### `InterviewKnowledgeAtom`

用途：

- 面试追问的最小可检索知识单元

建议字段：

- `id`
- `title`
- `subject`
- `domain`
- `difficulty`
- `category`
- `tags`
- `question_role`
- `reference_answer`
- `reference_keywords`
- `follow_up_hints`
- `status`：`draft / published / archived`
- `source_ref`
- `batch_id`
- `vector_status`
- `created_at`
- `updated_at`

### `InterviewQuestion`

首期不再作为主链路开场题来源，而是退化为兜底数据源。

### `InterviewKnowledgeBatch`

用途：

- 记录导入与发布批次

建议字段：

- `batch_id`
- `source_ref`
- `mode`
- `status`
- `atom_count`
- `created_by`
- `created_at`

### `InterviewRetrievalLog`

用途：

- 记录每轮检索请求、候选结果、进入上下文的条目和回退原因

## 4.3 数据库存储

结合当前项目 `001_schema.sql` 的结构，推荐采用“新增表 + 少量扩展旧表”的方式，而不是大改现有面试表。

### 新增表

#### `interview_knowledge_atoms`

建议用途：

- 存储可发布的面试追问知识原子

建议字段：

- `id TEXT PRIMARY KEY`
- `atom_key TEXT UNIQUE NOT NULL`
- `title TEXT NOT NULL`
- `subject TEXT NOT NULL`
- `domain VARCHAR(50) NOT NULL`
- `difficulty VARCHAR(16)`
- `category VARCHAR(64)`
- `tags TEXT[]`
- `reference_answer TEXT`
- `reference_keywords TEXT[]`
- `follow_up_hints JSONB`
- `question_role VARCHAR(20)`  // opening / followup / mixed
- `status VARCHAR(20) DEFAULT 'draft'`
- `source_ref TEXT`
- `batch_id TEXT`
- `checksum TEXT`
- `current_version INT DEFAULT 1`
- `vector_status VARCHAR(20) DEFAULT 'pending'`
- `last_indexed_at TIMESTAMPTZ`
- `created_by TEXT REFERENCES users(id)`
- `created_at TIMESTAMPTZ DEFAULT NOW()`
- `updated_at TIMESTAMPTZ DEFAULT NOW()`

#### `interview_knowledge_atom_versions`

建议用途：

- 存储面试知识原子的完整版本历史和首期审计信息

建议字段：

- `id TEXT PRIMARY KEY`
- `atom_id TEXT REFERENCES interview_knowledge_atoms(id)`
- `version INT NOT NULL`
- `version_type VARCHAR(32) NOT NULL`
- `snapshot JSONB NOT NULL`
- `diff_summary JSONB DEFAULT '{}'`
- `no_content_change BOOLEAN DEFAULT FALSE`
- `admin_id TEXT REFERENCES users(id)`
- `change_note TEXT`
- `archive_reason TEXT`
- `batch_id TEXT`
- `created_at TIMESTAMPTZ DEFAULT NOW()`

约束建议：

- `UNIQUE(atom_id, version)`
- `version_type IN (content_update, duplicate_import, manual_edit, restore_archived)`

索引建议：

- `(atom_id, version DESC)`
- `(version_type, created_at DESC)`
- `(admin_id, created_at DESC)`

设计原则：

- 主表只保存当前生效版本
- 主表通过 `current_version` 指向当前版本号
- 版本表追历史、承载审计信息
- 首期不单独建审计表
- 批次导入、在线编辑、归档、恢复归档都写入同一版本表
- `snapshot` 保存完整标准化题目内容
- `diff_summary` 只用于摘要展示，不承担恢复能力
- `current_version` 从 `1` 开始，按事件单调递增
- `duplicate_import` 即使内容不变也推进版本号

`snapshot` 首期包含：

- `id`
- `title`
- `subject`
- `domain`
- `difficulty`
- `category`
- `question_role`
- `sourceRef`
- `tags`
- `principles`
- `pitfalls`
- `followUpPaths`
- `status`

`snapshot` 首期不包含：

- `vector_status`
- `last_indexed_at`

`vector_status / last_indexed_at` 属于运行时索引状态，不进入内容版本快照。

#### `interview_knowledge_batches`

建议用途：

- 存储导入与发布批次

建议字段：

- `id TEXT PRIMARY KEY`
- `batch_key TEXT UNIQUE NOT NULL`
- `source_ref TEXT`
- `target_category VARCHAR(64)`
- `mode VARCHAR(20) DEFAULT 'draft'`
- `status VARCHAR(20) DEFAULT 'created'`
- `atom_count INT DEFAULT 0`
- `validation_report JSONB`
- `review_report JSONB`
- `created_by TEXT REFERENCES users(id)`
- `created_at TIMESTAMPTZ DEFAULT NOW()`
- `updated_at TIMESTAMPTZ DEFAULT NOW()`

#### `interview_retrieval_logs`

建议用途：

- 每轮检索请求级日志

建议字段：

- `id TEXT PRIMARY KEY`
- `session_id TEXT REFERENCES interview_sessions(id) ON DELETE CASCADE`
- `user_id TEXT REFERENCES users(id)`
- `round INT NOT NULL`
- `phase VARCHAR(32)`
- `query_text TEXT`
- `requested_limit INT DEFAULT 10`
- `candidate_count INT DEFAULT 0`
- `selected_count INT DEFAULT 0`
- `retrieval_strategy VARCHAR(32)`
- `latency_ms BIGINT DEFAULT 0`
- `status VARCHAR(16)`
- `error_message TEXT`
- `selected_atom_ids TEXT[]`
- `candidate_atom_ids TEXT[]`
- `created_at TIMESTAMPTZ DEFAULT NOW()`

### 扩展现有表

#### `interview_sessions`

建议新增：

- `difficulty_level VARCHAR(16)`
- `focus_areas TEXT[]`
- `setup_notes JSONB DEFAULT '{}'`
- `question_snapshot JSONB DEFAULT '{}'`

其中：

- `setup_notes` 建议只承载会话级准备参数，如本次面试目标岗位补充说明
- 不建议首期单独建完整简历实体表
- `question_snapshot` 用于保存开场题快照，避免题目后续编辑影响历史会话与报告

#### `users.profile`

当前项目的“个人档案”已经有 `target_level` 与 `preferred_domains`。

首期建议直接扩展现有 `users.profile`，新增可选字段：

- `resume_summary`
- `project_summary`

用途：

- 让用户可在“个人档案”中填写简历摘要/项目经历摘要
- 让 Agent 在面试中更精准理解用户背景

约束：

- 这两个字段都是可选
- 不参与登录、权限或硬性校验
- 为空时面试流程照常运行

#### `interview_sessions.evaluations`

当前项目把轮次评分存在 JSONB 中，这反而适合增量演进。

建议在 `InterviewEvaluation` 结构中追加：

- `retrieval_strategy`
- `retrieval_candidate_count`
- `retrieval_selected_count`
- `selected_atom_ids`
- `selected_atom_snapshots`
- `knowledge_domains`

好处：

- 无需为每个回合单独建新表
- 报告页和历史页取数简单
- 对现有 schema 侵入小

其中：

- `selected_atom_snapshots` 只保存轻量元数据，如 `atom_id / version / title / subject / domain`
- 不保存追问原子的整段正文，避免会话 JSON 膨胀

## 4.4 首期种子迁移策略

当前项目已经有一批 `InterviewQuestion` 种子题。

为了避免首期上线后题库为空，建议采用一次性引导迁移：

1. 将现有 `InterviewQuestion` 批量迁移进入新题库体系。
2. 启动时把现有种子题中的：
   - `title`
   - `description`
   - `reference_answer`
   - `reference_keywords`
   - `follow_up_strategies`
   映射成首批新题库记录，并标记 `question_role = opening` 或 `mixed`。
3. 默认写入 `published` 状态，来源标记为 `seed_bootstrap`。
4. 主链路优先使用带 `question_role=opening/mixed` 的新题库记录；追问检索优先使用 `question_role=followup/mixed`。
5. 只有当新题库为空、筛选失败或兼容迁移未完成时，才回退读旧 `InterviewQuestion`。
6. 后续新增资源一律走管理端导入/发布流程。

这样可以保证：

- 首期一接入动态检索就有可用内容
- 不需要先等后台工具全部做完才有演示数据
- 当前已有题目资产不会被废掉，但会从“主链路来源”降级为“兜底数据”

## 4.5 首期数据入口

已确认首期唯一正式入库格式为标准 JSON 导入包。

推荐包结构：

```json
{
  "batchId": "qb-java-opening-20260612-001",
  "sourceRef": "manual-curation",
  "targetCategory": "java",
  "mode": "DRAFT",
  "atoms": [
    {
      "id": "java-hashmap-opening-001",
      "subject": "HashMap 扩容机制",
      "category": "java",
      "difficulty": "mid",
      "tags": ["java", "hashmap", "collections"],
      "questionRole": "opening",
      "sourceRef": "manual-curation",
      "content": {
        "principles": "核心原理与标准答案",
        "pitfalls": "常见误区",
        "followUpPaths": [
          "强回答时的深挖路径",
          "弱回答时的补救路径"
        ]
      }
    }
  ]
}
```

说明：

- 管理端首期只接受这种结构化 JSON 包
- `atoms` 内每条记录仍然统一进入同一张题库表
- `questionRole` 在导入时映射到库内的 `question_role`

## 4.6 原始素材来源

后续可支持的原始来源：

- PDF
- DOCX
- TXT
- MD
- 已有 JSON

但这些来源都不是首期正式入库格式。

它们的正确路径是：

1. 原始文档读取
2. 内容清洗 / 切块 / 去噪
3. 转成标准 JSON 导入包
4. 管理端校验
5. 发布 / 归档 / 重建索引

## 4.7 数据清洗最低标准

已确认首期每条题目至少必须包含：

- `principles`
- `pitfalls`
- `followUpPaths`

缺一不可。

### 参考外部仓库的实际做法

外部 `AI-Interview` 仓库当前脚本已经有这些事实：

1. `scripts/question_bank_import.py` 会把导入目标规范化为：
   - `subject`
   - `category`
   - `difficulty`
   - `tags`
   - `content.principles`
   - `content.pitfalls`
   - `content.followUpPaths`
2. 它当前对 `content.principles` 是硬要求。
3. 它当前对 `followUpPaths` 的检查偏“推荐至少两条”，还不是强制失败。

本项目首期建议比它更严格：

- `principles` 缺失：直接失败
- `principles` 少于 2 条：直接失败
- `pitfalls` 缺失：直接失败
- `followUpPaths` 缺失：直接失败
- `followUpPaths` 少于 2 条：直接失败
- `principles` 无法支撑一次完整回答：直接失败
- `pitfalls` 少于 2 条：直接失败

### 顺序要求

已确认首期 `pitfalls` 与 `followUpPaths` 保留顺序。

推荐解释：

- `pitfalls[0]`：最常见或最关键误区
- `followUpPaths[0]`：默认优先追问路径

因此：

- 清洗器不能打乱原始顺序
- 存储层不能把它们当成无序集合

### `principles` 的最低粒度

已确认首期 `principles` 不能只是关键词、短标签或占位提示。

它至少要达到：

- 候选人掌握这段内容后
- 能组织出一次完整回答

不要求写成超长标准答案，但必须具备：

- 核心原理
- 关键判断点
- 基本解释路径

### 原因

因为你已经明确：

- 题库质量是这个功能的基底
- 一旦数据塌了，后面的服务没有意义

所以首期要优先收紧数据门槛，而不是追求导入吞吐量。

## 4.9 首期导入流程

已确认首期导入流程采用三段式：

1. `validate`
2. `dry-run / preview`
3. `publish`

### 目的

- `validate`：检查字段完整性、枚举合法性、重复 ID、缺失核心块
- `dry-run / preview`：让管理员看到导入后会产生什么结果，但先不进入正式主链路
- `publish`：只有确认无误后才写入正式题库并进入后续索引流程

### 原则

- 不允许上传后直接变成已发布题目
- 首期宁可多一步确认，也不要用“快进库”换数据风险

## 4.10 首期试运行策略

已确认首期试运行/预览只做纯模拟，不写正式库。

### 试运行阶段允许输出

- 校验错误列表
- 校验警告列表
- `error_count`
- `warning_count`
- 新增题目数量
- 更新题目数量
- 归档题目数量
- 重复 ID 列表
- 分类分布
- 题目角色分布
- opening / followup / mixed 数量统计
- 样例预览
- 开放组合命中统计
- 内容质量拦截数量
- 结构字段缺失拦截数量

### 试运行阶段禁止行为

- 写入正式题库表
- 生成正式版本号
- 改变发布态
- 触发索引写入
- 影响开场题选择和追问检索

### 这样做的目的

- 保证预览数据和正式数据彻底分离
- 降低脏数据残留风险
- 让管理员先看清结果，再决定是否发布

## 4.11 首期发布与索引策略

已确认正式发布时同步触发当前批次的增量索引。

发布阶段应执行：

1. 写入正式题库记录
2. 标记发布态
3. 只对当前批次新增/变更记录触发增量索引
4. 记录索引状态与失败原因

同时保留：

- 管理员手动 `reindex` 修复入口

### 为什么不是“发布后手动再建索引”

因为这样会造成明显的用户错觉：

- 管理员看到“发布成功”
- 但面试主链路实际还搜不到新题

首期既然已经把试运行做成纯模拟，那么正式发布就必须承担“真正生效”的职责。

## 4.12 首期不做的增强

已确认以下能力不纳入首期：

- 可回滚的试导入
- 试运行写临时库再回滚
- 复杂的导入事务回放机制

## 4.13 索引失败时的降级策略

已确认索引失败时不能让面试主链路整体失效。

### 开场题

- 仍然可以从已发布题库记录中按普通条件筛选：
  - `domain`
  - `difficulty`
  - `question_role in (opening, mixed)`
    - `status = published`
- 索引失败不影响开场题选择

### 追问

- 如果索引不可用、当前批次增量索引失败，或检索阶段异常：
  - 不注入题库检索上下文
  - 回退到当前已有规则化追问链路
  - 允许继续完成本场面试

### 管理端

- 必须显示：
  - 批次索引状态
  - 失败记录数量
  - 失败原因
    - 手动重建入口
- 手动重建索引只允许 `admin`
- 管理端必须支持筛选 `vector_status = failed` 的题目

管理端还必须支持：

- 状态筛选，包含 `archived`
- `version_type = manual_edit / restore_archived` 筛选

题库列表默认隐藏 `archived`。

题库列表需要显示：

- 最近编辑人
- 最近编辑时间

## 4.14 首期难度体系

已确认首期底层 `difficulty` 继续沿用当前项目已有的 `L1-L5`。

### 管理端语义标签

管理端可以增加语义标签辅助理解，但不改变底层存储值。

建议映射：

- `L1 -> 入门`
- `L2 -> 初级`
- `L3 -> 中级`
- `L4 -> 高级`
- `L5 -> 专家`

## 4.15 首期题库分类策略

已确认首期 `category` 使用少量受控枚举，不接受自由文本分类。

建议首期枚举：

- `java`
- `database`
- `cache`
- `middleware`
- `system_design`
- `frontend`
- `ai_llm`
- `hr_soft_skill`

### 标签与分类分工

- `category`：少量稳定一级分组，用于筛选和统计
- `tags`：更细粒度技术点，如 `redis`、`mysql`、`jvm`、`spring`、`rag`

### 非法分类处理

已确认当导入包中的 `category` 不在受控枚举里时，首期直接报错。

不采用：

- 自动映射到“最近分类”
- 猜测同义词后放行
- 静默替换

## 4.15A 首期 domain 策略

已确认首期 `domain` 也使用受控枚举，不接受自由文本。

原因：

- `domain` 直接参与开场题筛选
- `domain` 直接参与追问检索
- 如果放自由文本，会立刻出现同义词和脏数据分裂

建议首期与当前项目已有面试域保持一致，由后端统一约束。

## 4.16 首期 tags 策略

已确认首期 `tags` 允许管理员自由填写，但系统必须做基础规范化。

首期最少应做：

- 去首尾空格
- 去重
- 统一命名风格
- 过滤空标签

推荐：

- 默认转小写或统一展示风格
- 为常见别名保留后续映射能力，例如：
  - `mq` / `message-queue`
  - `llm` / `ai_llm`

原则：

- `category` 保守
- `tags` 灵活

## 4.17 `question_role` 使用策略

已确认首期保留：

- `opening`
- `followup`
- `mixed`

但 `mixed` 只是例外类型，不是默认类型。

### 使用原则

- `opening`：优先用于开场题
- `followup`：优先用于追问检索
- `mixed`：只有当同一条题目确实同时适合两种用途时才使用

### 管理要求

- 管理端应提示 `mixed` 谨慎使用
- 校验器应统计 `mixed` 占比
- 当批次中 `mixed` 过多时，至少给 warning

### 批次级默认值

已确认首期允许导入包顶层声明默认 `questionRole`。

执行规则：

- 批次级默认值作为 fallback
- 单题未填写时继承
- 单题显式填写时覆盖顶层默认值

## 4.17A `difficulty` 批次级默认值

已确认首期允许导入包顶层声明默认 `difficulty`。

执行规则：

- 批次级默认值作为 fallback
- 单题未填写时继承
- 单题显式填写时覆盖顶层默认值

## 4.17B `category` 批次级默认值

已确认首期允许导入包顶层声明默认 `category`。

执行规则：

- 批次级默认值作为 fallback
- 单题未填写时继承
- 单题显式填写时覆盖顶层默认值
- 但无论顶层还是单题，值都必须命中受控枚举

## 4.17C `tags` 批次级默认值

已确认首期允许导入包顶层声明默认 `tags`。

执行规则：

- 顶层标签作为公共标签
- 单题未填写时继承顶层标签
- 单题显式填写时，与顶层标签去重合并

## 4.17D 核心内容字段不允许批次默认值

已确认首期以下字段不允许使用批次级默认值：

- `principles`
- `pitfalls`
- `followUpPaths`

原因：

- 这三块是题目本体内容，不是元信息
- 允许批次级继承会导致整批题目共用同一套内容骨架，直接破坏题库质量

执行规则：

- 必须逐题显式填写
- 清洗器不得用批次级默认值补这三个字段

## 4.17E 核心内容字段统一数组化

已确认首期以下字段的最终规范统一为数组：

- `principles: string[]`
- `pitfalls: string[]`
- `followUpPaths: string[]`

### 原则

- 即使只有一条内容，也必须包成数组
- 清洗器负责把历史字符串格式统一收敛成数组
- 前后端都按数组协议处理

## 4.17F 首期内容质量校验

已确认首期除了结构校验，还必须做基础内容质量校验。

至少拦截：

- 明显重复句式
- 空泛套话
- 只有模板词、没有实质信息
- `principles / pitfalls / followUpPaths` 三块内容高度重复

即使结构字段齐全，只要内容质量不达标，也不能通过首期校验。

### 首期裁决方式

已确认首期内容质量裁决方式为：

- 规则校验为主
- 管理员人工复核为辅
- AI 不作为首期主裁判

原因：

- 规则校验更稳定
- 人工复核更可解释
- AI 辅助可以存在，但不能决定最终是否入库

### 复核备注

已确认首期管理员人工复核时，关键发布动作必须填写简短备注。

最少可支持：

- `补全 followUpPaths`
- `修正 difficulty`
- `确认重复导入留痕`
- `人工确认内容可用`

## 4.17G 批次内部分发布

已确认首期允许同一批次内部分题目发布。

### 含义

- 一批题目导入后
- 管理员可以只发布其中通过的部分
- 不需要等待整批所有题都达到可发布状态

### 目的

- 避免少数坏题拖住整批
- 提升题库运营效率

## 4.17H 批次状态细分

已确认首期批次状态需要细分，并至少包含：

- `draft`
- `validated`
- `previewed`
- `partially_published`
- `published`
- `failed`

其中：

- `partially_published` 专门表达“同一批次里有一部分题已经发布，另一部分仍未发布”

## 4.17H-1 批次详情展示字段

已确认首期批次详情页必须显示每条题的：

- `category`
- `domain`
- `difficulty`
- `question_role`
- 单题状态

## 4.17H-2 单题详情展示字段

已确认首期管理员点开单题时，必须能看到：

- `principles`
- `pitfalls`
- `followUpPaths`

的原始结构化内容。

### 题池展示

已确认单题详情页不只显示 `question_role` 原始值，还应直接翻译为：

- `opening` -> 开场题池
- `followup` -> 追问池
- `mixed` -> 开场题池 + 追问池

### 开放组合命中展示

已确认单题详情页还应显示这条题当前命中的开放组合，例如：

- `database + L2`
- `database + L3`

### 未命中原因展示

已确认单题详情页还应显示未命中原因，例如：

- 题量不足
- `question_role` 不匹配
- `difficulty` 不匹配
- `category` 不在开放集合
- 题目仍是 `draft`
- 题目已 `archived`

## 4.17I 单题状态与批次状态并存

已确认首期必须同时保留：

- 批次状态
- 单题状态

### 原因

- 批次内允许部分发布
- 因此单题是否已发布不能靠批次状态反推

### 结论

- `batch.status` 负责描述整批的整体进度
- `atom.status` 负责描述单题自己的发布态

## 4.17J 首期单题状态

已确认首期单题状态至少包含：

- `draft`
- `published`
- `archived`

### archived 语义

已确认 `archived` 状态的题彻底退出主链路：

- 不能做开场题
- 不能做追问检索
- 但版本历史、批次关系和审计信息继续保留
- 首期不允许物理删除题目，只允许归档
- 归档时必须填写归档原因
- 允许通过独立恢复动作恢复为 `published`
- 恢复时必须执行硬校验并二次确认
- 恢复归档题不用填写恢复备注
- 恢复归档题生成的新版本类型为 `restore_archived`
- 通过硬校验后立即恢复为 `published`，可进入后续新面试主链路
- 恢复后索引异步期间允许做开场题，追问增强等待索引完成
- `archive_reason` 必须写入版本历史和审计信息

### draft 语义

已确认 `draft` 状态的题完全不进入用户侧面试链路。

也就是：

- 不能做开场题
- 不能做追问检索
- 只作为后台准备态存在

## 4.17K 首期管理端最小动作集

已确认首期管理端最小必须动作集只保留：

1. 校验
2. 试运行 / 预览
3. 正式发布
4. 归档
5. 恢复归档
6. 重建索引

不在首期最小集里继续膨胀更多后台动作。

已确认首期“检索预览”并入“试运行 / 预览”，不单独作为第 6 个后台动作存在。

## 4.18 `subject` 规范

已确认首期导入包中的 `subject` 必须是稳定的“面试考察点标题”，不能直接写成自然语言题干。

### 作用

- 作为题目管理的稳定锚点
- 支撑去重、归类、筛选和统计
- 便于后续复用为开场题或追问资源

### 推荐写法

- 名词化、短标题化
- 聚焦知识点本身，而不是提问语气

示例：

- `HashMap 扩容机制`
- `MySQL 联合索引最左匹配`
- `Redis 过期删除策略`

不推荐：

- `请解释一下 HashMap 为什么扩容`
- `你会怎么分析 MySQL 联合索引`

## 4.19 `title` 与 `subject` 的字段分工

已确认首期同时保留：

- `title`
- `subject`

### 推荐职责

- `subject`：
  - 稳定知识点锚点
  - 用于去重、归类、筛选、统计
- `title`：
  - 用于管理端与前端展示
  - 可以比 `subject` 更完整、更易读

### 示例

- `subject`: `HashMap 扩容机制`
- `title`: `Java 集合：HashMap 扩容机制与常见误区`

### 原则

- `subject` 负责“机器可管理”
- `title` 负责“人可阅读”

## 4.21 同 ID 导入与版本历史

已确认同一稳定 `id` 的题目再次导入时，首期按“更新旧题”处理，并保留版本历史。

### 推荐行为

- 导入阶段发现已有同 `id`
- 不视为冲突新题
- 进入“更新已有题目”分支
- 生成新版本快照
- 当前正式版本指向最新发布版

### 同内容再次导入

已确认：

- 即使同一 `id` 的内容完全一样
- 再次导入时也仍然生成新版本记录

这意味着首期版本历史强调“导入事件留痕”，而不只强调“内容差异留痕”。

### 批内重复处理

已确认同一个导入包内如果出现重复 `id`，首期直接判错。

不采用：

- 取最后一条
- 静默覆盖
- 自动合并

### 版本类型

已确认首期版本历史必须至少区分四类：

- `content_update`
- `duplicate_import`
- `manual_edit`
- `restore_archived`

作用：

- 防止版本历史被重复导入污染后失去可读性
- 让管理员能区分“真的改了题”还是“只是又导了一次”
- 让管理员能识别“后台在线编辑保存后直接生效”的版本来源
- 让管理员能识别“恢复归档题后重新进入主链路”的版本来源

### 版本表设计

首期版本历史单独存入 `interview_knowledge_atom_versions`。

题目主表 `interview_knowledge_atoms` 只保存当前生效版本，并通过 `current_version` 标记当前版本号。

版本表统一承载：

- 批次导入生成的版本
- 在线编辑生成的版本
- 重复导入生成的版本
- 归档 / 恢复归档生成的版本
- 字段级 diff 摘要
- 首期审计信息

首期不单独建审计表。

### 版本写入函数

批次导入、在线编辑、归档、恢复归档必须复用同一套版本写入函数。

该函数至少负责：

- 计算新版本号
- 写入 `version_type`
- 写入当前快照
- 写入 `diff_summary`
- 写入 `admin_id / change_note / archive_reason / batch_id`
- 更新主表 `current_version`

这样可以避免不同动作产生不一致的版本历史。

异常判定：

- 如果主表 `current_version` 与版本表最新 `version` 不一致，视为数据异常
- 管理端应暴露异常，后续可用修复脚本处理

### 已发布题修改方式

已确认首期开放“已发布题目编辑入口”，且编辑保存后直接生效：保留版本历史，但当前版本立即切换。

当前确定：

- 管理端可进入已发布题的编辑界面
- 保存后直接切换当前正式生效内容
- 同时保留版本历史与审计信息
- 历史会话继续读取 `question_snapshot` 与 `selected_atom_snapshots`，不被新版本反向污染
- 保存接口先返回成功，索引更新异步补齐
- 保存后将当前题目的 `vector_status` 标记为 `pending`
- 索引成功后改为 `indexed`
- 索引失败后改为 `failed`，并保留手动重建索引入口
- 保存前复用导入链路的同一套硬校验规则
- 校验失败时不生成新版本、不切换当前正式内容
- 保存成功时必须提交 `change_note`
- `change_note` 写入版本历史和审计信息
- 在线编辑生成的新版本类型为 `manual_edit`
- 即使内容没有实际变化，也生成 `manual_edit` 版本，并标记 `no_content_change = true`
- 版本历史和审计信息必须记录 `admin_id`、操作时间、`change_note` 和版本类型
- 保存字段级 diff 摘要，不保存复杂富文本 diff
- diff 摘要只记录字段名和摘要，不记录完整旧正文
- `diff_summary` 使用 `JSONB`
- 保存时必须校验版本号；如果管理员基于旧版本编辑，拒绝保存并提示刷新
- 保存前需要轻量二次确认弹窗
- 稳定 `id` 不允许在线编辑修改
- 如需更换 `id`，应新建题目并归档旧题
- 允许修改 `question_role`
- 修改 `question_role` 后立即重算题池归属与开放组合命中情况
- 允许修改 `category / domain / difficulty`，但必须命中受控枚举，并在保存后立即重算开放组合
- 允许修改 `subject`，但必须保持稳定考察点标题语义，不能改成自然语言题干
- 允许修改 `title`，且仍允许 `title = subject`
- 允许修改 `sourceRef`，但必须非空，且只在管理端、版本历史和审计中可见
- 允许修改 `principles / pitfalls / followUpPaths`，但必须继续满足数组、数量、顺序和内容质量硬校验
- 允许修改 `tags`，但保存时必须 trim、去重、统一大小写或别名映射
- 普通编辑表单不允许修改单题状态 `draft / published / archived`

二次确认要求：

- 只用于防误操作
- 不做复杂审批流
- 不要求多人复核
- 文案提示“保存后将立即影响后续新面试，历史会话不受影响”

### 在线编辑校验边界

在线编辑不需要走完整批次预览，但必须执行与导入相同的单题硬校验：

- 稳定 `id` 不可修改
- `domain / category / difficulty / question_role` 枚举合法
- `sourceRef` 必填且非空
- `principles / pitfalls / followUpPaths` 必须为数组
- `principles / pitfalls / followUpPaths` 数量与内容质量达标
- `tags` 规范化后保存
- `subject` 必须仍是稳定考察点标题，不能是自然语言题干
- `title` 可修改，且允许等于 `subject`
- 单题状态不得通过普通编辑表单修改
- 请求必须携带 `base_version`

首期不做：

- 批量在线编辑多题

校验失败时：

- 不保存
- 不生成新版本
- 不触发索引
- 向管理员返回可修正的错误明细

并发控制：

- 如果 `base_version` 缺失，拒绝保存
- 如果 `base_version != current_version`，拒绝保存并提示管理员刷新
- 如果匹配，则生成新版本并切换主表 `current_version`

首期冲突提示保持轻量：

- `版本已更新，请刷新后重试`

首期不做：

- 强制覆盖别人刚改内容的按钮

如果管理员确实需要更换题目 `id`：

- 新建一条题目
- 归档旧题
- 让两条题目的版本历史和会话快照保持独立

如果管理员修改 `question_role`：

- 保存后立即重新计算该题进入的题池：开场题池、追问池或两者
- 保存后立即重新计算当前命中的开放组合
- 单题详情页同步展示新的命中结果和未命中原因
- 允许 `mixed` 改成 `opening` 或 `followup`

如果管理员修改 `category / domain / difficulty`：

- 保存后立即重新计算开放组合命中情况
- 单题详情页同步展示新的命中结果和未命中原因

如果重新计算导致某个 `category + difficulty` 组合低于最低题量：

- 后端自动关闭该组合
- 普通用户不再看到该组合
- 管理端显示警告和关闭原因
- 首期不做消息推送

如果管理员需要修改单题状态：

- `draft -> published` 继续走独立发布动作
- `published -> archived` 继续走独立归档动作
- 不在普通编辑表单中直接切换状态

### 在线编辑变更备注

在线编辑已发布题目保存成功时，必须填写简短编辑原因 / 变更备注。

建议字段：

- `change_note TEXT NOT NULL`

最小要求：

- 非空
- 进入版本历史
- 进入审计信息
- 管理端版本详情可见

示例：

- `修正 Redis 过期策略表述错误`
- `补充 HashMap 扩容触发条件`

### 归档与恢复

归档题目必须填写归档原因。

建议字段：

- `archive_reason TEXT NOT NULL`

`archive_reason` 必须写入版本历史和审计信息。

归档后允许恢复为 `published`，但必须走独立恢复动作：

- 执行与导入 / 在线编辑相同的硬校验
- 轻量二次确认
- 生成 `restore_archived` 版本
- 恢复后重新计算题池归属与开放组合命中
- 恢复后将 `vector_status` 标记为 `pending`，索引异步补齐
- 通过硬校验后立即恢复为 `published`
- 恢复后可进入后续新面试主链路
- 索引异步期间允许做开场题，追问增强等待索引完成

恢复失败时不切换状态。

恢复归档题不用填写恢复备注。

恢复时如果硬校验不通过，必须拒绝恢复。

### 版本 diff 摘要

版本历史需要保存字段级 diff 摘要。

首期只记录：

- 字段名
- 变化摘要
- 是否内容实际变化

首期不记录：

- 完整旧正文
- 复杂富文本 diff

首期不做：

- 回滚到历史版本

### 版本历史展示

版本历史列表默认按 `created_at DESC` 排序。

版本历史列表应显示：

- 版本号
- `version_type`
- `admin_id`
- `created_at`
- `no_content_change`
- `change_note`

版本详情页应显示：

- 完整 `snapshot`
- `diff_summary`
- `no_content_change`

### 不推荐

- 直接拒绝这类导入
- 直接覆盖且不保留历史

### 首期容错

已确认首期允许 `title = subject`。

也就是说：

- 如果管理员没有单独维护展示标题
- 系统不强制报错
- 可以直接把 `subject` 作为 `title`

## 4.22 `sourceRef` 使用策略

已确认首期保留 `sourceRef`，但不在前端用户界面显示。

### 用途

- 追溯题目来源
- 支撑批次核对
- 支撑后台审计
- 支撑后续内容修订回查

### 展示边界

- 管理端可见
- 导入校验/预览可见
- 版本历史可见
- 普通前端用户界面首期不展示

### 校验要求

已确认首期 `sourceRef` 必填且不能为空。

### 批次级继承

已确认首期允许：

- 导入包顶层配置统一 `sourceRef`
- 单题未填写时继承顶层值

这样可以降低批量录题成本。

但在入库前应展开为：

- 每条题自己的明确 `sourceRef`

## 4.23 首期最低题量门槛

已确认首期每个 `category + difficulty` 组合至少应满足：

- `opening / mixed` >= 3
- `followup / mixed` >= 6

### 用途

- 控制哪些组合可以真正开放给用户
- 避免某个组合只有 1 道开场题或极少追问资源时直接上线

### 达不到门槛时的处理

首期达不到最低题量的组合不得开放。

如果在线编辑、归档或角色变更导致已开放组合跌破门槛：

- 后端自动把该组合从普通用户可见列表移除
- 管理端显示警告
- 不使用旧 `InterviewQuestion` 补题量
- 不做消息推送

- 默认不开放该组合

首期不采用：

- 用旧 `InterviewQuestion` 去填平题量不足的运营缺口

也就是说，旧 `InterviewQuestion` 只服务明确的兼容/异常兜底，不服务日常运营补位。

### 旧 `InterviewQuestion` 的兜底边界

已确认它首期只用于：

- 迁移期兼容
- 明确的异常降级

不用于：

- 补题量
- 补建设进度
- 偷偷给用户兜底体验

### 兜底可见性

已确认：

- 普通用户侧不暴露“当前走的是兜底链路”
- 管理员侧必须能看到：
  - 是否触发兜底
  - 触发原因
  - 触发次数

### 首期处理要求

已确认首期管理员侧对兜底事件：

- 只做被动可见
- 只做可追踪

首期不要求：

- 实时处理
- 告警升级
- 值班响应机制

## 4.24 首期开放组合

已确认首期只开放少量高价值组合，不求全量铺开。

首期默认开放：

- `java + L2/L3`
- `database + L2/L3`
- `cache + L2/L3`
- `ai_llm + L2/L3`

### 目的

- 先在最有价值、最容易凑齐题量门槛的组合上验证主链路
- 控制题库建设压力
- 降低“分类很多但每类都很薄”的风险

## 4.25 未开放组合的前端展示

已确认首期未开放组合不显示。

### 原因

- 有题库、能开放的才显示，避免前端出现大量“看得见但不能用”的噪音入口
- 当前项目更重视可用性，不强调“路线图展示”

### 前端建议

- 面试启动台只展示当前后端返回的可用组合
- 未开放组合不渲染

## 4.26 组合状态来源

已确认首期组合状态由后端统一返回，不写死前端。

### 后端负责判断

后端基于以下事实计算每个 `category + difficulty` 组合状态：

- 是否属于首期允许范围
- 是否满足最低题量门槛
- 是否有可用开场题
- 是否有可用追问资源
- 索引状态是否可接受

在线编辑保存后，如果影响 `question_role / category / domain / difficulty / tags`：

- 立即重算组合状态
- 自动关闭低于门槛的组合
- 管理端展示关闭原因

### 前端负责展示

前端只消费后端允许展示的组合列表。

并据此决定：

- 展示哪些组合
- 哪些组合允许进入面试

## 4.27 个人档案摘要缺失时的前端表现

已确认个人档案中的简历摘要 / 项目经历摘要是可选项。

因此首期前端行为应为：

- 如果为空，显示中性提示
- 提示语义：补充后可以让 Agent 更精准理解用户背景
- 不做错误态
- 不做红色警告
- 不阻断面试启动


这些都属于后续增强期，而不是当前第一阶段的基础能力。

## 4.2 为什么不直接复用当前 `InterviewQuestion`

当前 `InterviewQuestion` 更像“开场题目实体”，而不是“题库原子”。

如果硬复用，会产生三个问题：

1. 开场题和追问知识点耦合
2. 发布态与追问资源粒度不清
3. 后续做题库导入、批次管理、检索日志时模型会越长越乱

因此推荐：

- 新题库体系同时承担“开场题 + 追问资源”
- `InterviewQuestion` 仅保留为兜底兼容数据

## 5. 后端接入方案

## 5.1 目录建议

建议在当前后端继续沿用 `httpapi + store + ai + domain` 的边界，不单独引入第二套服务。

建议新增：

- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/interview_retrieval.go`
- `backend/internal/store/interview_bank.go`
- `backend/internal/domain/interview_bank.go`

必要时扩展：

- `backend/internal/agent/interview.go`
- `backend/internal/httpapi/handlers_interviews.go`

## 5.2 接口建议

### 管理端

- `GET /admin/interview-bank/categories`
- `POST /admin/interview-bank/import/validate`
- `POST /admin/interview-bank/import/publish`
- `POST /admin/interview-bank/search`
- `POST /admin/interview-bank/atoms/archive`
- `POST /admin/interview-bank/atoms/publish`
- `POST /admin/interview-bank/reindex`

### 面试运行时

不建议对前端暴露新的复杂检索接口。

追问检索应在后端内部完成，前端仍然只调用：

- 创建会话
- 提交回答
- 回答追问
- 获取报告

### 请求体建议

#### 创建面试会话

在当前 `createInterview` 基础上扩展：

```json
{
  "domain": "database",
  "difficulty": "L3",
  "question_type": "troubleshooting",
  "difficulty_level": "mid",
  "focus_areas": ["depth", "communication"],
  "resume_summary": "做过 MySQL 慢查询治理和缓存一致性治理"
}
```

说明：

- `resume_summary` 默认从“个人档案”带出
- 允许面试发起时为空
- 首期不要求用户每次进入面试都重新填写
- 首期开场题不做复杂推荐，只做简单可控筛选

#### 报告返回增强

在当前报告响应中追加：

```json
{
  "retrieval_summary": {
    "used_rounds": 2,
    "fallback_rounds": 1,
    "top_domains": ["database", "cache"],
    "selected_atom_count": 5
  }
}
```

普通用户报告页不展示题库版本号。

如果后端保留版本标识用于追溯，应只返回给管理端视图或审计接口。

## 5.3 追问链路改造

当前链路：

- 创建面试会话
- 用户回答
- 五维评分
- 判断是否追问
- 生成反馈

改造后链路：

1. 创建面试会话时，优先从新题库体系中选取 `question_role=opening/mixed` 的记录作为开场题。
2. 创建会话时同步写入 `question_snapshot`，保存当次题目版本与展示内容。
3. 每轮提交回答时，构造检索 query：
     - 开场题
     - 岗位域
     - 难度
     - 当前回答
     - 历史追问
     - 已使用原子
4. 检索已发布且 `question_role=followup/mixed` 的 `InterviewKnowledgeAtom`
5. 对本轮最终实际命中的追问知识原子，写入轻量快照元数据，不保存整段正文。
6. 根据回答质量和召回质量做决策：
     - `deepen`
   - `remedial`
   - `switch_topic`
   - `fallback_rule_only`
5. 将精选上下文注入反馈生成
  6. 在 session / evaluation 中记录：

运行时边界：

- 已创建会话的开场题继续读取 `question_snapshot`
- 在线编辑只影响后续新面试的开场题选择
- 正在进行中的面试，后续轮次追问检索使用编辑后的实时题库内容
- 每轮实际命中的追问原子写入 `selected_atom_snapshots`
   - 候选原子
   - 实际进入上下文的原子
   - 回退原因

兜底链路：

- 如果新题库为空
- 或开场题筛选失败
- 或迁移期间兼容逻辑未满足

则允许短路回退到旧 `InterviewQuestion`。

### 首期开场题选择策略

首期建议只做简单可控筛选，不做复杂推荐排序。

基础筛选条件：

- `domain`
- `difficulty`
- `question_role in (opening, mixed)`
- `status = published`

候选选取策略建议：

1. 先筛出满足条件的候选
2. 若有标签或焦点能力可用，做轻量优先级匹配
3. 若仍有多条候选，按简单稳定规则取一条：
   - 最新发布优先
   - 或固定随机种子
   - 或按更新时间倒序

首期不建议做：

- 多维打分排序
- 用户画像学习推荐
- 历史表现驱动的开场题推荐

## 5.4 与当前 Agent 评分链路的衔接

当前项目的面试评分已经是 Agent 化步骤：

- `analyze_answer_intent`
- `evaluate_dimensions`
- `decide_follow_up`
- `generate_feedback`
- `safety_rewrite`

推荐做法不是推翻，而是在 `decide_follow_up` 之前插入一个检索决策步骤，例如：

- `retrieve_interview_atoms`
- `plan_follow_up_strategy`

然后让现有：

- `decide_follow_up`
- `generate_feedback`

消费该步骤产出的上下文和策略。

这样能保持：

- 当前追问判断逻辑主线不散
- 安全改写仍是最后一道出口
- 运行时阶段消息仍能复用现有前端流式展示

## 5.5 决策规则建议

首期可以直接吸收 `AI-Interview` 的思想，但用当前项目可维护的实现：

- 低信息回答 + 高置信召回：补救追问
- 低信息回答 + 弱召回：切换知识点
- 正常回答 + 可用召回：深挖
- 连续空泛回答：切换知识点或结束追问
- 检索失败：回退现有固定追问规则

## 5.5 向量检索方案

### 首期推荐

复用当前项目已有向量基础设施，不引入 Qdrant。

理由：

1. 当前仓库已经有 `vectorStore` 与 pgvector 路线。
2. 当前比赛项目已有 embedding 配置与降级策略。
3. 新增 Qdrant + embedding-service 会显著放大部署复杂度。

### 首期做法

- 新增面试题库原子的向量文档构建函数
- 在 PostgreSQL + pgvector 中维护面试题库向量索引
- 若 pgvector 不可用，保留内存回退

建议尽量复用当前项目已有模式：

- `question_id / doc_type / doc_key / doc_text / embedding / metadata`

不要在首期为了面试题库再造一套完全不同的向量索引协议。

### 后续可选

如果后续题库规模明显增大，且检索质量/延迟证明确实需要，再考虑独立向量服务。

## 6. 前端接入方案

## 6.1 页面层

不建议照搬 Vue 页面结构。

当前项目建议落在已有页面中增量改造：

- `frontend/src/features/interviews/InterviewsPage.tsx`
- `frontend/src/features/interviews/InterviewSessionRoute.tsx`
- `frontend/src/features/interviews/InterviewReportPage.tsx`
- `frontend/src/features/system/SystemPage.tsx`

## 6.2 面试准备页

当前项目没有独立“面试准备中心”，但已有启动台。

建议首期在当前 `InterviewsPage` 上补充：

- 目标岗位摘要
- 难度倾向
- 重点能力多选
- 展示并可选覆盖“个人档案”里的简历摘要/项目摘要

不建议首期为了模仿外部项目而单独新建一整页复杂流程。

## 6.3 管理入口

建议不要新增独立设置站点。

推荐方式：

- 在现有 `SystemPage` 下增加 `Interview Bank` 管理分区
- 或新增 `/system/interview-bank` 子页

理由：

- 当前项目管理员已经集中在系统状态页操作
- 不需要再造第二个“Settings”

## 6.4 报告页增强

在现有报告页上增加：

- 本场追问策略摘要
- 命中知识点数
- 建议补强方向
- 可选“本场覆盖知识域”

不建议首期把报告页改成完全不同的产品风格。

普通用户报告页不展示题库版本号；版本号属于管理端治理信息。

## 6.5 前端状态建议

建议新增或扩展：

- `frontend/src/stores/interviewBankStore.ts`
- 扩展 `frontend/src/stores/interviewSessionStore.ts`

其中：

- `interviewSessionStore.ts` 负责面试运行态、准备页参数和每轮检索摘要
- `interviewBankStore.ts` 负责管理员题库维护态

不要把题库管理状态塞进 `systemStore.ts`，否则系统状态页会逐渐失去边界。

## 7. 简历画像与 Mentor 策略

## 7.1 简历画像

首期不建议直接接 PDF 解析。

推荐过渡方案：

- 在“个人档案”中新增可选字段，支持手填：
  - 个人简历摘要
  - 项目经历摘要
- 面试准备页只读取并展示这些字段，不强制重复填写

后续再考虑：

- 上传 PDF
- 服务端解析
- 生成结构化画像

## 7.2 AI Mentor

首期不建议直接做独立 Mentor 页。

推荐先做：

- 面试报告里的“下一步建议”
- 仪表盘里的“面试专项建议”

等题库与动态追问跑稳后，再评估是否值得拆成独立入口。

## 8. 与现有模块的耦合点

## 8.1 学习仪表盘

新增的面试追问结果应继续回流：

- `learningPlan`
- `reviewCalendar`
- `weakPoints`
- `dashboard recommendations`

## 8.1A 与案例工坊的边界

已确认：

- 案例工坊是“真实场景故障”沉淀模块
- 面试题库是“技术面试训练素材”模块

首期不做：

- 案例工坊自动转面试题库
- 面试题库自动反写案例工坊

如果后续真的要建立联动，也应通过显式人工映射，而不是自动流转。

## 8.2 系统状态页

应增加可观测项：

- 面试题库总量
- 已发布数量
- `published` 数
- `failed` 索引数
- 开放组合数

首期系统状态页只展示题库治理摘要，不承载题库管理操作。

题库管理入口应放在现有 `admin` 区域，不新建独立后台。

## 8.3 权限模型

建议继续沿用：

- `student`
- `instructor`
- `admin`

已确认：首期题库维护只给 `admin`。

具体限制：

- `student`：不能看到题库管理入口
- `instructor`：继续负责案例工坊初审，不参与题库导入、发布、归档和重建索引
- `admin`：独占题库管理、发布态切换和索引维护

## 8.4 系统级 Provider 保持不变

外部项目有“用户自定义 LLM Provider 配置”。

当前项目首期不建议迁过去，原因：

1. 当前项目已有系统级 AI Router 和系统状态页。
2. 比赛场景更需要统一稳定的模型线路。
3. 用户级 provider 会把调试、额度、安全和演示复杂度同时放大。

因此首期建议：

- 继续使用当前系统级 Provider
- 把动态检索接入到当前统一 AI Router
- 后续如果项目目标转向多租户产品，再评估用户级 provider

## 9. 不建议首期引入的外部能力

以下能力可以借鉴思路，但不建议首期直接引入：

- 用户自定义 LLM Provider 配置
- Qdrant
- embedding-service
- MySQL
- 视频面试情绪分析
- 独立运营统计后台

原因不是这些能力没价值，而是它们与当前项目的接入收益/复杂度比不合适。

## 10. 风险与应对

## 10.1 风险：动态检索引入后面试反馈不稳定

应对：

- 保留当前固定规则回退
- 对检索上下文设置注入上限
- 在报告中记录回退原因

## 10.2 风险：面试题库和现有 `InterviewQuestion` 双模型混乱

应对：

- 明确“开场题”和“追问原子”分层
- 不把所有责任都压给 `InterviewQuestion`

## 10.3 风险：管理端做太大

应对：

- 首期只做导入/发布/搜索/重建
- 不追求一次性复刻外部项目全量后台

## 10.3A 风险：案例工坊和面试题库边界混乱

应对：

- 首期明确模块分离
- 案例工坊继续服务真实场景故障沉淀
- 面试题库只服务面试开场题与追问资源
- 不做自动互转

## 10.4 风险：比赛演示环境不稳定

应对：

- 首期不引入 Qdrant
- 首期不引入第二数据库
- 首期所有新能力都必须可降级

## 10.5 风险：管理端导入错误导致题库脏数据

应对：

- 首期就设计“校验 -> 预览 -> 发布”三段式
- 发布前保留预览统计与错误列表
- 仅 `published` 才可检索
- `archived` 不可检索但保留历史

## 11. 实施顺序

### Step 1

补领域模型和数据库表：

- `InterviewKnowledgeAtom`
- `InterviewKnowledgeBatch`
- `InterviewRetrievalLog`

### Step 2

补管理接口和最小管理界面。

### Step 3

改造 `handlers_interviews.go` 与 `agent/interview.go`，接入动态检索决策层。

### Step 4

增强报告页和系统状态页。

### Step 5

把结果回流到学习闭环。

## 11.1 建议任务拆分

可以按下面顺序拆成可独立交付的实现任务：

1. 数据模型与 migration
2. store 层 CRUD 与索引同步
3. 管理端 API
4. 管理端前端页面
5. 面试准备参数扩展
6. 面试运行时检索步骤
7. 报告页增强
8. 仪表盘回流
9. 系统状态观测项
10. 回归测试与演示脚本

## 11.2 测试建议

### 后端

- 题库导入/发布/归档单测
- 检索决策单测
- 弱召回回退单测
- 面试报告序列化兼容测试

### 前端

- 面试准备参数传递测试
- 管理端交互测试
- 报告页新增摘要渲染测试

### 端到端

- 管理员发布题库 -> 学员开始面试 -> 命中题库追问 -> 报告显示摘要
- pgvector 不可用 -> 自动回退 -> 面试仍可完成

## 12. 最终建议

如果目标是“尽快把外部项目的价值吸进当前系统”，最佳落地方式不是“接整仓”，而是：

1. 用当前 Go/React 架构承接
2. 先落题库 + 动态追问
3. 再做准备页、报告和 Mentor 增强
4. 最后再评估是否需要更重的向量基础设施或用户级模型配置

## 13. 实施清单摘要

### A. 数据层

- 新增 `interview_knowledge_atoms`
- 新增 `interview_knowledge_atom_versions`
- 新增 `interview_knowledge_batches`
- 新增 `interview_retrieval_logs`
- 扩展 `interview_sessions`
- 扩展 `users.profile`
- 扩展 `interview_sessions.evaluations`

### B. 后端能力

- 导入校验 / 预览 / 发布
- 题目归档 / 恢复归档
- 在线编辑 + 版本写入
- 版本历史查询
- `vector_status` 治理
- 开场题筛选
- 追问检索与回退
- 报告摘要输出

### C. 管理端

- 题库列表
- 单题详情
- 批次详情
- 版本历史列表 / 详情
- `archived` / `vector_status=failed` / `version_type` 筛选
- 组合命中与关闭原因展示

### D. 用户端

- 面试准备参数增强
- 面试运行时追问增强
- 报告页追问摘要增强
- 不展示治理信息，如题库版本号

### E. 观测与降级

- 系统状态页题库治理摘要
- 索引失败不影响开场题普通筛选
- 追问检索失败回退现有规则链路

### F. 验证顺序

1. migration 与 store
2. 管理端 API
3. 管理端页面
4. 面试运行时链路
5. 报告页
6. 系统状态页
7. 回归测试
