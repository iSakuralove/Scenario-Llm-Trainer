import { expect, type Route, test } from '@playwright/test'
import { loginAs } from './helpers/auth'

test('scenario filters expose clickable tag chips', async ({ page }) => {
  const requests: string[] = []
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    requests.push(url.searchParams.get('tag') ?? '')
    await fulfillJSON(route, { list: [scenario('scenario-1'), scenario('scenario-2')], total: 2 })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await expect(page.locator('.tag-filter-chips button', { hasText: 'MySQL' })).toBeVisible()
  await page.locator('.tag-filter-chips button', { hasText: 'MySQL' }).click()

  await expect(page.locator('.tag-filter-chips button.active', { hasText: 'MySQL' })).toBeVisible()
  await expect.poll(() => requests.at(-1)).toBe('MySQL')
})

test('interview answer supports markdown preview and readable streaming feedback', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'rich-interview-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), status: 'active', submissions: [], evaluations: [] },
    })
  })
  await page.route('**/api/v1/interviews/sessions/rich-interview-session/submit', async (route) => {
    await route.fulfill({
      contentType: 'text/event-stream',
      body: [
        ['stage', { message: 'AI 正在评分', step: 'llm' }],
        ['delta', { chunk: '{"highlights":["hidden json"]}', displayable: false }],
        ['delta', { chunk: '总分：86 分\n', displayable: true }],
        ['delta', { chunk: '亮点：定位路径清晰\n', displayable: true }],
      ['finish', { evaluation: interviewEvaluation(), session_status: 'follow_up_1_presented', session: { ...interviewSession(), status: 'follow_up_1_presented', evaluations: [interviewEvaluation()] } }],
      ].map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join(''),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  await page.getByLabel('Markdown 回答').fill('## 定位路径\n- 查看慢查询日志\n\n```python\nprint("hello world")\n```')
  await page.getByRole('button', { name: /预览/ }).click()

  await expect(page.locator('.markdown-preview h2', { hasText: '定位路径' })).toBeVisible()
  await expect(page.locator('.markdown-preview .code-window-header')).toContainText('Python')
  await expect(page.locator('.markdown-preview .window-dot')).toHaveCount(3)
  await expect(page.locator('.markdown-preview .token.function', { hasText: 'print' })).toBeVisible()
  await expect(page.locator('.markdown-preview .token.string', { hasText: '"hello world"' })).toBeVisible()

  await page.getByRole('button', { name: /提交/ }).click()
  const feedback = page.getByTestId('interview-stream-feedback')
  await expect(feedback).toContainText('本轮评分已生成')
  await expect(feedback).toContainText('总分')
  await expect(feedback.locator('.interview-feedback-details')).not.toHaveAttribute('open', '')
  await expect(feedback.locator('.interview-feedback-body')).toHaveCSS('display', 'none')
  await expect(feedback).not.toContainText('highlights')
  await feedback.locator('summary').click()
  await expect(feedback.locator('.interview-feedback-body')).toContainText('亮点')
  await expect(feedback.locator('.interview-feedback-body')).toContainText('定位路径清晰')
  await feedback.locator('summary').click()
  await expect(feedback.locator('.interview-feedback-details')).not.toHaveAttribute('open', '')
  await expect(feedback.locator('.interview-feedback-body')).toHaveCSS('display', 'none')
})

test('interview answer prevents duplicate templates and imports markdown files', async ({ page }) => {
  let submittedContent = ''
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'markdown-import-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'markdown-import-session', status: 'active', submissions: [], evaluations: [] },
    })
  })
  await page.route('**/api/v1/interviews/sessions/markdown-import-session/submit', async (route) => {
    submittedContent = JSON.parse(route.request().postData() || '{}').content ?? ''
    await route.fulfill({
      contentType: 'text/event-stream',
      body: [
        ['stage', { message: 'AI scoring', step: 'llm' }],
        ['delta', { chunk: 'Score 86\n', displayable: true }],
        ['finish', { evaluation: interviewEvaluation(), session_status: 'follow_up_1_presented', session: { ...interviewSession(), id: 'markdown-import-session', status: 'follow_up_1_presented', evaluations: [interviewEvaluation()] } }],
      ].map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join(''),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  const editor = page.getByTestId('interview-answer-editor')
  await page.getByRole('button', { name: /回答模板/ }).click()
  const pathTemplate = page.locator('.template-step', { hasText: '定位路径' })
  await pathTemplate.click()
  expect(((await editor.inputValue()).match(/## 定位路径/g) ?? []).length).toBe(1)
  await expect(pathTemplate).toBeDisabled()

  await page.getByTestId('markdown-file-input').setInputFiles({
    name: 'answer.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('## Imported Answer\n- Check slow query log\n\n```sql\nEXPLAIN SELECT * FROM orders;\n```'),
  })
  await expect(editor).toHaveValue(/Imported Answer/)
  await chooseMarkdownMenu(page, '选择 Mermaid 图类型', '流程图')
  await expect(editor).toHaveValue(/```mermaid/)
  await expect(editor).toHaveValue(/graph LR/)
  await expect(editor).not.toHaveValue(/发现问题|定位原因|修复验证/)
  await expect(editor).not.toHaveValue(/定位路径/)
  await expect(page.locator('.markdown-preview h2', { hasText: 'Imported Answer' })).toBeVisible()
  await expect(page.locator('.markdown-preview .code-window-header', { hasText: 'SQL' })).toBeVisible()
  await expect(page.locator('.markdown-preview .token.keyword', { hasText: 'EXPLAIN' })).toBeVisible()

  await expect(page.getByRole('note')).toContainText('提交时仍会使用原始回答内容')
  const composer = page.locator('.markdown-composer')
  await expect(page.getByRole('note')).toHaveCSS('background-color', 'rgba(142, 240, 219, 0.12)')
  await expect(page.getByRole('note')).toHaveCSS('color', 'rgb(204, 251, 241)')
  await composer.getByRole('button', { name: '全屏', exact: true }).click()
  await expect(page.locator('.markdown-composer.expanded')).toBeVisible()
  await expect(page.locator('.markdown-composer.expanded')).toHaveCSS('background-color', 'rgb(10, 15, 23)')
  await expect(page.locator('.markdown-composer.expanded')).toHaveCSS('border-top-color', 'rgba(142, 240, 219, 0.32)')
  await expect(page.locator('.markdown-composer.expanded .markdown-preview-panel')).toHaveCSS('background-color', 'rgba(255, 255, 255, 0.04)')
  await expect(page.locator('.markdown-preview h2', { hasText: 'Imported Answer' })).toBeVisible()
  await composer.getByRole('button', { name: '退出全屏', exact: true }).click()
  await expect(page.locator('.markdown-composer.expanded')).toHaveCount(0)
  await expect(page.locator('.answer-panel .primary-button')).toBeEnabled()

  await page.locator('.answer-panel .primary-button').click()
  await expect(page.getByTestId('interview-stream-feedback')).toContainText('总分 86 分')
  await expect(page.getByTestId('interview-stream-feedback')).not.toContainText('highlights')
  expect(submittedContent).toContain('Imported Answer')
  expect(submittedContent).toContain('EXPLAIN SELECT')
  expect(submittedContent).toContain('```mermaid')
  expect(submittedContent).toContain('graph LR')
  expect(submittedContent).not.toContain('发现问题')
})

test('interview voice confirmation resets after answer edits and blocks submit until reconfirmed', async ({ page }) => {
  let submitCalls = 0
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'voice-confirm-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'voice-confirm-session', status: 'active', submissions: [], evaluations: [] },
    })
  })
  await page.route('**/api/v1/assets', async (route) => {
    await fulfillJSON(route, {
      id: 'voice-asset-1',
      kind: 'voice',
      filename: 'answer.wav',
      mime_type: 'audio/wav',
      size: 2048,
      url: '/assets/voice-asset-1',
      checksum: 'sha256-demo',
      created_at: new Date().toISOString(),
    })
  })
  await page.route('**/api/v1/interviews/sessions/voice-confirm-session/voice', async (route) => {
    await fulfillJSON(route, {
      asset: {
        id: 'voice-asset-1',
        kind: 'voice',
        filename: 'answer.wav',
        mime_type: 'audio/wav',
        size: 2048,
        url: '/assets/voice-asset-1',
        checksum: 'sha256-demo',
        created_at: new Date().toISOString(),
      },
      transcript: '原始转写答案',
      duration_seconds: 8,
      status: 'ok',
      quality: {
        detected_language: 'zh',
        stt_confidence: 0.98,
        topic_relevance_score: 0.92,
        keyword_hits: ['MySQL'],
        transcript_suggestions: [],
        reasons: [],
        status: 'accepted',
      },
    })
  })
  await page.route('**/api/v1/interviews/sessions/voice-confirm-session/submit', async (route) => {
    submitCalls += 1
    await route.fulfill({
      contentType: 'text/event-stream',
      body: [
        ['stage', { message: 'AI scoring', step: 'llm' }],
        ['delta', { chunk: 'Score 90\n', displayable: true }],
        ['finish', { evaluation: interviewEvaluation(), session_status: 'follow_up_1_presented', session: { ...interviewSession(), id: 'voice-confirm-session', status: 'follow_up_1_presented', evaluations: [interviewEvaluation()] } }],
      ].map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join(''),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  await page.getByTestId('voice-file-input').setInputFiles({
    name: 'answer.wav',
    mimeType: 'audio/wav',
    buffer: Buffer.from('RIFFdemoWAVEfmt '),
  })

  const submitButton = page.getByTestId('submit-interview-answer')
  await expect(page.getByTestId('confirm-voice-transcript')).toBeVisible()
  await expect(submitButton).toBeDisabled()

  await page.getByTestId('confirm-voice-transcript').click()
  await expect(submitButton).toBeEnabled()

  const editor = page.getByTestId('interview-answer-editor')
  await editor.fill('原始转写答案，补充人工修改')
  await expect(submitButton).toBeDisabled()
  await expect(page.getByText('转写文本已修改，请重新确认')).toBeVisible()

  await submitButton.click({ force: true })
  expect(submitCalls).toBe(0)
  await expect(page.getByTestId('interview-stream-feedback')).toContainText('等待确认转写文本')

  await page.getByTestId('confirm-voice-transcript').click()
  await expect(submitButton).toBeEnabled()
  await submitButton.click()
  expect(submitCalls).toBe(1)
  await expect(page.getByTestId('interview-stream-feedback')).toContainText('总分')
})

test('markdown toolbar uses dropdowns and inserts only syntax markers', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'markdown-toolbar-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'markdown-toolbar-session', status: 'active', submissions: [], evaluations: [] },
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  const editor = page.getByTestId('interview-answer-editor')
  await expect(page.locator('.toolbar-menu')).toHaveCount(4)
  await expect(page.getByRole('button', { name: 'H1' })).toHaveCount(0)
  await expect(page.getByText(/默认：/)).toHaveCount(0)
  await page.getByLabel('选择标题级别').click()
  await expect(page.getByRole('menuitem', { name: '一级标题', exact: true })).toContainText('Ctrl+1')
  await expect(page.getByRole('menuitem', { name: '一级标题', exact: true }).locator('.menu-option-label')).toContainText('一级')
  await expect(page.getByRole('menuitem', { name: '一级标题', exact: true }).locator('.menu-option-label')).not.toContainText('一级标题')
  await page.keyboard.press('Escape')
  await page.getByLabel('选择列表类型').click()
  await expect(page.getByRole('menuitem', { name: '无序列表', exact: true }).locator('.menu-option-label')).toContainText('● 无序')
  await page.keyboard.press('Escape')
  await page.getByLabel('选择代码块语言').click()
  await expect(page.getByRole('menuitem', { name: 'JavaScript', exact: true }).locator('.language-badge')).toContainText('JS')
  await page.keyboard.press('Escape')

  await chooseMarkdownMenu(page, '选择标题级别', '一级标题')
  await chooseMarkdownMenu(page, '选择标题级别', '二级标题')
  await chooseMarkdownMenu(page, '选择标题级别', '三级标题')
  await chooseMarkdownMenu(page, '选择列表类型', '无序列表')
  await chooseMarkdownMenu(page, '选择列表类型', '有序列表')
  await chooseMarkdownMenu(page, '选择代码块语言', 'Python')
  await chooseMarkdownMenu(page, '选择代码块语言', 'Java')
  await chooseMarkdownMenu(page, '选择 Mermaid 图类型', '思维导图')
  await page.getByRole('button', { name: '引用' }).click()

  await expect(editor).toHaveValue('#\n\n##\n\n###\n\n- \n\n1. \n\n```python\n\n```\n\n```java\n\n```\n\n```mermaid\nmindmap\n\n```\n\n>')
  await expect(editor).not.toHaveValue(/定位路径|关键步骤|验证结果|EXPLAIN|发现问题|关键判断/)
})

test('markdown editor supports tab indentation and typora shortcuts', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'markdown-shortcut-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'markdown-shortcut-session', status: 'active', submissions: [], evaluations: [] },
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  const editor = page.getByTestId('interview-answer-editor')
  await editor.fill('定位路径\n关键命令')
  await editor.evaluate((node) => {
    const textarea = node as HTMLTextAreaElement
    textarea.setSelectionRange(0, 0)
  })
  await editor.press('Control+1')
  await expect(editor).toHaveValue('# 定位路径\n关键命令')

  await editor.evaluate((node) => {
    const textarea = node as HTMLTextAreaElement
    textarea.setSelectionRange(textarea.value.length, textarea.value.length)
  })
  await editor.press('Tab')
  await expect(editor).toHaveValue('# 定位路径\n关键命令  ')
  await editor.press('Shift+Tab')
  await expect(editor).toHaveValue('# 定位路径\n关键命令')

  await editor.press('Control+Shift+]')
  await expect(editor).toHaveValue('# 定位路径\n- 关键命令')
  await editor.press('Control+Shift+K')
  await expect(editor).toHaveValue('# 定位路径\n```\n- 关键命令\n```')
})

test('markdown ordered list continues and exits with enter', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'markdown-list-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'markdown-list-session', status: 'active', submissions: [], evaluations: [] },
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  const editor = page.getByTestId('interview-answer-editor')
  await editor.fill('1. 定位路径')
  await editor.press('End')
  await editor.press('Enter')
  await expect(editor).toHaveValue('1. 定位路径\n2. ')

  await editor.type('关键命令')
  await editor.press('Enter')
  await expect(editor).toHaveValue('1. 定位路径\n2. 关键命令\n3. ')

  await editor.press('Enter')
  await expect(editor).toHaveValue('1. 定位路径\n2. 关键命令\n')

  await editor.fill('- 排查入口')
  await editor.press('End')
  await editor.press('Enter')
  await expect(editor).toHaveValue('- 排查入口\n- ')
  await editor.type('查看慢查询')
  await editor.press('Enter')
  await expect(editor).toHaveValue('- 排查入口\n- 查看慢查询\n- ')
  await editor.press('Enter')
  await expect(editor).toHaveValue('- 排查入口\n- 查看慢查询\n')
})

test('markdown list menu keeps focus and exits empty list item with enter', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    await fulfillJSON(route, {
      session_id: 'markdown-list-menu-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession(), id: 'markdown-list-menu-session', status: 'active', submissions: [], evaluations: [] },
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await startInterviewFromLaunchpad(page)

  const editor = page.getByTestId('interview-answer-editor')
  await chooseMarkdownMenu(page, '选择列表类型', '有序列表')
  await expect(editor).toBeFocused()
  await expect(editor).toHaveValue('1. ')

  await editor.press('Enter')
  await expect(editor).toHaveValue('')

  await editor.press('Control+Shift+[')
  await expect(editor).toBeFocused()
  await expect(editor).toHaveValue('1. ')
  await editor.type('定位路径')
  await editor.press('Enter')
  await expect(editor).toHaveValue('1. 定位路径\n2. ')

  await editor.fill('')
  await chooseMarkdownMenu(page, '选择列表类型', '无序列表')
  await expect(editor).toBeFocused()
  await expect(editor).toHaveValue('- ')
  await editor.press('Enter')
  await expect(editor).toHaveValue('')

  await editor.press('Control+Shift+]')
  await expect(editor).toBeFocused()
  await expect(editor).toHaveValue('- ')
  await editor.type('排查入口')
  await editor.press('Enter')
  await expect(editor).toHaveValue('- 排查入口\n- ')
})

async function chooseMarkdownMenu(page: import('@playwright/test').Page, menuName: string, itemName: string) {
  await page.getByLabel(menuName).click()
  await page.getByRole('menuitem', { name: itemName, exact: true }).click()
}

async function fulfillJSON(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

async function startInterviewFromLaunchpad(page: Page) {
  await page.getByTestId('interview-track-section').getByRole('button', { name: '开始面试' }).click()
}

function scenario(id: string) {
  return {
    id,
    title: `Scenario ${id}`,
    description: 'A database incident for tag filtering.',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['MySQL', 'cache'],
    content: { reveal_strategy: { surface_clues: [], deep_clues: [], distractors: [] }, architecture_diagram: '', reference_links: [] },
    status: 'active',
    source: 'llm_generated',
    created_by: 'user-admin',
    version: 1,
    is_sanitized: true,
  }
}

function interviewQuestion() {
  return {
    id: 'rich-question',
    title: 'MySQL slow query',
    description: 'Explain the diagnosis path.',
    domain: 'database',
    difficulty: 'L3',
    question_type: 'scenario_analysis',
    evaluation_dimensions: [],
    follow_up_strategies: [],
  }
}

function interviewSession() {
  return {
    id: 'rich-interview-session',
    user_id: 'demo-user',
    question_id: 'rich-question',
    status: 'active',
    current_round: 1,
    max_rounds: 2,
    submissions: [],
    evaluations: [],
  }
}

function interviewEvaluation() {
  return {
    round: 1,
    total_score: 86,
    dimension_scores: { technical_accuracy: 88, logical_completeness: 82, solution_feasibility: 86 },
    is_passed: true,
    highlights: ['定位路径清晰'],
    deficiencies: ['补充回滚验证'],
    follow_up_triggered: false,
    agent_trace: agentTrace(),
    created_at: new Date().toISOString(),
  }
}

function agentTrace() {
  const now = new Date().toISOString()
  return {
    run_id: 'run-e2e-rich',
    agent: 'interview_agent',
    mode: 'safe_summary',
    tool_count: 4,
    started_at: now,
    finished_at: now,
    steps: [
      { name: 'step-1', kind: 'analysis', status: 'success', summary: '完成安全分析', started_at: now, ended_at: now },
      { name: 'step-2', kind: 'rewrite', status: 'success', summary: '生成安全回复', started_at: now, ended_at: now },
      { name: 'step-3', kind: 'safety', status: 'success', summary: '通过安全检查', started_at: now, ended_at: now },
    ],
  }
}
