# 面试题库导入脚本原始素材支持

## 目标

把导入包脚本从“只支持 JSON”推进到原始素材链路，支持把文档型输入转换成标准题库导入包。

## 修改范围

- 扩展 [interview_bank_import.py](/G:/计算机设计大赛/scripts/interview_bank_import.py)
- 扩展 [test_interview_bank_import.py](/G:/计算机设计大赛/scripts/test_interview_bank_import.py)
- 更新架构与导入脚本说明

## 核心实现

- 新增输入类型支持：
  - `json`
  - `txt`
  - `md`
  - `pdf`（依赖存在时）
  - `docx`（依赖存在时）
- `--input` 支持文件或目录扫描。
- 文档输入路径新增：
  - 文本提取
  - 长文本切块
  - 调用 OpenAI 兼容 `chat/completions`
  - 解析模型返回的 JSON 原子
- JSON 规范化路径保持兼容，不影响之前的标准导入包生成。

## 影响范围

- 不改后端 admin validate/publish 接口。
- 脚本现在既能处理本地 JSON，也能处理原始文档到导入包的转换。

## 验证方式

- `python scripts/test_interview_bank_import.py`
- `python scripts/interview_bank_import.py --help`

## 已知限制

- 文档输入模式依赖可用的 OpenAI 兼容 chat API。
- PDF / DOCX 解析需要本地存在 `PyPDF2` / `python-docx`。
- 当前测试不直连真实大模型，只覆盖文件收集和切块等本地行为。
