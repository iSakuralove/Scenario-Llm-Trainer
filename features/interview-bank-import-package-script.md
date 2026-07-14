# 面试题库导入包脚本

## 目标

补齐文档里仍未落地的“面试题库导入包脚本”，提供一个本地 CLI，把原始 JSON 题目数据规范化为当前 admin 导入接口可直接消费的标准导入包。

## 修改范围

- 新增 [scripts/interview_bank_import.py](/G:/计算机设计大赛/scripts/interview_bank_import.py)
- 新增脚本单测 [scripts/test_interview_bank_import.py](/G:/计算机设计大赛/scripts/test_interview_bank_import.py)
- 更新后端契约与架构说明

## 核心实现

- 支持三种 JSON 输入形态：
  - 单个 atom 对象
  - atom 数组
  - 带 `items` / `atoms` 的包结构
- 输出统一的标准导入包结构：
  - `batchId`
  - `items`
  - `validationReport`
  - `reviewReport`
- 自动规范化：
  - snake_case / camelCase 兼容
  - `title` 可回退到 `subject`
  - `domain / category / difficulty / questionRole / status` 默认值填充
  - `principles / pitfalls / followUpPaths` 统一转成字符串数组
  - 基础去空值、去重、ID 去重
- 本地校验对齐当前后端核心约束：
  - 类别 / 难度 / questionRole / status 枚举
  - 必填字段
  - `principles / pitfalls / followUpPaths` 至少 2 条

## 影响范围

- 不新增后端 API。
- 不改 admin 导入 validate/publish 逻辑。
- 为后续真正的原始素材抽取链路保留了标准导入包这一中间层。

## 验证方式

- `python scripts/test_interview_bank_import.py`
- `python scripts/interview_bank_import.py --help`

## 已知限制

- 当前最小版只支持 JSON 输入。
- PDF / DOCX / TXT 自动抽取和 LLM 生成链路仍未纳入这次核心验收。
- 当前脚本只生成本地导入包，不直接调用 admin validate/publish 接口。
