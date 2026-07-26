const MIN_PASSWORD_BYTES = 12
const MAX_PASSWORD_BYTES = 72

export function passwordByteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export function passwordValidationMessage(value: string, label = '密码'): string {
  const bytes = passwordByteLength(value)
  if (bytes < MIN_PASSWORD_BYTES) return `${label}至少需要 ${MIN_PASSWORD_BYTES} 个字节`
  if (bytes > MAX_PASSWORD_BYTES) return `${label}不能超过 ${MAX_PASSWORD_BYTES} 个字节（中文通常占 3 个字节）`
  return ''
}
