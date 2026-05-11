import { expect, type Route, test } from '@playwright/test'
import { expectNoWhiteScreen, loginAs } from './helpers/auth'

test('student can complete a troubleshooting review', async ({ page }) => {
  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await expectNoWhiteScreen(page)
  await expect(page.getByRole('heading', { name: '排查工坊' })).toBeVisible()
  await expect(page.getByRole('button', { name: /生成题目|生成中/ })).toBeVisible()

  const firstStart = page.getByRole('button', { name: '开始排查' }).first()
  await expect(firstStart).toBeVisible()
  await firstStart.click()

  await expect(page.getByText('渐进式排查会话')).toBeVisible()
  await page.getByPlaceholder('提交最终根因答案').fill('缓存 key 规则变化导致命中率下降，数据库读请求升高。')
  await page.getByRole('button', { name: /提交答案/ }).click()

  await expect(page.getByRole('heading', { name: '排查复盘' })).toBeVisible()
  await expect(page.getByText('标准答案')).toBeVisible()
})

test('scenario generation resumes after switching to system status and returning', async ({ page }) => {
  const generatedQuestion = {
    ...scenarioQuestion('e2e-resumed-generation-question', 'E2E 恢复生成题目'),
    status: 'active',
    source: 'llm_generated',
  }
  let pollCount = 0

  await page.route('**/api/v1/system/status', async (route) => {
    await fulfill(route, systemStatus())
  })
  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-resume-job-1', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-resume-job-1', async (route) => {
    pollCount += 1
    await fulfill(route, {
      job: aiJob('e2e-resume-job-1', 'completed', 100, {
        stage: 'persisted',
        validated: true,
        result_question_id: generatedQuestion.id,
      }),
      question_id: generatedQuestion.id,
      question: generatedQuestion,
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    if (url.searchParams.get('difficulty') === generatedQuestion.difficulty) {
      await fulfill(route, { list: [generatedQuestion], total: 1 })
      return
    }
    await fulfill(route, { list: [scenarioQuestion('e2e-existing-generation-question', 'E2E 初始题目')], total: 1 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /生成题目/ }).click()
  await page.getByRole('button', { name: '开始生成' }).click()
  await expect(page.locator('.generation-status')).toContainText('e2e-res')

  await page.goto('/system')
  await expect(page.getByRole('heading', { name: '系统状态' })).toBeVisible()
  await page.goto('/scenarios')

  await expect(page.locator('.scenario-card').first()).toContainText('E2E 恢复生成题目')
  await expect(page.locator('.generation-status')).toContainText('题目已生成')
  expect(pollCount).toBeGreaterThan(0)
  expect(await page.evaluate(() => window.localStorage.getItem('scenario-generation-active-job'))).toBeNull()
})

test('scenario generation uses selected difficulty', async ({ page }) => {
  let requestedDifficulty = ''
  const generatedQuestion = {
    ...scenarioQuestion('e2e-difficulty-generation-question', 'E2E L4 生成题目'),
    difficulty: 'L4',
    status: 'active',
    source: 'llm_generated',
  }

  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    const body = route.request().postDataJSON() as { difficulty?: string }
    requestedDifficulty = body.difficulty ?? ''
    await fulfill(route, {
      job: aiJob('e2e-difficulty-job-1', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-difficulty-job-1', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-difficulty-job-1', 'completed', 100, {
        stage: 'persisted',
        validated: true,
        result_question_id: generatedQuestion.id,
      }),
      question_id: generatedQuestion.id,
      question: generatedQuestion,
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    if (url.searchParams.get('difficulty') === 'L4') {
      await fulfill(route, { list: [generatedQuestion], total: 1 })
      return
    }
    await fulfill(route, { list: [scenarioQuestion('e2e-existing-difficulty-question', 'E2E 初始题目')], total: 1 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByLabel('生成难度').selectOption('L4')
  await page.getByRole('button', { name: /生成题目/ }).click()

  await expect(page.locator('.scenario-card').first()).toContainText('L4')
  expect(requestedDifficulty).toBe('L4')
})

test('scenario generation difficulty is independent from list filters', async ({ page }) => {
  let requestedDifficulty = ''
  const generatedQuestion = {
    ...scenarioQuestion('e2e-independent-difficulty-generation-question', 'E2E 独立难度生成题目'),
    difficulty: 'L5',
    status: 'active',
    source: 'llm_generated',
  }

  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    const body = route.request().postDataJSON() as { difficulty?: string }
    requestedDifficulty = body.difficulty ?? ''
    await fulfill(route, {
      job: aiJob('e2e-independent-difficulty-job-1', 'running', 35, {
        stage: 'provider_call',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-independent-difficulty-job-1', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-independent-difficulty-job-1', 'completed', 100, {
        stage: 'persisted',
        validated: true,
        result_question_id: generatedQuestion.id,
      }),
      question_id: generatedQuestion.id,
      question: generatedQuestion,
    })
  })
  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    if (url.searchParams.get('difficulty') === 'L5') {
      await fulfill(route, { list: [generatedQuestion], total: 1 })
      return
    }
    await fulfill(route, { list: [scenarioQuestion('e2e-filter-difficulty-question', 'E2E 筛选题目')], total: 1 })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByLabel('生成难度').selectOption('L5')
  await page.locator('.filter-controls').getByLabel('难度').selectOption('L2')
  await page.getByRole('button', { name: /生成题目/ }).click()

  await expect(page.locator('.scenario-card').first()).toContainText('L5')
  expect(requestedDifficulty).toBe('L5')
})

test('student can stop scenario generation without adding router counts', async ({ page }) => {
  const telemetryCalls = 0
  let canceled = false

  await page.route('**/api/v1/system/ai', async (route) => {
    await fulfill(route, {
      provider: 'mock',
      model: 'mock',
      fallback: true,
      telemetry: {
        total_calls: telemetryCalls,
        successful_calls: telemetryCalls,
        failed_calls: 0,
        fallback_calls: 0,
        stream_calls: 0,
        json_calls: telemetryCalls,
        safety_rewrites: 0,
        validation_errors: 0,
        provider_calls: { mock: telemetryCalls },
        task_calls: telemetryCalls > 0 ? { scenario_generate: telemetryCalls } : {},
        recent_decisions: [],
        updated_at: new Date().toISOString(),
      },
    })
  })
  await page.route('**/api/v1/system/status', async (route) => {
    await fulfill(route, {
      ...systemStatus(),
      ai: {
        provider: 'mock',
        model: 'mock',
        fallback: true,
        telemetry: {
          total_calls: telemetryCalls,
          successful_calls: telemetryCalls,
          failed_calls: 0,
          fallback_calls: 0,
          stream_calls: 0,
          json_calls: telemetryCalls,
          safety_rewrites: 0,
          validation_errors: 0,
          provider_calls: { mock: telemetryCalls },
          task_calls: telemetryCalls > 0 ? { scenario_generate: telemetryCalls } : {},
          recent_decisions: [],
          updated_at: new Date().toISOString(),
        },
      },
    })
  })
  await page.route('**/api/v1/scenarios/generate/jobs', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-stop-job-1', 'running', 35, {
        stage: 'calling_model',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-stop-job-1/cancel', async (route) => {
    canceled = true
    await fulfill(route, {
      job: aiJob('e2e-stop-job-1', 'canceled', 100, {
        stage: 'canceled',
      }),
    })
  })
  await page.route('**/api/v1/ai/jobs/e2e-stop-job-1', async (route) => {
    await fulfill(route, {
      job: aiJob('e2e-stop-job-1', canceled ? 'canceled' : 'running', canceled ? 100 : 35, {
        stage: canceled ? 'canceled' : 'calling_model',
      }),
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: /生成题目/ }).click()
  await page.getByRole('button', { name: '开始生成' }).click()
  await expect(page.locator('.generation-status')).toContainText('AI 正在生成情景题')
  await page.getByRole('button', { name: '停止生成' }).click()
  await expect(page.locator('.generation-status')).toContainText('已停止')
  expect(await page.evaluate(() => window.localStorage.getItem('scenario-generation-active-job'))).toBeNull()

  await page.goto('/system')
  const routerStats = page.locator('.panel').filter({ hasText: 'Router 统计' })
  await expect(routerStats).toContainText('总调用')
  await expect(routerStats).toContainText('0')
})

test('student can submit troubleshooting final answer with markdown preview', async ({ page }) => {
  const question = { ...scenarioQuestion('e2e-markdown-scenario-question', 'E2E Markdown 最终答案题目'), status: 'active' }
  let submittedAnswer = ''

  await page.route('**/api/v1/scenarios**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.pathname !== '/api/v1/scenarios') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [question], total: 1 })
  })
  await page.route('**/api/v1/scenarios/e2e-markdown-scenario-question/sessions', async (route) => {
    await fulfill(route, {
      session_id: 'e2e-markdown-scenario-session',
      status: 'active',
      question_snapshot: question,
    })
  })
  await page.route('**/api/v1/scenarios/sessions/e2e-markdown-scenario-session', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session: {
        ...scenarioSession('e2e-markdown-scenario-session', 'active'),
        question_id: question.id,
        question_snapshot: question,
      },
      messages: [],
    })
  })
  await page.route('**/api/v1/scenarios/sessions/e2e-markdown-scenario-session/answer', async (route) => {
    submittedAnswer = JSON.parse(route.request().postData() || '{}').answer ?? ''
    await fulfill(route, {
      evaluation_id: 'e2e-markdown-evaluation',
      status: 'completed',
      result: {
        is_correct: true,
        match_degree: 92,
        missing_points: [],
        standard_procedure: ['定位慢查询', '验证索引', '灰度修复'],
      },
      score: {
        efficiency: 88,
        accuracy: 92,
        clue_usage: 90,
        total: 90,
      },
    })
  })
  await page.route('**/api/v1/scenarios/sessions/e2e-markdown-scenario-session/review', async (route) => {
    await fulfill(route, {
      session: {
        ...scenarioSession('e2e-markdown-scenario-session', 'completed'),
        question_id: question.id,
        question_snapshot: question,
        user_answer: submittedAnswer,
        evaluation_result: {
          is_correct: true,
          match_degree: 92,
          missing_points: [],
          standard_procedure: ['定位慢查询', '验证索引', '灰度修复'],
        },
        score: {
          efficiency: 88,
          accuracy: 92,
          clue_usage: 90,
          total: 90,
        },
      },
      messages: [],
      standard_answer: '缓存 key 规则变化导致命中率下降。',
      standard_steps: ['定位慢查询', '验证索引', '灰度修复'],
      key_evidence: ['慢查询峰值', '缓存命中率下降'],
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).first().click()
  await page.getByRole('button', { name: '展开最终答案区' }).click()

  const editor = page.getByTestId('scenario-answer-editor')
  await editor.fill('## 根因结论\n- 缓存 key 规则变化导致命中率下降\n\n```sql\nEXPLAIN SELECT * FROM orders;\n```')
  await page.getByRole('button', { name: /预览/ }).click()

  await expect(page.locator('.markdown-preview h2', { hasText: '根因结论' })).toBeVisible()
  await expect(page.locator('.markdown-preview .code-window-header', { hasText: 'SQL' })).toBeVisible()
  await expect(page.locator('.markdown-preview .token.keyword', { hasText: 'EXPLAIN' })).toBeVisible()

  await page.getByRole('button', { name: '全屏' }).click()
  await expect(page.locator('.markdown-composer.expanded')).toBeVisible()
  await page.getByRole('button', { name: '退出全屏' }).click()
  await expect(page.locator('.markdown-composer.expanded')).toHaveCount(0)

  await page.getByTestId('submit-scenario-answer').click()

  await expect(page).toHaveURL(/\/scenarios\/session\/e2e-markdown-scenario-session\/review$/)
  await expect(page.getByRole('heading', { name: '排查复盘' })).toBeVisible()
  await expect(page.locator('.scenario-review-page')).toBeVisible()
  await expect(page.locator('.scenario-review-page .metric')).toHaveCount(4)
  await expect(page.getByRole('link', { name: '返回排查工坊' })).toBeVisible()
  await expect(page.getByRole('button', { name: '打印/导出 PDF' })).toBeVisible()
  await page.getByRole('link', { name: '返回排查工坊' }).click()
  await expect(page).toHaveURL(/\/scenarios$/)
  expect(submittedAnswer).toContain('## 根因结论')
  expect(submittedAnswer).toContain('EXPLAIN SELECT')
})

test('student can fork a scenario from the workshop', async ({ page }) => {
  await page.route('**/api/v1/scenarios/*/fork', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: communityPost('e2e-forked-post', 'E2E 派生题目'),
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await expectNoWhiteScreen(page)
  const cardCount = await page.locator('.scenario-card').count()
  await page.getByRole('button', { name: '派生题目' }).first().click()

  await expect(page.locator('.inline-success')).toContainText('草稿')
  await expect(page.locator('.inline-success')).toContainText('提交初审')
  await expect(page.locator('.inline-success')).toContainText('初审队列')
  await expect(page.locator('.scenario-card')).toHaveCount(cardCount)
})

test('instructor can open own fork draft from the workshop notice', async ({ page }) => {
  const forkedPost = { ...communityPost('e2e-instructor-forked-post', 'E2E 讲师派生草稿'), user_id: 'user-instructor' }

  await page.route('**/api/v1/scenarios/*/fork', async (route) => {
    await fulfill(route, forkedPost)
  })

  await page.route('**/api/v1/community/posts**', async (route) => {
    const url = new URL(route.request().url())
    const status = url.searchParams.get('status') ?? ''
    await fulfill(route, {
      list: !status || status === forkedPost.status ? [forkedPost] : [],
    })
  })

  await loginAs(page, 'instructor')
  await page.goto('/scenarios')

  await expectNoWhiteScreen(page)
  await page.getByRole('button', { name: '派生题目' }).first().click()
  await expect(page.locator('.inline-success')).toContainText('我的草稿')
  await page.getByRole('button', { name: '去编辑草稿' }).click()

  await expect(page).toHaveURL(/\/community\?status=draft/)
  await expect(page.getByRole('button', { name: '我的草稿' })).toBeVisible()
  await expect(page.getByRole('heading', { name: forkedPost.title })).toBeVisible()
  await expect(page.getByRole('button', { name: /提交初审/ })).toBeVisible()
})

test('student can upload voice answer and use transcript draft', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: 'e2e-voice-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-voice-session'), status: 'active', submissions: [], evaluations: [], final_score: undefined, final_report: undefined },
    })
  })

  await page.route('**/api/v1/assets', async (route) => {
    await fulfill(route, {
      id: 'voice-asset-1',
      user_id: 'demo-user',
      kind: 'voice',
      filename: 'answer.webm',
      mime_type: 'audio/webm',
      size: 16,
      storage_key: 'voice-asset-1',
      url: '/api/v1/assets/voice-asset-1',
      content_url: '/api/v1/assets/voice-asset-1?content=1',
      checksum: 'mock-sha256',
      created_at: new Date().toISOString(),
    })
  })

  await page.route('**/api/v1/assets/voice-asset-1?content=1', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'audio/webm',
      body: Buffer.from('voice-bytes'),
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-voice-session/voice', async (route) => {
    await fulfill(route, {
      asset: { id: 'voice-asset-1', filename: 'answer.webm' },
      transcript: '语音转写草稿：先定位慢查询，再验证索引和回滚方案。',
      duration_seconds: 12,
      status: 'draft_ready',
      quality: voiceQuality('draft_ready'),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/submit', async (route) => {
    const body = route.request().postDataJSON() as { confirmed_transcript?: boolean }
    expect(body.confirmed_transcript).toBe(true)
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/submit/)?.[1] ?? 'e2e-session'
    await fulfill(route, {
      evaluation: interviewEvaluation(),
      session_status: 'final_evaluated',
      session: interviewSession(sessionId, 'voice'),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/report', async (route) => {
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/report/)?.[1] ?? 'e2e-session'
    await fulfill(route, {
      session: interviewSession(sessionId, 'voice'),
      question: interviewQuestion(),
      radar_data: [{ dimension: '准确性', score: 86 }],
      final_score: 86,
      final_report: '语音面试报告已生成。',
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.getByRole('button', { name: '开始面试' }).click()

  const file = Buffer.from('voice')
  await page.getByTestId('voice-file-input').setInputFiles({ name: 'answer.webm', mimeType: 'audio/webm', buffer: file })
  await expect(page.getByPlaceholder(/用结构化方式回答/)).toHaveValue(/语音转写草稿/)
  await expect(page.getByText('请确认转写文本')).toBeVisible()
  await expect(page.getByRole('button', { name: /提交语音回答/ })).toBeDisabled()
  await page.getByRole('button', { name: '确认转写文本' }).click()
  await page.getByRole('button', { name: /提交语音回答/ }).click()
  await expect(page.getByRole('heading', { name: '面试报告' })).toBeVisible()
  await expect(page.getByText('语音资源')).toBeVisible()
})

test('student can apply suggested technical term corrections before confirming transcript', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: 'e2e-voice-correction-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-voice-correction-session'), status: 'active', submissions: [], evaluations: [], final_score: undefined, final_report: undefined },
    })
  })

  await page.route('**/api/v1/assets', async (route) => {
    await fulfill(route, {
      id: 'voice-asset-2',
      user_id: 'demo-user',
      kind: 'voice',
      filename: 'answer.webm',
      mime_type: 'audio/webm',
      size: 16,
      storage_key: 'voice-asset-2',
      url: '/api/v1/assets/voice-asset-2',
      content_url: '/api/v1/assets/voice-asset-2?content=1',
      checksum: 'mock-sha256',
      created_at: new Date().toISOString(),
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-voice-correction-session/voice', async (route) => {
    await fulfill(route, {
      asset: { id: 'voice-asset-2', filename: 'answer.webm' },
      transcript: '鎴戜細鍏堢湅鎭╅噾鍏嬫柉璁块棶鏃ュ織锛屽啀鎺掓煡涔癝QL鎱㈡煡璇紝鏈€鍚庣敤 explain 楠岃瘉绱㈠紩銆?',
      duration_seconds: 15,
      status: 'needs_review',
      quality: voiceQualityWithSuggestions('needs_review', [
        { original: '鎭╅噾鍏嬫柉', suggested: 'nginx', reason: '妫€娴嬪埌甯歌 Web 鏈嶅姟鍣ㄦ湳璇皭闊?' },
        { original: '涔癝QL', suggested: 'MySQL', reason: '妫€娴嬪埌鏁版嵁搴撴湳璇皭闊虫垨鎷嗗啓' },
        { original: 'explain', suggested: 'EXPLAIN', reason: '妫€娴嬪埌甯歌鏁版嵁搴撳懡浠ゅぇ灏忓啓涓嶈鑼?' },
      ]),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/submit', async (route) => {
    const body = route.request().postDataJSON() as {
      confirmed_transcript?: boolean
      transcript?: string
      content?: string
      source?: string
    }
    expect(body.confirmed_transcript).toBe(true)
    expect(body.transcript).toContain('鎭╅噾鍏嬫柉')
    expect(body.content).toContain('nginx')
    expect(body.content).toContain('MySQL')
    expect(body.content).toContain('EXPLAIN')
    expect(body.source).toBe('voice_edited')
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/submit/)?.[1] ?? 'e2e-session'
    await fulfill(route, {
      evaluation: interviewEvaluation(),
      session_status: 'final_evaluated',
      session: interviewSession(sessionId, 'voice'),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/report', async (route) => {
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/report/)?.[1] ?? 'e2e-session'
    await fulfill(route, {
      session: interviewSession(sessionId, 'voice'),
      question: interviewQuestion(),
      radar_data: [{ dimension: '鍑嗙‘鎬?', score: 86 }],
      final_score: 86,
      final_report: '鏈绾犻敊鍚庣殑璇煶闈㈣瘯鎶ュ憡宸茬敓鎴愩€?',
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.locator('button.primary-button').first().click()

  const file = Buffer.from('voice')
  const editor = page.getByTestId('interview-answer-editor')
  await page.getByTestId('voice-file-input').setInputFiles({ name: 'answer.webm', mimeType: 'audio/webm', buffer: file })

  await expect(page.getByTestId('voice-transcript-suggestions')).toBeVisible()
  await expect(page.getByTestId('transcript-suggestion-item-0')).toContainText('nginx')
  await expect(page.getByTestId('transcript-suggestion-item-1')).toContainText('MySQL')
  await expect(page.getByTestId('transcript-suggestion-item-2')).toContainText('EXPLAIN')
  await expect(editor).toHaveValue(/鎭╅噾鍏嬫柉/)

  await page.getByTestId('confirm-voice-transcript').click()
  await page.getByTestId('apply-transcript-suggestion-0').click()
  await expect(editor).toHaveValue(/nginx/)
  await expect(page.getByTestId('submit-interview-answer')).toBeDisabled()

  await page.getByTestId('apply-all-transcript-suggestions').click()
  await expect(editor).toHaveValue(/MySQL/)
  await expect(editor).toHaveValue(/EXPLAIN/)
  await page.getByTestId('confirm-voice-transcript').click()
  await page.getByTestId('submit-interview-answer').click()
  await expect(page).toHaveURL(/\/interviews\/session\/[^/]+\/report$/)
  await expect(page.locator('.submission-evidence')).toBeVisible()
})

test('student sees an empty terminology panel when no suggestions are available', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: 'e2e-empty-term-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-empty-term-session'), status: 'active', submissions: [], evaluations: [], final_score: undefined, final_report: undefined },
    })
  })

  await page.route('**/api/v1/assets', async (route) => {
    await fulfill(route, {
      id: 'voice-asset-empty',
      user_id: 'demo-user',
      kind: 'voice',
      filename: 'answer.webm',
      mime_type: 'audio/webm',
      size: 16,
      storage_key: 'voice-asset-empty',
      url: '/api/v1/assets/voice-asset-empty',
      content_url: '/api/v1/assets/voice-asset-empty?content=1',
      checksum: 'mock-sha256',
      created_at: new Date().toISOString(),
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-empty-term-session/voice', async (route) => {
    await fulfill(route, {
      asset: { id: 'voice-asset-empty', filename: 'answer.webm' },
      transcript: '我会先定位慢查询日志，再验证索引和回滚方案。',
      duration_seconds: 12,
      status: 'draft_ready',
      quality: voiceQuality('draft_ready'),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.locator('button.primary-button').first().click()

  await page.getByTestId('voice-file-input').setInputFiles({ name: 'answer.webm', mimeType: 'audio/webm', buffer: Buffer.from('voice') })
  await expect(page.getByTestId('voice-transcript-suggestions')).toBeVisible()
  await expect(page.getByTestId('voice-transcript-suggestions-empty')).toBeVisible()
  await expect(page.getByTestId('voice-transcript-suggestions')).not.toContainText('应用')
})

test('student sees session-expired guidance instead of rejected voice quality when transcript session is missing', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: 'e2e-expired-voice-session',
      status: 'active',
      question: interviewQuestion(),
      session: { ...interviewSession('e2e-expired-voice-session'), status: 'active', submissions: [], evaluations: [], final_score: undefined, final_report: undefined },
    })
  })

  await page.route('**/api/v1/assets', async (route) => {
    await fulfill(route, {
      id: 'voice-asset-expired',
      user_id: 'demo-user',
      kind: 'voice',
      filename: 'answer.webm',
      mime_type: 'audio/webm',
      size: 16,
      storage_key: 'voice-asset-expired',
      url: '/api/v1/assets/voice-asset-expired',
      content_url: '/api/v1/assets/voice-asset-expired?content=1',
      checksum: 'mock-sha256',
      created_at: new Date().toISOString(),
    })
  })

  await page.route('**/api/v1/interviews/sessions/e2e-expired-voice-session/voice', async (route) => {
    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 404,
        message: 'interview session not found',
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')
  await page.locator('button.primary-button').first().click()

  const file = Buffer.from('voice')
  await page.getByTestId('voice-file-input').setInputFiles({ name: 'answer.webm', mimeType: 'audio/webm', buffer: file })

  await expect(page.locator('.voice-quality.rejected')).toHaveCount(0)
  await expect(page.getByTestId('interview-stream-feedback')).toContainText('面试会话已失效')
  await expect(page.getByTestId('interview-session-restart')).toBeVisible()
})

test('student can filter and page scenarios', async ({ page }) => {
  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await expectNoWhiteScreen(page)
  await expect(page.getByText(/共 \d+ 道题/)).toBeVisible()
  await page.getByLabel('难度').selectOption('L2')
  await page.getByLabel('标签').fill('缓存')

  await expectNoWhiteScreen(page)
  await expect(page.getByRole('button', { name: '上一页' })).toBeVisible()
  await expect(page.getByRole('button', { name: '下一页' })).toBeVisible()
  await expect(page.locator('.pagination-bar')).toContainText(/第 \d+ \/ \d+ 页/)

  await page.getByRole('button', { name: '重置筛选' }).click()
  await expect(page.getByLabel('难度')).toHaveValue('')
  await expect(page.getByLabel('标签')).toHaveValue('')
})

test('student can abandon a troubleshooting session', async ({ page }) => {
  await page.route('**/api/v1/scenarios/*/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          session_id: 'e2e-abandon-session',
          status: 'active',
          question_snapshot: scenarioQuestion('e2e-abandon-question', 'E2E 放弃会话题目'),
        },
      }),
    })
  })

  await page.route('**/api/v1/scenarios/sessions/e2e-abandon-session/quit', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          status: 'abandoned',
          session: scenarioSession('e2e-abandon-session', 'abandoned'),
        },
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')

  await page.getByRole('button', { name: '开始排查' }).first().click()
  await expect(page.getByText('渐进式排查会话')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('CPU /')
  await page.getByRole('button', { name: '放弃会话' }).click()

  await expectNoWhiteScreen(page)
  await expect(page).toHaveURL(/\/scenarios$/)
  await expect(page.getByRole('heading', { name: '排查工坊' })).toBeVisible()
})

test('invalid mermaid is shown as a compact fallback', async ({ page }) => {
  await page.route('**/api/v1/scenarios/*/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          session_id: 'e2e-invalid-mermaid-session',
          status: 'active',
          question_snapshot: {
            ...scenarioQuestion('e2e-invalid-mermaid-question', 'E2E 错误架构图题目'),
            content: {
              ...scenarioQuestion('e2e-invalid-mermaid-question', 'E2E 错误架构图题目').content,
              architecture_diagram: 'graph TD\n  A[旧规则 --> B[失效]',
            },
          },
        },
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/scenarios')
  await page.getByRole('button', { name: '开始排查' }).first().click()

  await expect(page.getByText(/Mermaid 暂不可渲染|架构图暂不可渲染/)).toBeVisible()
  await expect(page.locator('body')).not.toContainText('Syntax error in text')
  await expect(page.locator('body')).not.toContainText('mermaid version')
})

test('scenario list and session snapshot use public SC-04 DTO', async ({ page }) => {
  const sensitiveSnapshot = {
    ...scenarioQuestion('e2e-sc04-question', '腾讯公司案例 AI 模型key=sk-admin-visible'),
    description: "马哥教育真实案例 密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@",
    tags: ['马哥教育', 'api_key=sk-session-secret'],
    content: {
      ...scenarioQuestion('e2e-sc04-question', '腾讯公司案例 AI 模型key=sk-admin-visible').content,
      architecture_diagram: 'graph TD\nA[马哥教育] --> B[password=diagram-secret]',
    },
    status: 'active',
    is_sanitized: true,
  }

  await page.route('**/api/v1/scenarios**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfill(route, { list: [sensitiveSnapshot], total: 1 })
  })

  await page.route('**/api/v1/scenarios/*/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await fulfill(route, {
      session_id: 'e2e-sc04-public-session',
      status: 'active',
      question_snapshot: sensitiveSnapshot,
    })
  })

  await loginAs(page, 'admin')
  await page.goto('/scenarios')
  await expect(page.locator('.scenario-card')).toContainText('脱敏选题卡片')
  await expect(page.locator('main')).not.toContainText('腾讯公司')
  await expect(page.locator('main')).not.toContainText('马哥教育')
  await expect(page.locator('main')).not.toContainText('12345asdfasd')
  await expect(page.locator('main')).not.toContainText('123qq.com')
  await expect(page.locator('main')).not.toContainText("df'hww@@")
  await expect(page.locator('main')).not.toContainText('完整版')
  await expect(page.locator('body')).toContainText('【机构A】')
  await expect(page.locator('body')).toContainText('【密钥】')
  await page.getByRole('button', { name: '开始排查' }).first().click()

  await expect(page.getByText('题目快照')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('腾讯公司')
  await expect(page.locator('body')).not.toContainText('马哥教育')
  await expect(page.locator('body')).not.toContainText('12345asdfasd')
  await expect(page.locator('body')).not.toContainText('123qq.com')
  await expect(page.locator('body')).not.toContainText("df'hww@@")
  await expect(page.locator('body')).toContainText('【机构A】')
  await expect(page.locator('body')).toContainText('【密钥】')
})

test('student can reach an interview report without real LLM feedback', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          session_id: 'e2e-interview-session',
          status: 'active',
          question: interviewQuestion(),
          session: {
            ...interviewSession('e2e-interview-session'),
            status: 'active',
            submissions: [],
            evaluations: [],
            final_score: undefined,
            final_report: undefined,
          },
        },
      }),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/submit', async (route) => {
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/submit/)?.[1] ?? 'e2e-session'
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          evaluation: interviewEvaluation(),
          session_status: 'final_evaluated',
          session: interviewSession(sessionId),
        },
      }),
    })
  })

  await page.route('**/api/v1/interviews/sessions/*/report', async (route) => {
    const sessionId = route.request().url().match(/sessions\/([^/]+)\/report/)?.[1] ?? 'e2e-session'
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          session: interviewSession(sessionId),
          question: interviewQuestion(),
          radar_data: [
            { dimension: 'technical_accuracy', score: 88 },
            { dimension: 'logical_completeness', score: 82 },
            { dimension: 'solution_feasibility', score: 86 },
          ],
          final_score: 86,
          final_report: 'E2E 综合评语已生成。',
        },
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews')

  await expectNoWhiteScreen(page)
  await expect(page.getByRole('heading', { name: '技术面试舱' })).toBeVisible()
  await page.getByRole('button', { name: '开始面试' }).click()
  await expect(page.locator('.answer-panel')).toContainText('回答')
  await page.getByPlaceholder(/用结构化方式回答/).fill('首先定位慢查询日志，然后对比执行计划和索引命中，最后灰度回滚并验证核心指标。')
  await page.getByRole('button', { name: /提交回答/ }).click()

  await expect(page.getByRole('heading', { name: '面试报告' })).toBeVisible()
  await expect(page.getByText('最终得分')).toBeVisible()
  await expect(page.locator('.interview-report-page .metric').filter({ hasText: '状态' })).toContainText('已完成')
  await expect(page.locator('.interview-report-page .metric').filter({ hasText: '状态' })).not.toContainText('final_evaluated')
  await expect(page.locator('.panel-title').filter({ hasText: '综合评语' })).toBeVisible()
  const reportPage = page.locator('.interview-report-page')
  await expect(reportPage).toBeVisible()
  await expect(reportPage).toHaveCSS('color', 'rgb(230, 237, 245)')
  await expect(page.locator('.interview-report-page .metric').first()).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(page.locator('.interview-report-page .report-overview')).toHaveCSS('background-color', 'rgb(13, 19, 29)')
  await expect(page.getByTestId('report-agent-summary').first()).toHaveCSS('background-color', 'rgba(255, 255, 255, 0.04)')
  await expect(page.getByTestId('report-agent-summary')).toHaveCount(1)
  await expect(page.locator('.interview-report-page .chart-box')).toContainText('技术准确性')
  await expect(page.locator('.interview-report-page .chart-box')).toContainText('逻辑完整性')
  await expect(page.locator('.interview-report-page .chart-box')).not.toContainText('technical_accuracy')
})

test('invalidated interview report hides score chart and only asks to keep improving', async ({ page }) => {
  await page.route('**/api/v1/interviews/sessions/e2e-invalidated-interview/report', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        code: 200,
        message: 'success',
        data: {
          session: {
            ...interviewSession('e2e-invalidated-interview'),
            status: 'final_evaluated',
            final_score: 0,
            final_report: '继续沉淀',
            submissions: [
              { round: 1, content: '我想聊电影音乐和周末旅行安排，这些内容和当前面试题无关。', type: 'text', source: 'text', quality_flag: 'irrelevant', submitted_at: new Date().toISOString() },
            ],
            evaluations: [
              {
                ...interviewEvaluation(),
                total_score: 0,
                dimension_scores: {},
                highlights: [],
                deficiencies: ['面试官认为你还没有准备好，请先继续沉淀，再重新开始本场面试。'],
                follow_up_triggered: false,
              },
            ],
          },
          question: interviewQuestion(),
          radar_data: [],
          final_score: 0,
          final_report: '继续沉淀',
        },
      }),
    })
  })

  await loginAs(page, 'student')
  await page.goto('/interviews/session/e2e-invalidated-interview/report')

  await expect(page.getByRole('heading', { name: '面试报告' })).toBeVisible()
  await expect(page.locator('.panel-title').filter({ hasText: '五维评分' })).toHaveCount(0)
  await expect(page.locator('.interview-report-page .chart-box')).toHaveCount(0)
  await expect(page.locator('.report-invalidated-panel')).toContainText('继续沉淀')
  await expect(page.locator('.report-invalidated-panel')).toContainText('没有生成详细数据评分')
  await expect(page.locator('.panel-title').filter({ hasText: '综合评语' })).toBeVisible()
  await expect(page.locator('.report-text')).toHaveText('继续沉淀')
  const turnCompare = page.locator('.panel', { hasText: '轮次对比' })
  await expect(turnCompare).toContainText('无效作答')
  await expect(turnCompare).toContainText('未进行详细评分')
  await expect(turnCompare).not.toContainText('面试官认为你还没有准备好')
  await expect(turnCompare).not.toContainText('第 1 轮：0 分')
})

function interviewQuestion() {
  return {
    id: 'e2e-question',
    title: 'E2E 数据库慢查询定位',
    description: '说明如何定位数据库慢查询并恢复服务。',
    domain: 'database',
    difficulty: 'L3',
    question_type: 'scenario_analysis',
    evaluation_dimensions: [],
    follow_up_strategies: [],
  }
}

function scenarioQuestion(id: string, title: string) {
  return {
    id,
    title,
    description: '通过已有题目派生出的待审核训练案例。',
    domain: 'database',
    difficulty: 'L2',
    scenario_type: 'troubleshooting',
    tags: ['派生', 'E2E'],
    content: {
      reveal_strategy: {
        surface_clues: [],
        deep_clues: [],
        distractors: [],
      },
      architecture_diagram: '',
      reference_links: [],
    },
    status: 'pending',
    source: 'ugc_structured',
    created_by: 'demo-user',
    version: 2,
    is_sanitized: true,
  }
}

function communityPost(id: string, title: string) {
  return {
    id,
    user_id: 'demo-user',
    title,
    raw_content: '基于原题派生出的待审核案例。',
    domain: 'database',
    tags: ['派生', 'E2E'],
    ai_structured_content: scenarioQuestion('fork-source', title).content,
    review_history: [],
    status: 'draft',
    forked_from_scenario_id: 'fork-source',
    created_at: new Date().toISOString(),
  }
}

function scenarioSession(id: string, status: string) {
  return {
    id,
    user_id: 'demo-user',
    question_id: 'e2e-abandon-question',
    status,
    current_turn: 0,
    max_turns: 50,
    revealed_clue_ids: [],
    question_snapshot: scenarioQuestion('e2e-abandon-question', 'E2E 放弃会话题目'),
    hint_level: 1,
    no_new_clue_streak: 0,
    started_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    ended_at: new Date().toISOString(),
  }
}

function interviewSession(sessionId: string, type: 'text' | 'voice' = 'text') {
  return {
    id: sessionId,
    user_id: 'demo-user',
    question_id: 'e2e-question',
    status: 'final_evaluated',
    current_round: 1,
    max_rounds: 2,
    submissions: [
      {
        round: 1,
        content: type === 'voice' ? '语音转写草稿：先定位慢查询，再验证索引和回滚方案。' : '首先定位慢查询日志，然后对比执行计划和索引命中，最后灰度回滚并验证核心指标。',
        type,
        source: type === 'voice' ? 'voice_transcript' : 'text',
        asset_id: type === 'voice' ? 'voice-asset-1' : undefined,
        asset: type === 'voice' ? {
          id: 'voice-asset-1',
          user_id: 'demo-user',
          kind: 'voice',
          filename: 'answer.webm',
          mime_type: 'audio/webm',
          size: 16,
          storage_key: 'voice/demo-user/voice-asset-1.webm',
          url: '/api/v1/assets/voice-asset-1',
          content_url: '/api/v1/assets/voice-asset-1?content=1',
          checksum: 'mock-sha256',
          created_at: new Date().toISOString(),
        } : undefined,
        duration_seconds: type === 'voice' ? 12 : undefined,
        voice_quality: type === 'voice' ? voiceQuality('draft_ready') : undefined,
        submitted_at: new Date().toISOString(),
      },
    ],
    evaluations: [interviewEvaluation()],
    final_score: 86,
    final_report: 'E2E 综合评语已生成。',
  }
}

function voiceQuality(status: 'draft_ready' | 'needs_review' | 'rejected') {
  return {
    detected_language: status === 'rejected' ? 'en' : 'zh',
    stt_confidence: status === 'rejected' ? 0.7 : 0.92,
    topic_relevance_score: status === 'rejected' ? 8 : 82,
    keyword_hits: status === 'rejected' ? [] : ['MySQL', 'EXPLAIN', '索引'],
    reasons: status === 'rejected' ? ['转写内容与本题相关性不足'] : [],
    status,
  }
}

function aiJob(
  id: string,
  status: 'queued' | 'running' | 'completed' | 'failed',
  progress: number,
  overrides: Record<string, unknown> = {},
) {
  const now = new Date().toISOString()
  return {
    id,
    user_id: 'demo-user',
    kind: 'scenario_generation',
    status,
    stage: status,
    progress,
    provider: 'mock',
    validated: status === 'completed',
    fallback_used: false,
    created_at: now,
    started_at: now,
    completed_at: status === 'completed' || status === 'failed' ? now : undefined,
    updated_at: now,
    ...overrides,
  }
}

function systemStatus() {
  const now = new Date().toISOString()
  return {
    generated_at: now,
    services: [
      { name: 'API', status: 'ok', detail: 'ok' },
      { name: 'AI Provider', status: 'fallback', detail: 'mock provider active' },
      { name: 'Seed Data', status: 'ok', detail: 'seed ready' },
    ],
    ai: { provider: 'mock', model: 'mock', fallback: true },
    counts: {
      users: 3,
      scenarios: 1,
      active_scenarios: 1,
      community_posts: 0,
      pending_ugc: 0,
    },
    demo_accounts: [
      { role: 'admin', username: 'admin', purpose: '系统状态验证' },
    ],
    runbook: [
      { title: '启动前端', command: 'npm run dev' },
    ],
  }
}

function voiceQualityWithSuggestions(
  status: 'draft_ready' | 'needs_review' | 'rejected',
  transcriptSuggestions: Array<{ original: string; suggested: string; reason: string }>,
) {
  return {
    ...voiceQuality(status),
    transcript_suggestions: transcriptSuggestions,
  }
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 200, message: 'success', data }),
  })
}

function interviewEvaluation() {
  return {
    round: 1,
    total_score: 86,
    dimension_scores: {
      technical_accuracy: 88,
      logical_completeness: 82,
      solution_feasibility: 86,
    },
    is_passed: true,
    highlights: ['回答覆盖了定位、处理与验证。'],
    deficiencies: ['可继续补充量化指标。'],
    follow_up_triggered: false,
    agent_trace: agentTrace(),
    created_at: new Date().toISOString(),
  }
}

function agentTrace() {
  const now = new Date().toISOString()
  return {
    run_id: 'run-e2e-report',
    agent: 'interview_agent',
    mode: 'safe_summary',
    tool_count: 5,
    started_at: now,
    finished_at: now,
    steps: [
      { name: 'score', kind: 'analysis', status: 'success', summary: '确认作答文本可进入面试评分', started_at: now, ended_at: now },
      { name: 'dimensions', kind: 'analysis', status: 'success', summary: '完成五维评分与通过判断', started_at: now, ended_at: now },
      { name: 'safety', kind: 'safety', status: 'success', summary: '反馈通过安全检查', started_at: now, ended_at: now },
    ],
  }
}
