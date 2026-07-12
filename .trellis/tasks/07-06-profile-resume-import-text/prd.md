# 个人档案导入简历文本 PRD

## 目标

把“个人档案”从纯手填推进到“可上传简历文本导入摘要”的最小闭环。

首版范围：

- 支持 `TXT / MD / DOCX`
- 后端提取文本
- 写入 `resume_summary`
- 前端档案页提供上传入口

## 需求

1. 新接口
   - `POST /api/v1/users/me/profile/import`
   - multipart 上传单个文件

2. 支持格式
   - `txt`
   - `md`
   - `docx`
   - `pdf` 暂不纳入这条最小闭环

3. 行为
   - 解析出的文本写入 `resume_summary`
   - 不覆盖 `project_summary`
   - 空文件或不支持格式返回明确错误

4. 前端
   - Profile 页面提供上传按钮
   - 上传成功后刷新当前用户会话

## 验收标准

- 上传文本文件后，个人档案的 `resume_summary` 被更新
- Profile 页面能看到导入后的内容

## 不做

- 不做 PDF 解析
- 不做 LLM 摘要压缩
