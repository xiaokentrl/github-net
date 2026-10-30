import { useCallback, useEffect, useRef, useState } from 'react'
import type { MutableRefObject, PointerEvent as ReactPointerEvent } from 'react'

export type BarAxis = 'x' | 'y'

export type BarResize = {
  dragging: boolean
  /** 拖动期间的临时尺寸；松手后交给后端，由配置快照接管。 */
  liveValue: number | null
  /** 挂到被拖动的那一栏上，用来读拖动开始时的实际尺寸。 */
  targetRef: MutableRefObject<HTMLElement | null>
  onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void
  onPointerMove: (event: ReactPointerEvent<HTMLElement>) => void
  onPointerUp: (event: ReactPointerEvent<HTMLElement>) => void
}

export type BarResizeOptions = {
  /** 拖的是竖直分隔条（改宽度）还是水平分隔条（改高度）。 */
  axis?: BarAxis
  /** 往哪个方向拖算「变大」。队列在下方：手柄往上拖才是变高，所以要取反。 */
  invert?: boolean
}

// 分隔条拖动的通用实现：侧栏宽度、队列里「任务列表」宽度、队列高度共用这一套时序。
// clamp 每次渲染都换引用，所以存进 ref，不让它进回调依赖（进了就每次渲染重建回调）。
// 拖动只在松手那一刻提交一次，过程只改本地预览：
// 每动一下都写盘会把配置刷成几百个 revision，也会让保存状态一直闪。
// 更新按 requestAnimationFrame 合并（结构规划第 323 行），指针事件本身可能一帧来好几次。
export function useBarResize(
  value: number | null,
  onCommit: (value: number) => void,
  clamp: (value: number) => number,
  fallback: number,
  options: BarResizeOptions = {},
): BarResize {
  const axis = options.axis ?? 'x'
  const invert = options.invert ?? false
  const targetRef = useRef<HTMLElement | null>(null)
  const clampRef = useRef(clamp)
  clampRef.current = clamp
  const [dragging, setDragging] = useState(false)
  const [liveValue, setLiveValue] = useState<number | null>(null)
  const drag = useRef<{ pointerId: number; startPos: number; startValue: number; lastPos: number } | null>(null)
  const rafPending = useRef(false)
  // 预览值另存一份：松手时要提交的就是它。状态更新函数里不能做提交这种副作用，
  // StrictMode 下更新函数可能被调两次，那样会连着写两次盘。
  const preview = useRef<number | null>(null)

  // 后端快照回来了就撤掉本地预览：保存成功时两个值相等（撤不撤视觉效果一样），
  // 保存失败时后端还是旧值，这里必须退回旧值，不能让界面停在没落盘成功的尺寸上。
  useEffect(() => {
    if (dragging || liveValue === null || value === null) return
    if (value !== liveValue) {
      preview.current = null
      setLiveValue(null)
    }
  }, [dragging, liveValue, value])

  const posOf = useCallback((event: { clientX: number; clientY: number }) => (axis === 'x' ? event.clientX : event.clientY), [axis])

  const applyDelta = useCallback(() => {
    rafPending.current = false
    const state = drag.current
    if (!state) return
    const step = invert ? state.startPos - state.lastPos : state.lastPos - state.startPos
    const next = clampRef.current(state.startValue + step)
    preview.current = next
    setLiveValue(next)
  }, [invert])

  const onPointerDown = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (event.button !== 0) return
      event.preventDefault()
      const element = targetRef.current
      const measured = axis === 'x' ? element?.offsetWidth : element?.offsetHeight
      const startValue = clampRef.current(measured ?? (value ?? fallback))
      const startPos = posOf(event)
      drag.current = { pointerId: event.pointerId, startPos, startValue, lastPos: startPos }
      preview.current = startValue
      // 抓住指针后，鼠标移出分隔条甚至移出窗口都还能继续拖，松手或抓取丢失都会走 onPointerUp。
      event.currentTarget.setPointerCapture(event.pointerId)
      setDragging(true)
      setLiveValue(startValue)
    },
    [axis, value, fallback, posOf],
  )

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      state.lastPos = posOf(event)
      if (rafPending.current) return
      rafPending.current = true
      requestAnimationFrame(applyDelta)
    },
    [applyDelta, posOf],
  )

  const onPointerUp = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      drag.current = null
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
      setDragging(false)
      // 只在这里提交一次。没拖动过就等于当前值，App 那层的「和后端一致就不写」会把它挡掉，
      // 所以按一下就松手不会白涨一个 revision。
      onCommit(preview.current ?? state.startValue)
    },
    [onCommit],
  )

  return { dragging, liveValue, targetRef, onPointerDown, onPointerMove, onPointerUp }
}
