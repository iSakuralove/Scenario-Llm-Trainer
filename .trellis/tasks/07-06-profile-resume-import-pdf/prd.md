# 个人档案导入 PDF 简历 PRD

## 目标

把当前个人档案导入能力从 `TXT / MD / DOCX` 扩到 `PDF`，进一步接近“简历上传/解析”能力。

## 需求

1. 支持 `.pdf` 上传
2. 后端提取文本并写入 `resume_summary`
3. 不改前端交互结构

## 验收标准

- Profile import 接口接受 PDF 扩展名
- PDF 文本解析失败时返回明确错误

## 不做

- 不做 LLM 摘要压缩
