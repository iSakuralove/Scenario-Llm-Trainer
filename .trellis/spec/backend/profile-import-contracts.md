# Profile Resume Import Contracts

## 适用范围

修改 `POST /api/v1/users/me/profile/import`、简历文本抽取或 `UserProfile` 回填行为时，必须遵守本契约。

## 输入与持久化契约

- multipart 字段固定为 `file`，最大读取 `2 MiB`。
- 支持扩展名：`TXT / MD / DOCX / PDF`。
- `TXT / MD` 直接按 UTF-8 文本读取；`DOCX` 读取 `word/document.xml`；`PDF` 使用现有 PDF 解析依赖提取纯文本。
- 导入成功后仍返回更新后的 `User`，不新增独立响应 schema。
- `resume_summary` 每次使用本次解析结果更新；只有成功提取到非空值时，才覆盖已有 `target_role` 或 `project_summary`。

## 结构化提取契约

- 目标岗位标签支持：`求职意向 / 目标岗位 / 应聘岗位 / Target Role / Position`。
- 项目章节支持：`项目经历 / 项目经验 / Projects / Project Experience`。
- 标签必须是行首标签，并使用中文冒号、英文冒号或独立章节标题；不要用任意子串包含判断，避免把正文中的 `position`、`experience` 误识别为标题。
- `project_summary` 从项目标题后开始，到下一个可识别简历章节标题前结束。
- `resume_summary` 保留其余非空行，但排除目标岗位行和完整项目章节；如果排除后为空，则回退为原始文本。

## 错误与边界

| 条件 | 行为 |
|---|---|
| 缺少文件、空文件、超过大小限制 | 返回 `400` 对应导入错误 |
| 不支持的扩展名 | 返回 `unsupported resume format` |
| DOCX/PDF 无法解析 | 返回明确的无效文件错误，不保存半成品 |
| 未识别到目标岗位或项目章节 | 保留原字段，只更新 `resume_summary` |
| 项目章节后存在工作/教育/技能章节 | 必须在新章节处停止，不能吞入 `project_summary` |

## 必需测试

- 中文“求职意向 + 项目经历”可提取 `target_role` 和 `project_summary`。
- 英文 `Target Role + Project Experience + Work Experience` 能在工作经历前结束项目段。
- `resume_summary` 不残留目标岗位行、项目标题或项目正文。
- 原有 TXT、PDF 导入和无效 PDF 回归继续通过。

## Wrong vs Correct

错误：用 `strings.Contains` 判断章节，或按 `idx+1` 切中文冒号，容易误命中正文并破坏 UTF-8。

正确：只匹配行首标签，显式处理 `：` / `:`，并按完整章节范围构造摘要。
