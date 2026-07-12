# 个人档案导入简历文本

## 目标

把个人档案从纯手填推进到“可上传文本简历导入摘要 + 最小结构化提取”的闭环。

## 修改范围

- 后端新增 `POST /api/v1/users/me/profile/import`
- 新增 [profile_import.go](/G:/计算机设计大赛/backend/internal/httpapi/profile_import.go)
- 新增 [profile_import_test.go](/G:/计算机设计大赛/backend/internal/httpapi/profile_import_test.go)
- 前端 Profile 页面新增“导入简历文本”入口

## 核心实现

- 首版上传链路支持：
  - `txt`
  - `md`
  - `docx`
  - `pdf`
- 启发式提取：
  - `target_role`
  - `resume_summary`
  - `project_summary`
- 后端会把解析出的文本写入 `resume_summary`
- 当文本中存在明显的“求职意向 / 目标岗位 / 应聘岗位 / 项目经历 / Projects”段落时，会尝试提取 `target_role` 与 `project_summary`
- 结构化章节使用行首标签识别；项目段遇到工作、教育、技能等下一章节时停止，避免把后续经历误并入项目摘要
- `resume_summary` 会排除目标岗位行和完整项目段；若排除后为空，则回退保留原始文本
- `docx` 解析使用标准库 `archive/zip + encoding/xml` 读取 `word/document.xml`
- `pdf` 解析使用 `github.com/ledongthuc/pdf`
- 前端上传成功后会刷新当前会话里的用户 profile，并回填到“简历摘要”文本框

## 影响范围

- 不新增数据库字段。
- 不做 LLM 摘要压缩，只保存导入文本。

## 验证方式

- `go test ./internal/httpapi -run 'Test(ProfileImport|ExtractStructuredResumeFields)' -count=1`
- `go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前结构化提取仍是启发式规则，不是 LLM 摘要或完整简历 schema。
- 当前不会解析复杂表格、双栏视觉布局，也不会推断缺少明确标签的目标岗位。
