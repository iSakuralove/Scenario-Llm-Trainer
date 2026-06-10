export function MermaidLoading({ message = '正在加载图形', detail = '正在校验 Mermaid 语法并生成预览。' }: { message?: string; detail?: string }) {
  return (
    <div className="mermaid-loading" role="status">
      <span aria-hidden="true" />
      <strong>{message}</strong>
      <small>{detail}</small>
    </div>
  )
}
