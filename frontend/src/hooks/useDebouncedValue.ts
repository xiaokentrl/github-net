import { useEffect, useState } from 'react'

// 只用来合并搜索这类高频输入；卸载时清掉计时器，不用它掩盖慢请求。
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs)
    return () => window.clearTimeout(timer)
  }, [value, delayMs])

  return debounced
}
