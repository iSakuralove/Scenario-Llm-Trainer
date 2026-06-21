# 忽略本地 agent 与记忆产物 PRD

## 目标

把当前工作区中与本次业务代码无关、属于本地 AI 工具或历史生成产物的未跟踪路径加入 `.gitignore`，降低后续 `git status` 噪音。

## 已确认事实

- 当前未跟踪路径包含 `.agents/`、`.codex/`、`AGENTS.generated.md`、`CONTEXT.md`、`MEMORY.md`、`docs/adr/`、`docs/memos-agent-prompt.md`、`features/2026-06-11-agents-generated-doc.md`。
- `.trellis/tasks/06-21-ignore-local-agent-artifacts/` 是当前 Trellis 任务目录，不应加入 `.gitignore`。
- `.gitignore` 已存在 “Local tool state” 和 “Local review and analysis output” 分区，可在现有结构内追加规则。

## 需求

- 精确忽略当前无关的本地 agent、Codex 状态、记忆上下文和历史生成文档路径。
- 不忽略 `.trellis/`、源码目录、正常 `features/` 目录或未来通用文档目录。
- 修改后 `git status --short` 不再显示这些无关路径，仅保留当前 Trellis 任务目录和 `.gitignore` 自身变更。

## 验收标准

- `.gitignore` 包含上述无关路径规则。
- `git status --short` 中不再出现 `.agents/`、`.codex/`、`AGENTS.generated.md`、`CONTEXT.md`、`MEMORY.md`、`docs/adr/`、`docs/memos-agent-prompt.md`、`features/2026-06-11-agents-generated-doc.md`。
- 不新增依赖，不修改业务代码。

## 不做

- 不清理或删除这些本地文件。
- 不忽略当前 Trellis 任务目录。
- 不处理其他历史任务或业务提交。
