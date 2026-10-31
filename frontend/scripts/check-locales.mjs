// 语言完整性门禁：比较 zh-CN 与其他语言文件的键集合，缺键/多键即失败（构建红）。
// 用法：node scripts/check-locales.mjs（npm run check:locales；build 前自动跑）。
import { readdirSync, readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'i18n', 'messages')
const files = readdirSync(root).filter((f) => f.endsWith('.json'))
if (!files.includes('zh-CN.json')) {
  console.error('[check-locales] 缺少基准语言文件 zh-CN.json')
  process.exit(1)
}

const base = JSON.parse(readFileSync(join(root, 'zh-CN.json'), 'utf8'))
const baseKeys = new Set(Object.keys(base))
let failed = false

for (const file of files) {
  if (file === 'zh-CN.json') continue
  const data = JSON.parse(readFileSync(join(root, file), 'utf8'))
  const keys = new Set(Object.keys(data))
  const missing = [...baseKeys].filter((k) => !keys.has(k))
  const extra = [...keys].filter((k) => !baseKeys.has(k))
  if (missing.length) {
    console.error(`[check-locales] ${file} 缺 ${missing.length} 个键：${missing.slice(0, 5).join(', ')}${missing.length > 5 ? ' …' : ''}`)
    failed = true
  }
  if (extra.length) {
    console.error(`[check-locales] ${file} 多出 ${extra.length} 个键：${extra.slice(0, 5).join(', ')}${extra.length > 5 ? ' …' : ''}`)
    failed = true
  }
}

if (failed) {
  console.error('[check-locales] 语言文件键集合不一致，构建失败。')
  process.exit(1)
}
console.log(`[check-locales] ${files.length} 个语言文件键集合一致（基准 ${baseKeys.size} 键）。`)
