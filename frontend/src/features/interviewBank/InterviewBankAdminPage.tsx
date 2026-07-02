import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Database,
  FileJson,
  ListFilter,
  PackageCheck,
  RefreshCw,
  Search,
  Upload,
} from 'lucide-react'
import { api } from '../../api/client'
import { Loading, Metric } from '../../components/common'
import { useToken } from '../../lib/auth'
import type {
  InterviewKnowledgeAtom,
  InterviewKnowledgeAtomFilters,
  InterviewKnowledgeBatch,
  InterviewKnowledgeIndexRebuildRequest,
  InterviewKnowledgeIndexRebuildResponse,
  InterviewKnowledgeImportReport,
  InterviewKnowledgePublishResponse,
  InterviewKnowledgeSummary,
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

export function InterviewBankAdminPage() {
  const token = useToken()
  const [summary, setSummary] = useState<InterviewKnowledgeSummary | null>(null)
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

  const loadData = useCallback(async () => {
    setIsLoading(true)
    setError('')
    try {
      const [nextSummary, atomData, batchData] = await Promise.all([
        api.adminInterviewBankSummary(token),
        api.adminInterviewBankAtoms(token, filters),
        api.adminInterviewBankBatches(token, 20),
      ])
      setSummary(nextSummary)
      setAtoms(atomData.list ?? [])
      setTotalAtoms(atomData.total ?? 0)
      setBatches(batchData.list ?? [])
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

  function updateFilter(key: keyof InterviewKnowledgeAtomFilters, value: string) {
    setFilters((current) => ({ ...current, [key]: value }))
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
                </tr>
              </thead>
              <tbody>
                {atoms.map((atom) => (
                  <AtomRow
                    atom={atom}
                    checked={selectedAtomIds.has(atom.id)}
                    onCheckedChange={(checked) => toggleAtomSelection(atom, checked)}
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
    </section>
  )
}

function AtomRow({
  atom,
  checked,
  onCheckedChange,
}: {
  atom: InterviewKnowledgeAtom
  checked: boolean
  onCheckedChange: (checked: boolean) => void
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
    </tr>
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

function categoryLabel(value: string) {
  return categoryOptions.find((option) => option.value === value)?.label ?? value
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

function canRebuildAtom(atom: InterviewKnowledgeAtom) {
  return atom.vector_status === 'pending' || atom.vector_status === 'failed'
}

function formatOptionalDate(value?: string) {
  if (!value) return '暂无'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
