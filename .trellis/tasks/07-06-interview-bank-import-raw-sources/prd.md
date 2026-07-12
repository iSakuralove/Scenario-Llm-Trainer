# 面试题库导入脚本原始素材支持 PRD

## 目标

把当前仅支持 JSON 的导入包脚本继续推进到原始素材链路，至少补齐：

- TXT / MD 输入
- PDF / DOCX 可选解析入口
- 文档切块
- 调用 OpenAI 兼容 chat 接口把文档块转成标准导入原子

## 已确认事实

- 当前仓库已新增 `scripts/interview_bank_import.py`，但只支持 JSON 输入。
- 参考仓库 `tmp/AI-Interview-ref/scripts/question_bank_import.py` 已有：
  - 文本提取
  - chunk
  - OpenAI 兼容 chat 调用
  - 原子 JSON 解析

## 需求

1. 输入
   - 支持：
     - `.json`
     - `.txt`
     - `.md`
     - `.pdf`（依赖存在时）
     - `.docx`（依赖存在时）
   - `--input` 支持文件或目录。

2. 文档转换
   - 文档输入时必须要求默认 `--category`。
   - 对长文本分块。
   - 每块调用 OpenAI 兼容 chat 接口生成标准原子 JSON。

3. 兼容性
   - JSON 规范化路径保持可用。
   - 如果缺少 PDF / DOCX 解析依赖，要给出明确错误提示。

4. 测试
   - 至少覆盖：
     - 文本分块
     - 文件收集
     - JSON + 文档路径的路由逻辑

## 验收标准

- 脚本支持 `--input` 目录扫描。
- 支持 TXT / MD 文档转原子。
- 支持 PDF / DOCX 的可选解析入口。

## 不做

- 不在测试里直连真实外部大模型。
- 不实现后台自动 publish。
