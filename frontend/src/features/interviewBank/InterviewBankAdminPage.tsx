import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  Archive,
  CheckCircle2,
  Database,
  Eye,
  FileJson,
  History,
  ListFilter,
  PackageCheck,
  RefreshCw,
  RotateCcw,
  Save,
  Search,
  Upload,
} from 'lucide-react'
import { api } from '../../api/client'
import { Loading, Metric } from '../../components/common'
import { useToken } from '../../lib/auth'
import type {
  InterviewKnowledgeAtom,
  InterviewKnowledgeAtomFilters,
  InterviewKnowledgeAtomUpdateRequest,
  InterviewKnowledgeAtomVersion,
  InterviewKnowledgeBatch,
  InterviewKnowledgeHealthCombination,
  InterviewKnowledgeHealthResponse,
  InterviewKnowledgeIndexRebuildRequest,
  InterviewKnowledgeIndexRebuildResponse,
  InterviewKnowledgeImportReport,
  InterviewKnowledgePublishResponse,
  InterviewKnowledgeRetrievalPreviewResponse,
  InterviewKnowledgeSummary,
  InterviewRetrievalAnalyticsResponse,
  InterviewRetrievalAtomHit,
  InterviewRetrievalFallbackCombination,
  InterviewRetrievalLog,
} from '../../types'
import './InterviewBankAdminPage.css'

const categoryOptions = [
  { value: '', label: '全部分类' },
  { value: 'java', label: 'Java' },
  { value: 'database', label: '数据库' },
  { value: 'cache', label: '缓存' },
  { value: 'middleware', label: '中间件' },
  { value: 'system_design', label: '系统设计' },
  { value: 'frontend', label: '前端' },
  { value: 'ai_llm', label: 'AI/LLM' },
  { value: 'hr_soft_skill', label: 'HR 软技能' },
]

const difficultyOptions = ['', 'L1', 'L2', 'L3', 'L4', 'L5']
const statusOptions = ['', 'published', 'draft', 'archived']
const questionRoleOptions = ['', 'opening', 'followup', 'mixed']
const vectorStatusOptions = ['', 'pending', 'indexed', 'failed']

const emptyFilters: InterviewKnowledgeAtomFilters = {
  status: '',
  domain: '',
  difficulty: '',
  category: '',
  question_role: '',
  vector_status: '',
  page_size: 20,
}

interface AtomEditForm {
  base_version: number
  change_note: string
  title: string
  subject: string
  domain: string
  difficulty: string
  category: string
  question_role: string
  source_ref: string
  tagsText: string
  principlesText: string
  pitfallsText: string
  followUpPathsText: string
}

interface RetrievalPreviewForm {
  domain: string
  category: string
  difficulty: string
  query: string
  limit: number
}

export function InterviewBankAdminPage() {
  const token = useToken()
  const [summary, setSummary] = useState<InterviewKnowledgeSummary | null>(null)
  const [health, setHealth] = useState<InterviewKnowledgeHealthResponse | null>(null)
  const [atoms, setAtoms] = useState<InterviewKnowledgeAtom[]>([])
  const [totalAtoms, setTotalAtoms] = useState(0)
  const [batches, setBatches] = useState<InterviewKnowledgeBatch[]>([])
  const [filters, setFilters] = useState<InterviewKnowledgeAtomFilters>(emptyFilters)
  const [isLoading, setIsLoading] = useState(true)
  const [isValidating, setIsValidating] = useState(false)
  const [isPublishing, setIsPublishing] = useState(false)
  const [isRebuilding, setIsRebuilding] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [importText, setImportText] = useState('')
  const [report, setReport] = useState<InterviewKnowledgeImportReport | null>(null)
  const [publishResult, setPublishResult] = useState<InterviewKnowledgePublishResponse | null>(null)
  const [rebuildResult, setRebuildResult] = useState<InterviewKnowledgeIndexRebuildResponse | null>(null)
  const [selectedAtomIds, setSelectedAtomIds] = useState<Set<string>>(() => new Set())
  const [activeAtom, setActiveAtom] = useState<InterviewKnowledgeAtom | null>(null)
  const [activeVersions, setActiveVersions] = useState<InterviewKnowledgeAtomVersion[]>([])
  const [editForm, setEditForm] = useState<AtomEditForm | null>(null)
  const [archiveReason, setArchiveReason] = useState('')
  const [isDetailLoading, setIsDetailLoading] = useState(false)
  const [isSavingAtom, setIsSavingAtom] = useState(false)
  const [isArchivingAtom, setIsArchivingAtom] = useState(false)
  const [isRestoringAtom, setIsRestoringAtom] = useState(false)
  const [isPreviewingRetrieval, setIsPreviewingRetrieval] = useState(false)
  const [retrievalPreviewForm, setRetrievalPreviewForm] = useState<RetrievalPreviewForm>({
    domain: 'backend',
    category: 'cache',
    difficulty: 'L3',
    query: '',
    limit: 5,
  })
  const [retrievalPreview, setRetrievalPreview] = useState<InterviewKnowledgeRetrievalPreviewResponse | null>(null)
  const [retrievalAnalytics, setRetrievalAnalytics] = useState<InterviewRetrievalAnalyticsResponse | null>(null)
  const [retrievalLogs, setRetrievalLogs] = useState<InterviewRetrievalLog[]>([])

  const loadData = useCallback(async () => {
    setIsLoading(true)
    setError('')
    try {
      const [nextSummary, nextHealth, atomData, batchData, nextRetrievalAnalytics, nextRetrievalLogs] = await Promise.all([
        api.adminInterviewBankSummary(token),
        api.adminInterviewBankHealth(token),
        api.adminInterviewBankAtoms(token, filters),
        api.adminInterviewBankBatches(token, 20),
        api.adminInterviewBankRetrievalAnalytics(token, { limit: 500 }),
        api.adminInterviewBankRetrievalLogs(token, { limit: 20 }),
      ])
      setSummary(nextSummary)
      setHealth(nextHealth)
      setAtoms(atomData.list ?? [])
      setTotalAtoms(atomData.total ?? 0)
      setBatches(batchData.list ?? [])
      setRetrievalAnalytics(nextRetrievalAnalytics)
      setRetrievalLogs(nextRetrievalLogs.list ?? [])
      setSelectedAtomIds((current) => {
        const visibleIDs = new Set((atomData.list ?? []).map((atom) => atom.id))
        return new Set([...current].filter((id) => visibleIDs.has(id)))
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : '题库治理数据读取失败')
    } finally {
      setIsLoading(false)
    }
  }, [filters, token])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadData()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [loadData])

  const reportSummary = report?.summary
  const canPublish = Boolean(report && (reportSummary?.valid_count ?? 0) > 0 && (reportSummary?.error_count ?? 0) === 0 && !isPublishing)
  const selectableAtoms = atoms.filter((atom) => canRebuildAtom(atom))
  const selectedCount = selectedAtomIds.size
  const allVisibleSelected = selectableAtoms.length > 0 && selectableAtoms.every((atom) => selectedAtomIds.has(atom.id))
  const heroMetrics = useMemo(() => [
    { label: '题库资源', value: summary?.total_atoms ?? 0 },
    { label: '已发布', value: summary?.published_atoms ?? 0 },
    { label: '失败索引', value: summary?.vector_failed_atoms ?? 0 },
    { label: '开放组合', value: summary?.open_combination_count ?? 0 },
  ], [summary])
  const healthHighlights = useMemo(
    () => (health?.combinations ?? []).filter((item) => item.status !== 'open').slice(0, 6),
    [health],
  )

  function updateFilter(key: keyof InterviewKnowledgeAtomFilters, value: string) {
    setFilters((current) => ({ ...current, [key]: value }))
  }

  function applyHealthCombination(combo: InterviewKnowledgeHealthCombination) {
    setFilters({
      status: '',
      domain: combo.domain,
      category: combo.category,
      difficulty: combo.difficulty,
      question_role: '',
      vector_status: '',
      page_size: 20,
    })
    setRetrievalPreviewForm((current) => ({
      ...current,
      domain: combo.domain,
      category: combo.category,
      difficulty: combo.difficulty,
    }))
  }

  function applyRetrievalCombination(combo: InterviewRetrievalFallbackCombination) {
    setFilters({
      status: '',
      domain: combo.domain,
      category: combo.category,
      difficulty: combo.difficulty,
      question_role: '',
      vector_status: '',
      page_size: 20,
    })
    setRetrievalPreviewForm((current) => ({
      ...current,
      domain: combo.domain,
      category: combo.category,
      difficulty: combo.difficulty,
    }))
    setMessage('已套用回退组合到题库筛选和检索预览')
  }

  function updateRetrievalPreviewForm<K extends keyof RetrievalPreviewForm>(key: K, value: RetrievalPreviewForm[K]) {
    setRetrievalPreviewForm((current) => ({ ...current, [key]: value }))
  }

  function parseImportPayload() {
    const text = importText.trim()
    if (!text) {
      throw new Error('导入 JSON 不能为空')
    }
    try {
      return JSON.parse(text) as unknown
    } catch {
      throw new Error('导入 JSON 格式不合法')
    }
  }

  async function handleValidate() {
    setError('')
    setMessage('')
    setPublishResult(null)
    setRebuildResult(null)
    try {
      const payload = parseImportPayload()
      setIsValidating(true)
      const nextReport = await api.validateInterviewBankImport(token, payload)
      setReport(nextReport)
      setMessage((nextReport.summary.error_count ?? 0) > 0 ? '校验完成，存在错误' : '校验通过')
    } catch (err) {
      setError(err instanceof Error ? err.message : '校验失败')
    } finally {
      setIsValidating(false)
    }
  }

  async function handlePublish() {
    setError('')
    setMessage('')
    try {
      if (!canPublish) {
        throw new Error('请先通过校验')
      }
      const payload = parseImportPayload()
      setIsPublishing(true)
      const result = await api.publishInterviewBankImport(token, payload)
      setPublishResult(result)
      setReport(result.report)
      setMessage(`发布完成：${result.report.summary.published_count ?? 0} 条`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : '发布失败')
    } finally {
      setIsPublishing(false)
    }
  }

  async function handleFileSelect(file?: File) {
    if (!file) return
    setError('')
    setMessage('')
    setReport(null)
    setPublishResult(null)
    setRebuildResult(null)
    try {
      setImportText(await file.text())
    } catch {
      setError('文件读取失败')
    }
  }

  function toggleAtomSelection(atom: InterviewKnowledgeAtom, checked: boolean) {
    if (!canRebuildAtom(atom)) return
    setSelectedAtomIds((current) => {
      const next = new Set(current)
      if (checked) {
        next.add(atom.id)
      } else {
        next.delete(atom.id)
      }
      return next
    })
  }

  function toggleAllVisibleSelections(checked: boolean) {
    setSelectedAtomIds((current) => {
      const next = new Set(current)
      for (const atom of selectableAtoms) {
        if (checked) {
          next.add(atom.id)
        } else {
          next.delete(atom.id)
        }
      }
      return next
    })
  }

  async function handleRebuild(scope: 'pending_failed' | 'selected') {
    setError('')
    setMessage('')
    setRebuildResult(null)
    try {
      const atomIDs = [...selectedAtomIds]
      if (scope === 'selected' && atomIDs.length === 0) {
        throw new Error('请选择要重建索引的题库资源')
      }
      const payload: InterviewKnowledgeIndexRebuildRequest = scope === 'selected'
        ? { atom_ids: atomIDs, limit: 50 }
        : { vector_status: 'pending_failed', limit: 50 }
      setIsRebuilding(true)
      const result = await api.rebuildInterviewBankIndex(token, payload)
      setRebuildResult(result)
      setMessage(`索引重建完成：成功 ${result.indexed} 条，失败 ${result.failed} 条，跳过 ${result.skipped} 条`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : '索引重建失败')
    } finally {
      setIsRebuilding(false)
    }
  }

  async function handleRetrievalPreview() {
    setError('')
    setMessage('')
    setRetrievalPreview(null)
    const query = retrievalPreviewForm.query.trim()
    if (!retrievalPreviewForm.category || !retrievalPreviewForm.difficulty || !query) {
      setError('请选择分类、难度并输入模拟文本')
      return
    }
    try {
      setIsPreviewingRetrieval(true)
      const result = await api.previewInterviewBankRetrieval(token, {
        domain: retrievalPreviewForm.domain.trim(),
        category: retrievalPreviewForm.category,
        difficulty: retrievalPreviewForm.difficulty,
        query,
        limit: retrievalPreviewForm.limit,
      })
      setRetrievalPreview(result)
      setMessage(result.fallback_used ? `检索预览回退：${result.fallback_reason}` : `检索预览命中 ${result.matched_count} 个题库原子`)
    } catch (err) {
      setError(err instanceof Error ? err.message : '检索预览失败')
    } finally {
      setIsPreviewingRetrieval(false)
    }
  }

  async function handleOpenAtom(atomID: string) {
    setError('')
    setMessage('')
    setIsDetailLoading(true)
    try {
      const [detail, history] = await Promise.all([
        api.adminInterviewBankAtom(token, atomID),
        api.adminInterviewBankAtomVersions(token, atomID),
      ])
      setActiveAtom(detail.atom)
      setActiveVersions(history.list ?? [])
      setEditForm(atomToEditForm(detail.atom))
      setArchiveReason('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '题目详情读取失败')
    } finally {
      setIsDetailLoading(false)
    }
  }

  function updateEditForm<K extends keyof AtomEditForm>(key: K, value: AtomEditForm[K]) {
    setEditForm((current) => current ? { ...current, [key]: value } : current)
  }

  async function handleSaveAtom() {
    if (!activeAtom || !editForm) return
    setError('')
    setMessage('')
    const confirmed = window.confirm('保存后将立即影响后续新面试，历史会话不受影响')
    if (!confirmed) return
    try {
      setIsSavingAtom(true)
      const payload = editFormToPayload(editForm)
      const result = await api.updateInterviewBankAtom(token, activeAtom.id, payload)
      const history = await api.adminInterviewBankAtomVersions(token, activeAtom.id)
      setActiveAtom(result.atom)
      setActiveVersions(history.list ?? [])
      setEditForm(atomToEditForm(result.atom))
      setMessage(`保存完成：${result.atom.id} v${result.atom.current_version}`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : '题目保存失败')
    } finally {
      setIsSavingAtom(false)
    }
  }

  async function handleArchiveAtom() {
    if (!activeAtom) return
    setError('')
    setMessage('')
    const reason = archiveReason.trim()
    if (!reason) {
      setError('请填写归档原因')
      return
    }
    const confirmed = window.confirm('归档后该题将不再进入后续新面试和追问检索')
    if (!confirmed) return
    try {
      setIsArchivingAtom(true)
      const result = await api.archiveInterviewBankAtom(token, activeAtom.id, { reason })
      const history = await api.adminInterviewBankAtomVersions(token, activeAtom.id)
      setActiveAtom(result.atom)
      setActiveVersions(history.list ?? [])
      setEditForm(atomToEditForm(result.atom))
      setArchiveReason('')
      setSelectedAtomIds((current) => {
        const next = new Set(current)
        next.delete(result.atom.id)
        return next
      })
      setMessage(`归档完成：${result.atom.id} v${result.atom.current_version}`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : '题目归档失败')
    } finally {
      setIsArchivingAtom(false)
    }
  }

  async function handleRestoreAtom() {
    if (!activeAtom) return
    setError('')
    setMessage('')
    const confirmed = window.confirm('恢复后将进入后续新面试；追问增强需等待管理员重建索引')
    if (!confirmed) return
    try {
      setIsRestoringAtom(true)
      const result = await api.restoreInterviewBankAtom(token, activeAtom.id)
      const history = await api.adminInterviewBankAtomVersions(token, activeAtom.id)
      setActiveAtom(result.atom)
      setActiveVersions(history.list ?? [])
      setEditForm(atomToEditForm(result.atom))
      setMessage(`恢复完成：${result.atom.id} v${result.atom.current_version}`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : '题目恢复失败')
    } finally {
      setIsRestoringAtom(false)
    }
  }

  if (isLoading && !summary) {
    return <Loading title="读取面试题库" />
  }

  return (
    <section className="page-stack interview-bank-page">
      <section className="panel interview-bank-hero">
        <div className="interview-bank-hero-copy">
          <span className="interview-bank-kicker">INTERVIEW BANK</span>
          <div className="interview-bank-title-row">
            <span className="interview-bank-title-icon"><Database size={24} /></span>
            <div>
              <h1>面试题库</h1>
              <div className="interview-bank-hero-tags">
                <span>{formatOptionalDate(summary?.last_imported_at)}</span>
                <span>{formatOptionalDate(summary?.last_edited_at)}</span>
                <span>{summary?.batch_count ?? 0} 批次</span>
              </div>
            </div>
          </div>
        </div>
        <div className="interview-bank-hero-metrics">
          {heroMetrics.map((item) => <Metric key={item.label} label={item.label} value={item.value} />)}
        </div>
      </section>

      {error && <span className="inline-error">{error}</span>}
      {message && <span className="success-line">{message}</span>}

      <section className="panel interview-bank-toolbar">
        <div className="panel-title"><ListFilter size={18} /> 筛选</div>
        <div className="interview-bank-filter-grid">
          <label>
            状态
            <select value={filters.status ?? ''} onChange={(event) => updateFilter('status', event.target.value)}>
              {statusOptions.map((value) => <option key={value || 'all'} value={value}>{statusLabel(value)}</option>)}
            </select>
          </label>
          <label>
            领域
            <input value={filters.domain ?? ''} onChange={(event) => updateFilter('domain', event.target.value)} placeholder="backend" />
          </label>
          <label>
            难度
            <select value={filters.difficulty ?? ''} onChange={(event) => updateFilter('difficulty', event.target.value)}>
              {difficultyOptions.map((value) => <option key={value || 'all'} value={value}>{value || '全部难度'}</option>)}
            </select>
          </label>
          <label>
            分类
            <select value={filters.category ?? ''} onChange={(event) => updateFilter('category', event.target.value)}>
              {categoryOptions.map((option) => <option key={option.value || 'all'} value={option.value}>{option.label}</option>)}
            </select>
          </label>
          <label>
            角色
            <select value={filters.question_role ?? ''} onChange={(event) => updateFilter('question_role', event.target.value)}>
              {questionRoleOptions.map((value) => <option key={value || 'all'} value={value}>{questionRoleLabel(value)}</option>)}
            </select>
          </label>
          <label>
            索引
            <select value={filters.vector_status ?? ''} onChange={(event) => updateFilter('vector_status', event.target.value)}>
              {vectorStatusOptions.map((value) => <option key={value || 'all'} value={value}>{vectorStatusLabel(value)}</option>)}
            </select>
          </label>
          <button className="ghost-button compact" type="button" onClick={() => void loadData()} disabled={isLoading}>
            <RefreshCw size={16} />刷新
          </button>
          <button className="ghost-button compact" type="button" onClick={() => setFilters(emptyFilters)}>
            <Search size={16} />重置
          </button>
          <button className="primary-button compact" type="button" onClick={() => void handleRebuild('pending_failed')} disabled={isRebuilding}>
            <RefreshCw size={16} />{isRebuilding ? '重建中' : '重建待处理'}
          </button>
        </div>
      </section>

      <div className="interview-bank-ops-grid">
        <HealthDiagnosticPanel
          health={health}
          highlights={healthHighlights}
          onApplyCombination={applyHealthCombination}
          onRebuild={() => void handleRebuild('pending_failed')}
          isRebuilding={isRebuilding}
        />
        <RetrievalPreviewPanel
          form={retrievalPreviewForm}
          result={retrievalPreview}
          isPreviewing={isPreviewingRetrieval}
          onChange={updateRetrievalPreviewForm}
          onSubmit={() => void handleRetrievalPreview()}
          onOpenAtom={(atomID) => void handleOpenAtom(atomID)}
        />
        <RetrievalOperationsPanel
          analytics={retrievalAnalytics}
          logs={retrievalLogs}
          onOpenAtom={(atomID) => void handleOpenAtom(atomID)}
          onApplyCombination={applyRetrievalCombination}
        />
      </div>

      <div className="interview-bank-main-grid">
        <section className="panel interview-bank-import-panel">
          <div className="panel-title"><FileJson size={18} /> 导入包</div>
          <textarea
            value={importText}
            onChange={(event) => {
              setImportText(event.target.value)
              setReport(null)
              setPublishResult(null)
            }}
            placeholder='{"source_ref":"manual","items":[]}'
          />
          <div className="interview-bank-actions">
            <label className="ghost-button compact interview-bank-file-button">
              <Upload size={16} />上传 JSON
              <input
                type="file"
                accept="application/json,.json"
                onChange={(event) => {
                  void handleFileSelect(event.target.files?.[0])
                  event.currentTarget.value = ''
                }}
              />
            </label>
            <button className="ghost-button compact" type="button" onClick={() => void handleValidate()} disabled={isValidating || isPublishing}>
              <CheckCircle2 size={16} />{isValidating ? '校验中' : '校验'}
            </button>
            <button className="primary-button compact" type="button" onClick={() => void handlePublish()} disabled={!canPublish}>
              <PackageCheck size={16} />{isPublishing ? '发布中' : '发布'}
            </button>
          </div>
          {report && <ImportReportPanel report={report} />}
          {rebuildResult && <IndexRebuildPanel result={rebuildResult} />}
          {publishResult && (
            <div className="interview-bank-publish-strip">
              <strong>{publishResult.batch.id}</strong>
              <span>{publishResult.batch.status} · {publishResult.batch.atom_count} 条 · {formatOptionalDate(publishResult.batch.updated_at)}</span>
            </div>
          )}
        </section>

        <section className="panel interview-bank-batch-panel">
          <div className="panel-title"><PackageCheck size={18} /> 最近批次</div>
          <div className="interview-bank-batch-list">
            {batches.length > 0 ? batches.map((batch) => (
              <div className="interview-bank-batch-row" key={batch.id}>
                <strong>{batch.id}</strong>
                <span>{batch.status} · {batch.mode} · {batch.atom_count} 条</span>
                <small>{batch.source_ref || 'manual'} · {formatOptionalDate(batch.created_at)}</small>
              </div>
            )) : (
              <div className="interview-bank-empty-row">
                <strong>暂无批次</strong>
                <span>发布导入包后会生成批次记录。</span>
              </div>
            )}
          </div>
        </section>
      </div>

      <section className="panel interview-bank-list-panel">
        <div className="interview-bank-list-title">
          <div className="panel-title"><Database size={18} /> 题库资源</div>
          <div className="interview-bank-list-actions">
            <span>{atoms.length} / {totalAtoms}</span>
            <button className="ghost-button compact" type="button" onClick={() => void handleRebuild('selected')} disabled={isRebuilding || selectedCount === 0}>
              <RefreshCw size={16} />重建选中 {selectedCount}
            </button>
          </div>
        </div>
        {atoms.length > 0 ? (
          <div className="interview-bank-table-wrap">
            <table className="interview-bank-table">
              <thead>
                <tr>
                  <th className="interview-bank-select-cell">
                    <input
                      type="checkbox"
                      aria-label="选择当前页可重建资源"
                      checked={allVisibleSelected}
                      disabled={selectableAtoms.length === 0}
                      onChange={(event) => toggleAllVisibleSelections(event.target.checked)}
                    />
                  </th>
                  <th>题目</th>
                  <th>分类</th>
                  <th>难度</th>
                  <th>角色</th>
                  <th>状态</th>
                  <th>索引</th>
                  <th>版本</th>
                  <th>更新时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {atoms.map((atom) => (
                  <AtomRow
                    atom={atom}
                    checked={selectedAtomIds.has(atom.id)}
                    onCheckedChange={(checked) => toggleAtomSelection(atom, checked)}
                    onOpen={() => void handleOpenAtom(atom.id)}
                    key={atom.id}
                  />
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="interview-bank-empty-row">
            <strong>暂无匹配题库资源</strong>
            <span>调整筛选条件或发布新的导入包。</span>
          </div>
        )}
      </section>

      <AtomDetailPanel
        atom={activeAtom}
        versions={activeVersions}
        form={editForm}
        isLoading={isDetailLoading}
        isSaving={isSavingAtom}
        archiveReason={archiveReason}
        isArchiving={isArchivingAtom}
        isRestoring={isRestoringAtom}
        onChange={updateEditForm}
        onArchiveReasonChange={setArchiveReason}
        onSave={() => void handleSaveAtom()}
        onArchive={() => void handleArchiveAtom()}
        onRestore={() => void handleRestoreAtom()}
      />
    </section>
  )
}

function HealthDiagnosticPanel({
  health,
  highlights,
  onApplyCombination,
  onRebuild,
  isRebuilding,
}: {
  health: InterviewKnowledgeHealthResponse | null
  highlights: InterviewKnowledgeHealthCombination[]
  onApplyCombination: (combo: InterviewKnowledgeHealthCombination) => void
  onRebuild: () => void
  isRebuilding: boolean
}) {
  if (!health) {
    return (
      <section className="panel interview-bank-health-panel">
        <div className="panel-title"><Database size={18} /> 健康诊断</div>
        <div className="interview-bank-empty-row">
          <strong>暂无健康数据</strong>
          <span>刷新后会展示题库组合状态。</span>
        </div>
      </section>
    )
  }

  return (
    <section className="panel interview-bank-health-panel">
      <div className="interview-bank-list-title">
        <div className="panel-title"><Database size={18} /> 健康诊断</div>
        <button className="ghost-button compact" type="button" onClick={onRebuild} disabled={isRebuilding}>
          <RefreshCw size={16} />{isRebuilding ? '重建中' : '重建异常索引'}
        </button>
      </div>
      <div className="interview-bank-health-metrics">
        <Metric label="可开放" value={health.summary.open_combinations} variant="compact" />
        <Metric label="告警" value={health.summary.warning_combinations} variant="compact" />
        <Metric label="阻断" value={health.summary.blocked_combinations} variant="compact" />
        <Metric label="索引失败" value={health.summary.vector_failed_atoms} variant="compact" />
      </div>
      {highlights.length > 0 ? (
        <div className="interview-bank-health-alerts">
          {highlights.map((combo) => (
            <button
              className={`interview-bank-health-alert status-${combo.status}`}
              type="button"
              key={`${combo.domain}|${combo.category}|${combo.difficulty}`}
              onClick={() => onApplyCombination(combo)}
            >
              <strong>{categoryLabel(combo.category)} · {combo.difficulty}</strong>
              <span>{healthStatusLabel(combo.status)} · {combo.reasons.join(' / ')}</span>
            </button>
          ))}
        </div>
      ) : (
        <div className="interview-bank-empty-row">
          <strong>当前组合健康</strong>
          <span>已发布组合具备开场题、追问题和已索引追问资源。</span>
        </div>
      )}
      <div className="interview-bank-health-table-wrap">
        <table className="interview-bank-health-table">
          <thead>
            <tr>
              <th>组合</th>
              <th>状态</th>
              <th>开场</th>
              <th>追问</th>
              <th>已索引追问</th>
              <th>待索引</th>
              <th>失败</th>
              <th>原因</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {health.combinations.map((combo) => (
              <tr key={`${combo.domain}|${combo.category}|${combo.difficulty}`}>
                <td>
                  <strong>{categoryLabel(combo.category)} · {combo.difficulty}</strong>
                  <span>{combo.domain || '未填领域'} · {combo.total_count} 条</span>
                </td>
                <td><span className={`interview-bank-pill health-${combo.status}`}>{healthStatusLabel(combo.status)}</span></td>
                <td>{combo.opening_count}</td>
                <td>{combo.followup_count}</td>
                <td>{combo.indexed_followup_count}</td>
                <td>{combo.pending_count}</td>
                <td>{combo.failed_count}</td>
                <td>{combo.reasons.join(' / ')}</td>
                <td>
                  <button className="ghost-button compact" type="button" onClick={() => onApplyCombination(combo)}>
                    <Search size={15} />定位
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function RetrievalPreviewPanel({
  form,
  result,
  isPreviewing,
  onChange,
  onSubmit,
  onOpenAtom,
}: {
  form: RetrievalPreviewForm
  result: InterviewKnowledgeRetrievalPreviewResponse | null
  isPreviewing: boolean
  onChange: <K extends keyof RetrievalPreviewForm>(key: K, value: RetrievalPreviewForm[K]) => void
  onSubmit: () => void
  onOpenAtom: (atomID: string) => void
}) {
  return (
    <section className="panel interview-bank-preview-panel">
      <div className="panel-title"><Search size={18} /> 检索预览</div>
      <div className="interview-bank-preview-form">
        <label>
          领域
          <input value={form.domain} onChange={(event) => onChange('domain', event.target.value)} placeholder="backend" />
        </label>
        <label>
          分类
          <select value={form.category} onChange={(event) => onChange('category', event.target.value)}>
            {categoryOptions.filter((option) => option.value).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </label>
        <label>
          难度
          <select value={form.difficulty} onChange={(event) => onChange('difficulty', event.target.value)}>
            {difficultyOptions.filter(Boolean).map((value) => <option key={value} value={value}>{value}</option>)}
          </select>
        </label>
        <label>
          返回数
          <select value={form.limit} onChange={(event) => onChange('limit', Number(event.target.value))}>
            {[3, 5, 10, 20].map((value) => <option key={value} value={value}>{value}</option>)}
          </select>
        </label>
        <label className="span-4">
          模拟文本
          <textarea
            value={form.query}
            onChange={(event) => onChange('query', event.target.value)}
            rows={5}
            placeholder="输入候选人的回答、追问意图或检索 query"
          />
        </label>
        <button className="primary-button compact span-4" type="button" onClick={onSubmit} disabled={isPreviewing}>
          <Search size={16} />{isPreviewing ? '预览中' : '执行检索预览'}
        </button>
      </div>
      {result ? (
        <div className={`interview-bank-preview-result ${result.fallback_used ? 'has-errors' : ''}`}>
          <div className="interview-bank-report-heading">
            <strong>{result.fallback_used ? '检索回退' : `命中 ${result.matched_count} 个题库原子`}</strong>
            <span>
              候选 {result.diagnostics.candidate_count} · 已索引 {result.diagnostics.indexed_candidates}
              {result.fallback_reason ? ` · ${result.fallback_reason}` : ''}
            </span>
          </div>
          <div className="interview-bank-report-metrics">
            <Metric label="已发布候选" value={result.diagnostics.published_candidates} variant="compact" />
            <Metric label="待索引" value={result.diagnostics.pending_candidates} variant="compact" />
            <Metric label="失败" value={result.diagnostics.failed_candidates} variant="compact" />
            <Metric label="归档" value={result.diagnostics.archived_candidates} variant="compact" />
          </div>
          {result.results.length > 0 ? (
            <div className="interview-bank-preview-result-list">
              {result.results.map((item) => (
                <div className="interview-bank-preview-hit" key={`${item.atom_id}|${item.doc_type}|${item.doc_key}`}>
                  <div>
                    <strong>{item.title}</strong>
                    <span>{item.subject} · {questionRoleLabel(item.question_role)} · {item.difficulty} · {previewScoreLabel(item.score)}</span>
                    <small>{docTypeLabel(item.doc_type)} · {item.snippet}</small>
                  </div>
                  <button className="ghost-button compact" type="button" onClick={() => onOpenAtom(item.atom_id)}>
                    <Eye size={15} />查看
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <div className="interview-bank-empty-row">
              <strong>没有可展示命中</strong>
              <span>检查组合题量、索引状态或输入文本。</span>
            </div>
          )}
        </div>
      ) : null}
    </section>
  )
}

function RetrievalOperationsPanel({
  analytics,
  logs,
  onOpenAtom,
  onApplyCombination,
}: {
  analytics: InterviewRetrievalAnalyticsResponse | null
  logs: InterviewRetrievalLog[]
  onOpenAtom: (atomID: string) => void
  onApplyCombination: (combo: InterviewRetrievalFallbackCombination) => void
}) {
  const hasAnalytics = Boolean(analytics && analytics.total_logs > 0)
  const recentFallbacks = analytics?.recent_fallbacks ?? []

  return (
    <section className="panel interview-bank-retrieval-ops-panel">
      <div className="interview-bank-list-title">
        <div>
          <div className="panel-title"><Search size={18} /> 真实检索运营</div>
          <p className="interview-bank-panel-subtitle">仅展示脱敏截断后的追问检索日志，不包含用户身份、完整回答或简历原文。</p>
        </div>
        <span className="interview-bank-retrieval-window">最近 {analytics?.total_logs ?? 0} 次</span>
      </div>

      <div className="interview-bank-retrieval-metrics">
        <Metric label="真实检索" value={analytics?.total_logs ?? 0} variant="compact" />
        <Metric label="命中率" value={formatPercent(analytics?.hit_rate)} variant="compact" />
        <Metric label="回退率" value={formatPercent(analytics?.fallback_rate)} variant="compact" />
        <Metric label="回退次数" value={analytics?.fallback_logs ?? 0} variant="compact" />
      </div>

      {hasAnalytics ? (
        <>
          <div className="interview-bank-retrieval-columns">
            <RetrievalAtomHitList
              title="热门命中原子"
              emptyTitle="暂无命中原子"
              emptyText="真实追问命中后会在这里展示高频资源。"
              atoms={analytics?.top_hit_atoms ?? []}
              tone="hot"
              onOpenAtom={onOpenAtom}
            />
            <RetrievalAtomHitList
              title="低命中/未命中原子"
              emptyTitle="暂无低命中资源"
              emptyText="当前分析窗口内没有需要关注的低命中资源。"
              atoms={analytics?.low_hit_atoms ?? []}
              tone="cold"
              onOpenAtom={onOpenAtom}
            />
            <div className="interview-bank-retrieval-column">
              <div className="interview-bank-retrieval-column-title">回退组合排行</div>
              {(analytics?.fallback_combinations ?? []).length > 0 ? (
                <div className="interview-bank-retrieval-list">
                  {(analytics?.fallback_combinations ?? []).map((combo) => (
                    <div className="interview-bank-retrieval-row" key={`${combo.domain}|${combo.category}|${combo.difficulty}`}>
                      <div>
                        <strong>{retrievalCombinationLabel(combo)}</strong>
                        <span>{combo.count} 次回退 · {formatOptionalDate(combo.last_seen_at)}</span>
                        <small>{combo.recent_reason || '暂无最近原因'}</small>
                      </div>
                      <button className="ghost-button compact" type="button" onClick={() => onApplyCombination(combo)}>
                        <Search size={15} />套用
                      </button>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="interview-bank-empty-row">
                  <strong>暂无回退组合</strong>
                  <span>当前窗口内没有真实回退记录。</span>
                </div>
              )}
            </div>
          </div>

          <div className="interview-bank-retrieval-columns lower">
            <div className="interview-bank-retrieval-column">
              <div className="interview-bank-retrieval-column-title">最近回退原因</div>
              {recentFallbacks.length > 0 ? (
                <div className="interview-bank-retrieval-list">
                  {recentFallbacks.map((log) => (
                    <RetrievalLogRow log={log} onOpenAtom={onOpenAtom} key={log.id} />
                  ))}
                </div>
              ) : (
                <div className="interview-bank-empty-row">
                  <strong>暂无最近回退</strong>
                  <span>真实追问检索回退后会记录原因。</span>
                </div>
              )}
            </div>
            <div className="interview-bank-retrieval-column span-2">
              <div className="interview-bank-retrieval-column-title">最近真实检索日志</div>
              {logs.length > 0 ? (
                <div className="interview-bank-retrieval-log-list">
                  {logs.map((log) => (
                    <RetrievalLogRow log={log} onOpenAtom={onOpenAtom} key={log.id} />
                  ))}
                </div>
              ) : (
                <div className="interview-bank-empty-row">
                  <strong>暂无真实检索日志</strong>
                  <span>用户面试触发追问检索后会出现在这里。</span>
                </div>
              )}
            </div>
          </div>
        </>
      ) : (
        <div className="interview-bank-empty-row">
          <strong>暂无真实检索数据</strong>
          <span>完成真实面试追问后会生成脱敏日志，再形成命中率、回退率和资源排行。</span>
        </div>
      )}
    </section>
  )
}

function RetrievalAtomHitList({
  title,
  emptyTitle,
  emptyText,
  atoms,
  tone,
  onOpenAtom,
}: {
  title: string
  emptyTitle: string
  emptyText: string
  atoms: InterviewRetrievalAtomHit[]
  tone: 'hot' | 'cold'
  onOpenAtom: (atomID: string) => void
}) {
  return (
    <div className="interview-bank-retrieval-column">
      <div className="interview-bank-retrieval-column-title">{title}</div>
      {atoms.length > 0 ? (
        <div className="interview-bank-retrieval-list">
          {atoms.map((atom) => (
            <div className={`interview-bank-retrieval-row tone-${tone}`} key={atom.atom_id}>
              <div>
                <strong>{atom.title || atom.atom_id}</strong>
                <span>{atom.subject || '未填考察点'} · {compactCategoryLabel(atom.category)} · {atom.difficulty || '未填难度'}</span>
                <small>{atom.hit_count} 次命中 · {questionRoleLabel(atom.question_role)} · {formatOptionalDate(atom.last_hit_at)}</small>
              </div>
              <button className="ghost-button compact" type="button" onClick={() => onOpenAtom(atom.atom_id)}>
                <Eye size={15} />查看
              </button>
            </div>
          ))}
        </div>
      ) : (
        <div className="interview-bank-empty-row">
          <strong>{emptyTitle}</strong>
          <span>{emptyText}</span>
        </div>
      )}
    </div>
  )
}

function RetrievalLogRow({ log, onOpenAtom }: { log: InterviewRetrievalLog; onOpenAtom: (atomID: string) => void }) {
  const matchedAtoms = Array.isArray(log.matched_atoms) ? log.matched_atoms : []
  return (
    <div className={`interview-bank-retrieval-log-row ${log.fallback_used ? 'is-fallback' : ''}`}>
      <div>
        <strong>{log.fallback_used ? '检索回退' : `命中 ${matchedAtoms.length} 个原子`} · 第 {log.round} 轮</strong>
        <span>{formatOptionalDate(log.created_at)} · {truncateDisplayText(log.query_text || '无 query', 96)}</span>
        <small>{log.error_message || (matchedAtoms.length > 0 ? matchedAtoms.map((atom) => atom.title || atom.atom_id).join(' / ') : '无错误信息')}</small>
      </div>
      {matchedAtoms.length > 0 ? (
        <div className="interview-bank-retrieval-log-actions">
          {matchedAtoms.slice(0, 3).map((atom) => (
            <button className="ghost-button compact" type="button" onClick={() => onOpenAtom(atom.atom_id)} key={`${log.id}|${atom.atom_id}`}>
              <Eye size={14} />{truncateDisplayText(atom.title || atom.atom_id, 18)}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function AtomRow({
  atom,
  checked,
  onCheckedChange,
  onOpen,
}: {
  atom: InterviewKnowledgeAtom
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  onOpen: () => void
}) {
  const canSelect = canRebuildAtom(atom)
  return (
    <tr>
      <td className="interview-bank-select-cell">
        <input
          type="checkbox"
          aria-label={`选择 ${atom.title}`}
          checked={checked}
          disabled={!canSelect}
          onChange={(event) => onCheckedChange(event.target.checked)}
        />
      </td>
      <td>
        <strong>{atom.title}</strong>
        <span>{atom.id} · {atom.subject}</span>
        <small>{atom.tags.slice(0, 4).join(' / ') || atom.source_ref}</small>
      </td>
      <td>{categoryLabel(atom.category)}</td>
      <td>{atom.difficulty}</td>
      <td>{questionRoleLabel(atom.question_role)}</td>
      <td><span className={`interview-bank-pill status-${atom.status}`}>{statusLabel(atom.status)}</span></td>
      <td><span className={`interview-bank-pill vector-${atom.vector_status}`}>{vectorStatusLabel(atom.vector_status)}</span></td>
      <td>v{atom.current_version}</td>
      <td>{formatOptionalDate(atom.updated_at)}</td>
      <td>
        <button className="ghost-button compact" type="button" onClick={onOpen}>
          <Eye size={15} />查看
        </button>
      </td>
    </tr>
  )
}

function AtomDetailPanel({
  atom,
  versions,
  form,
  isLoading,
  isSaving,
  archiveReason,
  isArchiving,
  isRestoring,
  onChange,
  onArchiveReasonChange,
  onSave,
  onArchive,
  onRestore,
}: {
  atom: InterviewKnowledgeAtom | null
  versions: InterviewKnowledgeAtomVersion[]
  form: AtomEditForm | null
  isLoading: boolean
  isSaving: boolean
  archiveReason: string
  isArchiving: boolean
  isRestoring: boolean
  onChange: <K extends keyof AtomEditForm>(key: K, value: AtomEditForm[K]) => void
  onArchiveReasonChange: (value: string) => void
  onSave: () => void
  onArchive: () => void
  onRestore: () => void
}) {
  if (isLoading) {
    return (
      <section className="panel interview-bank-detail-panel">
        <div className="panel-title"><Eye size={18} /> 题目详情</div>
        <div className="interview-bank-empty-row">
          <strong>正在读取题目详情</strong>
          <span>请稍候。</span>
        </div>
      </section>
    )
  }

  if (!atom || !form) {
    return (
      <section className="panel interview-bank-detail-panel">
        <div className="panel-title"><Eye size={18} /> 题目详情</div>
        <div className="interview-bank-empty-row">
          <strong>未选择题目</strong>
          <span>在题库资源列表中点击“查看”后可编辑内容并查看版本历史。</span>
        </div>
      </section>
    )
  }

  return (
    <section className="panel interview-bank-detail-panel">
      <div className="interview-bank-detail-header">
        <div>
          <div className="panel-title"><Eye size={18} /> 题目详情</div>
          <h2>{atom.title}</h2>
          <p>{atom.id} · v{atom.current_version} · {statusLabel(atom.status)} · {vectorStatusLabel(atom.vector_status)}</p>
        </div>
        <div className="interview-bank-detail-actions">
          <button className="primary-button compact" type="button" onClick={onSave} disabled={isSaving || isArchiving || isRestoring}>
            <Save size={16} />{isSaving ? '保存中' : '保存编辑'}
          </button>
          {atom.status === 'archived' ? (
            <button className="ghost-button compact" type="button" onClick={onRestore} disabled={isSaving || isArchiving || isRestoring}>
              <RotateCcw size={16} />{isRestoring ? '恢复中' : '恢复'}
            </button>
          ) : null}
        </div>
      </div>

      {atom.status !== 'archived' ? (
        <div className="interview-bank-archive-box">
          <label>
            归档原因
            <input value={archiveReason} onChange={(event) => onArchiveReasonChange(event.target.value)} placeholder="说明为什么下架该题" />
          </label>
          <button className="ghost-button compact danger-button" type="button" onClick={onArchive} disabled={isSaving || isArchiving || isRestoring}>
            <Archive size={16} />{isArchiving ? '归档中' : '归档'}
          </button>
        </div>
      ) : (
        <div className="interview-bank-archive-box archived">
          <span>该题已归档，不会进入后续新面试或追问检索。</span>
          <button className="ghost-button compact" type="button" onClick={onRestore} disabled={isSaving || isArchiving || isRestoring}>
            <RotateCcw size={16} />{isRestoring ? '恢复中' : '恢复为已发布'}
          </button>
        </div>
      )}

      <div className="interview-bank-edit-grid">
        <label>
          展示标题
          <input value={form.title} onChange={(event) => onChange('title', event.target.value)} />
        </label>
        <label>
          考察点标题
          <input value={form.subject} onChange={(event) => onChange('subject', event.target.value)} />
        </label>
        <label>
          领域
          <input value={form.domain} onChange={(event) => onChange('domain', event.target.value)} />
        </label>
        <label>
          难度
          <select value={form.difficulty} onChange={(event) => onChange('difficulty', event.target.value)}>
            {difficultyOptions.filter(Boolean).map((value) => <option key={value} value={value}>{value}</option>)}
          </select>
        </label>
        <label>
          分类
          <select value={form.category} onChange={(event) => onChange('category', event.target.value)}>
            {categoryOptions.filter((option) => option.value).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </label>
        <label>
          题目角色
          <select value={form.question_role} onChange={(event) => onChange('question_role', event.target.value)}>
            {questionRoleOptions.filter(Boolean).map((value) => <option key={value} value={value}>{questionRoleLabel(value)}</option>)}
          </select>
        </label>
        <label className="span-2">
          来源追溯
          <input value={form.source_ref} onChange={(event) => onChange('source_ref', event.target.value)} />
        </label>
        <label className="span-2">
          标签
          <textarea value={form.tagsText} onChange={(event) => onChange('tagsText', event.target.value)} rows={3} />
        </label>
        <label>
          原理要点
          <textarea value={form.principlesText} onChange={(event) => onChange('principlesText', event.target.value)} rows={6} />
        </label>
        <label>
          常见误区
          <textarea value={form.pitfallsText} onChange={(event) => onChange('pitfallsText', event.target.value)} rows={6} />
        </label>
        <label className="span-2">
          追问路径
          <textarea value={form.followUpPathsText} onChange={(event) => onChange('followUpPathsText', event.target.value)} rows={5} />
        </label>
        <label className="span-2">
          编辑备注
          <input value={form.change_note} onChange={(event) => onChange('change_note', event.target.value)} placeholder="说明本次修改原因" />
        </label>
      </div>

      <div className="interview-bank-version-panel">
        <div className="panel-title"><History size={18} /> 版本历史</div>
        {versions.length > 0 ? (
          <div className="interview-bank-version-list">
            {versions.map((version) => (
              <div className="interview-bank-version-row" key={version.id}>
                <strong>v{version.version} · {versionTypeLabel(version.version_type)}</strong>
                <span>{version.change_note || '无备注'} · {version.admin_id || 'system'} · {formatOptionalDate(version.created_at)}</span>
                <small>{version.no_content_change ? '无内容变化' : changedFieldsLabel(version.diff_summary)}</small>
              </div>
            ))}
          </div>
        ) : (
          <div className="interview-bank-empty-row">
            <strong>暂无版本历史</strong>
            <span>保存或导入后会生成版本记录。</span>
          </div>
        )}
      </div>
    </section>
  )
}

function IndexRebuildPanel({ result }: { result: InterviewKnowledgeIndexRebuildResponse }) {
  const failures = result.results.filter((item) => item.status === 'failed' || item.status === 'skipped')
  return (
    <div className={`interview-bank-report ${result.failed > 0 ? 'has-errors' : ''}`}>
      <div className="interview-bank-report-heading">
        <strong>索引重建</strong>
        <span>{result.total} 条 · 成功 {result.indexed} · 失败 {result.failed} · 跳过 {result.skipped}</span>
      </div>
      <div className="interview-bank-report-metrics">
        <Metric label="成功" value={result.indexed} variant="compact" />
        <Metric label="失败" value={result.failed} variant="compact" />
        <Metric label="跳过" value={result.skipped} variant="compact" />
        <Metric label="总数" value={result.total} variant="compact" />
      </div>
      {failures.length > 0 && (
        <div className="interview-bank-report-issues">
          <strong><AlertTriangle size={15} />结果</strong>
          {failures.slice(0, 8).map((item) => (
            <span key={item.atom_id}>{item.atom_id} · {item.status === 'skipped' ? '已跳过' : '失败'} · {item.error || '无详情'}</span>
          ))}
        </div>
      )}
    </div>
  )
}

function ImportReportPanel({ report }: { report: InterviewKnowledgeImportReport }) {
  const errorCount = report.summary.error_count ?? 0
  return (
    <div className={`interview-bank-report ${errorCount > 0 ? 'has-errors' : ''}`}>
      <div className="interview-bank-report-heading">
        <strong>{report.batch_id}</strong>
        <span>{report.source_ref || 'manual'} · {report.summary.valid_count ?? 0}/{report.summary.total ?? 0}</span>
      </div>
      <div className="interview-bank-report-metrics">
        <Metric label="新增" value={report.summary.create_count ?? 0} variant="compact" />
        <Metric label="更新" value={report.summary.update_count ?? 0} variant="compact" />
        <Metric label="重复" value={report.summary.duplicate_count ?? 0} variant="compact" />
        <Metric label="错误" value={errorCount} variant="compact" />
      </div>
      {report.errors.length > 0 && (
        <div className="interview-bank-report-issues">
          <strong><AlertTriangle size={15} />错误</strong>
          {report.errors.slice(0, 8).map((item) => <span key={item}>{item}</span>)}
        </div>
      )}
      <div className="interview-bank-result-list">
        {report.results.slice(0, 8).map((item) => (
          <div className={`interview-bank-result-row action-${item.action}`} key={`${item.index}-${item.id ?? 'empty'}`}>
            <strong>{item.id || `items[${item.index}]`} · {actionLabel(item.action)}</strong>
            <span>{item.title || '未命名'}{item.existing_version ? ` · v${item.existing_version}` : ''}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function atomToEditForm(atom: InterviewKnowledgeAtom): AtomEditForm {
  return {
    base_version: atom.current_version,
    change_note: '',
    title: atom.title,
    subject: atom.subject,
    domain: atom.domain,
    difficulty: atom.difficulty,
    category: atom.category,
    question_role: atom.question_role,
    source_ref: atom.source_ref,
    tagsText: atom.tags.join(', '),
    principlesText: atom.principles.join('\n'),
    pitfallsText: atom.pitfalls.join('\n'),
    followUpPathsText: atom.follow_up_paths.join('\n'),
  }
}

function editFormToPayload(form: AtomEditForm): InterviewKnowledgeAtomUpdateRequest {
  return {
    base_version: form.base_version,
    change_note: form.change_note,
    title: form.title,
    subject: form.subject,
    domain: form.domain,
    difficulty: form.difficulty,
    category: form.category,
    question_role: form.question_role,
    source_ref: form.source_ref,
    tags: parseDelimitedList(form.tagsText),
    principles: parseLineList(form.principlesText),
    pitfalls: parseLineList(form.pitfallsText),
    follow_up_paths: parseLineList(form.followUpPathsText),
  }
}

function parseLineList(value: string) {
  return value.split('\n').map((item) => item.trim()).filter(Boolean)
}

function parseDelimitedList(value: string) {
  const seen = new Set<string>()
  const items: string[] = []
  for (const item of value.split(/[\n,，;；]/)) {
    const normalized = item.trim()
    if (!normalized) continue
    const key = normalized.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    items.push(normalized)
  }
  return items
}

function statusLabel(value?: string) {
  switch (value) {
    case 'published':
      return '已发布'
    case 'draft':
      return '草稿'
    case 'archived':
      return '已归档'
    default:
      return '全部状态'
  }
}

function questionRoleLabel(value?: string) {
  switch (value) {
    case 'opening':
      return '开场'
    case 'followup':
      return '追问'
    case 'mixed':
      return '混合'
    default:
      return '全部角色'
  }
}

function vectorStatusLabel(value?: string) {
  switch (value) {
    case 'pending':
      return '待索引'
    case 'indexed':
      return '已索引'
    case 'failed':
      return '索引失败'
    default:
      return '全部索引'
  }
}

function healthStatusLabel(value?: string) {
  switch (value) {
    case 'open':
      return '可开放'
    case 'warning':
      return '告警'
    case 'blocked':
      return '阻断'
    default:
      return value || '未知'
  }
}

function categoryLabel(value: string) {
  return categoryOptions.find((option) => option.value === value)?.label ?? value
}

function compactCategoryLabel(value?: string) {
  return value ? categoryLabel(value) : '未填分类'
}

function retrievalCombinationLabel(combo: InterviewRetrievalFallbackCombination) {
  return `${combo.domain || '未填领域'} · ${compactCategoryLabel(combo.category)} · ${combo.difficulty || '未填难度'}`
}

function docTypeLabel(value: string) {
  switch (value) {
    case 'overview':
      return '概览'
    case 'principle':
      return '原理'
    case 'pitfall':
      return '误区'
    case 'follow_up':
      return '追问'
    default:
      return value
  }
}

function previewScoreLabel(value: number) {
  if (!Number.isFinite(value)) return 'score -'
  return `score ${value.toFixed(3)}`
}

function formatPercent(value?: number) {
  if (!Number.isFinite(value ?? Number.NaN)) return '0%'
  return `${((value ?? 0) * 100).toFixed(1)}%`
}

function truncateDisplayText(value: string, maxLength: number) {
  if (value.length <= maxLength) return value
  return `${value.slice(0, Math.max(0, maxLength - 1))}…`
}

function actionLabel(value: string) {
  switch (value) {
    case 'create':
      return '新增'
    case 'update':
      return '更新'
    case 'duplicate_import':
      return '重复导入'
    case 'invalid':
      return '校验失败'
    default:
      return value
  }
}

function versionTypeLabel(value: string) {
  switch (value) {
    case 'content_update':
      return '内容导入'
    case 'duplicate_import':
      return '重复导入'
    case 'manual_edit':
      return '在线编辑'
    case 'archive':
      return '归档'
    case 'restore_archived':
      return '恢复归档'
    default:
      return value
  }
}

function changedFieldsLabel(diffSummary: Record<string, unknown>) {
  const fields = diffSummary.fields_changed
  if (Array.isArray(fields) && fields.length > 0) {
    return `变更字段：${fields.join(' / ')}`
  }
  if (diffSummary.created) {
    return '创建版本'
  }
  return '无字段摘要'
}

function canRebuildAtom(atom: InterviewKnowledgeAtom) {
  return atom.vector_status === 'pending' || atom.vector_status === 'failed'
}

function formatOptionalDate(value?: string) {
  if (!value) return '暂无'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
