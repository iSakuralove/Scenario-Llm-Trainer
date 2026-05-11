export function redactSensitiveText(value = '') {
  return value
    .replace(/\b(?:\d{1,3}\.){3}\d{1,3}\b/g, '【IP地址】')
    .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '【邮箱】')
    .replace(/\bsk-[A-Za-z0-9_-]+/g, '【密钥】')
    .replace(/\b(?:sk|sl|ak|tk)-[A-Za-z0-9_\-;']{6,}\b/gi, '【密钥】')
    .replace(/\b(password|passwd|api[_ -]?key|apikey|access[_ -]?key|secret[_ -]?key|token|secret|key)\s*[:=：]\s*[^\s,，。]+/gi, '$1=【密钥】')
    .replace(/(密码|密钥|口令|令牌|凭证|API\s*KEY|Token)\s*(为|是|[:=：])\s*[^\s,，。]+/gi, '$1$2【密钥】')
    .replace(/(密码|口令|密钥|令牌|凭证|API\s*KEY|Token|key|secret)([^，。,；;]*?)(设置成了|设置为|改成了|改为了|设为|是|为)\s*[^\s,，。；;]+/gi, '$1$2$3【密钥】')
    .replace(/(设置成了|设置为|改成了|改为了|设为)\s*[^\s,，。；;]*(?:@|[_-])[^\s,，。；;]*/gi, '$1【密钥】')
    .replace(/(API\s*KEY|api[_ -]?key|apikey|token|secret|key)\s*[-=]\s*(?:\[已脱敏\]|\[[^\]]*脱敏[^\]]*\])?[^\s,，。]+/gi, '$1=【密钥】')
    .replace(/(password|passwd)\s*[-=]\s*(?:\[已脱敏\]|\[[^\]]*脱敏[^\]]*\])?[^\s,，。]+/gi, '$1=【密钥】')
    .replace(/[\u4e00-\u9fa5A-Za-z0-9+._-]{2,}(?:公司|教育|学校|机构|集团|银行|医院)/g, '【机构A】')
}
