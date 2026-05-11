export const domains = [
  { value: 'database', label: '数据库' },
  { value: 'network', label: '网络' },
  { value: 'os', label: '操作系统' },
  { value: 'security', label: '安全' },
  { value: 'devops', label: 'DevOps' },
]


export function domainLabel(value: string) {
  return domains.find((item) => item.value === value)?.label ?? value
}
