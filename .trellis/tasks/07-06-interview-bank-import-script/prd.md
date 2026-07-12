# 面试题库导入包脚本 PRD

## 目标

补齐文档里仍未落地的“面试题库导入包脚本”，提供一个本地 CLI，把 JSON 原始题目数据规范化为当前项目 admin 导入接口可直接消费的标准导入包。

首版目标聚焦：

- JSON 原始输入
- 规范化输出
- 本地校验报告

文档/PDF/DOCX 自动抽取保留为可选增强路径，不作为这次的核心验收。

## 已确认事实

- 当前后端已经提供：
  - `POST /api/v1/admin/interview-bank/import/validate`
  - `POST /api/v1/admin/interview-bank/import/publish`
- 当前仓库没有对应的本地导入包生成脚本。
- AI-interview 参考仓库已有 `scripts/question_bank_import.py`，可借鉴其“原始输入 -> 标准导入包”思路。

## 需求

1. 输入
   - 支持 JSON 文件输入：
     - 单个 atom 对象
     - atom 数组
     - 已带 `items` / `atoms` 的包结构

2. 输出
   - 输出当前项目可消费的标准导入包 JSON。
   - 包内至少包含：
     - `batchId`
     - `items`
     - `validationReport`
     - `reviewReport`

3. 规范化
   - 统一映射到当前项目字段：
     - `id`
     - `title`
     - `subject`
     - `domain`
     - `category`
     - `difficulty`
     - `questionRole`
     - `sourceRef`
     - `tags`
     - `principles`
     - `pitfalls`
     - `followUpPaths`
     - `status`
   - 兼容 snake_case / camelCase 输入。
   - 对字符串/数组做 trim、去空值、基础去重。

4. 本地校验
   - 至少校验与后端一致的核心枚举和必填项。
   - 输出 errors / warnings。
   - 若存在 error，脚本退出码非 0。

5. 验证
   - 增加脚本级单测，覆盖规范化和校验。

## 验收标准

- 能把原始 JSON 输入转成当前项目 admin 导入接口兼容的标准导入包。
- 输出里包含本地 `validationReport` 与 `reviewReport`。
- 有单测覆盖基础规范化和校验行为。

## 不做

- 不强制接入真实后台上传。
- 不把 PDF/DOCX/LLM 生成链路作为本次核心验收。
- 不新增数据库或后端 API。
