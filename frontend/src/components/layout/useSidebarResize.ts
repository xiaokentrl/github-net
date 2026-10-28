import { useCallback, useEffect, useRef, useState } from 'react'
import type { MutableRefObject, PointerEvent as ReactPointerEvent } from 'react'

// 侧栏可拖范围与后端 internal/config 的 SidebarWidthMin/SidebarWidthMax 必须逐个相等，
// 这条相等断言在 internal/config/layout_bounds_parity_test.go。改这里不改后端会被门拦下。
export const SIDEBAR_MIN_WIDTH = 200
export const SIDEBAR_MAX_WIDTH = 600

export function clampSidebarWidth(value: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(value)))
}

export type SidebarResize = {
  dragging: boolean
  /** 拖动期间的临时宽度；松手后交给后端，由配置快照接管。 */
  liveWidth: number | null
  /** 挂到侧栏元素上，用来读拖动开始时的实际宽度。 */
  sidebarRef: MutableRefObject<HTMLElement | null>
  onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void
  onPointerMove: (event: ReactPointerEvent<HTMLElement>) => void
  onPointerUp: (event: ReactPointerEvent<HTMLElement>) => void
}

// 拖动只在松手那一刻提交一次，过程只改本地预览：
// 每动一下都写盘会把配置刷成几百个 revision，也会让保存状态一直闪。
// 更新按 requestAnimationFrame 合并（结构规划第 323 行），指针事件本身可能一帧来好几次。
export function useSidebarResize(width: number | null, onCommit: (width: number) => void): SidebarResize {
  const sidebarRef = useRef<HTMLElement | null>(null)
  const [dragging, setDragging] = useState(false)
  const [liveWidth, setLiveWidth] = useState<number | null>(null)
  const drag = useRef<{ pointerId: number; startX: number; startWidth: number; lastX: number } | null>(null)
  const rafPending = useRef(false)
  // 预览值另存一份：松手时要提交的就是它。状态更新函数里不能做提交这种副作用，
  // StrictMode 下更新函数可能被调两次，那样会连着写两次盘。
  const preview = useRef<number | null>(null)

  // 后端快照回来了就撤掉本地预览：保存成功时两个值相等（撤不撤视觉效果一样），
  // 保存失败时后端还是旧值，这里必须退回旧值，不能让界面停在没落盘成功的宽度上。
  useEffect(() => {
    if (dragging || liveWidth === null || width === null) return
    if (width !== liveWidth) {
      preview.current = null
      setLiveWidth(null)
    }
  }, [dragging, liveWidth, width])

  const applyDelta = useCallback(() => {
    rafPending.current = false
    const state = drag.current
    if (!state) return
    const next = clampSidebarWidth(state.startWidth + (state.lastX - state.startX))
    preview.current = next
    setLiveWidth(next)
  }, [])

  const onPointerDown = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (event.button !== 0) return
      event.preventDefault()
      const element = sidebarRef.current
      const startWidth = clampSidebarWidth(element ? element.offsetWidth : (width ?? SIDEBAR_MIN_WIDTH))
      drag.current = { pointerId: event.pointerId, startX: event.clientX, startWidth, lastX: event.clientX }
      preview.current = startWidth
      // 抓住指针后，鼠标移出分隔条甚至移出窗口都还能继续拖，松手或抓取丢失都会走 onPointerUp。
      event.currentTarget.setPointerCapture(event.pointerId)
      setDragging(true)
      setLiveWidth(startWidth)
    },
    [width],
  )

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      state.lastX = event.clientX
      if (rafPending.current) return
      rafPending.current = true
      requestAnimationFrame(applyDelta)
    },
    [applyDelta],
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
      onCommit(preview.current ?? state.startWidth)
    },
    [onCommit],
  )

  return { dragging, liveWidth, sidebarRef, onPointerDown, onPointerMove, onPointerUp }
}
