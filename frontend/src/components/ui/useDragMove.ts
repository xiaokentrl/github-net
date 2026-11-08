import { useCallback, useRef, useState, type RefObject, type PointerEvent as ReactPointerEvent } from 'react'

/** 界面缩放是给 body 设 CSS zoom：指针坐标是视口像素，元素长度是缩放前的 CSS 像素，
    拖动换算要先除回来（和公共 Dialog 同一套口径）。 */
function bodyZoom(): number {
  const raw = parseFloat(getComputedStyle(document.body).zoom || '1')
  return Number.isFinite(raw) && raw > 0 ? raw : 1
}

/**
 * 轻量拖动 hook（拍板 2026-11-08：设置中心这类自绘弹框也要能拖）。
 * 面板本身用 flex 居中，拖动只记一个位移 offset，渲染时 transform 平移——
 * 不碰布局坐标，也不跟居中逻辑打架。公共 Dialog 是「拖＋缩放」的完整版；
 * 这里只管拖：头部按住拖动、位移夹在「面板不出窗口」内、双击头部回居中。
 */
export function useDragMove(panelRef: RefObject<HTMLElement | null>, edgeGap = 8) {
  const [offset, setOffset] = useState({ x: 0, y: 0 })
  const [dragging, setDragging] = useState(false)
  const drag = useRef<{ pointerId: number; startX: number; startY: number; baseX: number; baseY: number } | null>(null)
  const offsetRef = useRef(offset)
  offsetRef.current = offset
  const rafPending = useRef(false)
  const pending = useRef<{ x: number; y: number } | null>(null)

  const clamp = useCallback(
    (next: { x: number; y: number }) => {
      const el = panelRef.current
      const zoom = bodyZoom()
      const viewWidth = window.innerWidth / zoom
      const viewHeight = window.innerHeight / zoom
      const w = el?.offsetWidth ?? 0
      const h = el?.offsetHeight ?? 0
      // 面板由 flex 居中：中心点在 (view-w)/2。位移的允许范围＝面板四边不越出窗口留白。
      const centerX = (viewWidth - w) / 2
      const centerY = (viewHeight - h) / 2
      const minX = Math.min(0, edgeGap - centerX)
      const maxX = Math.max(0, viewWidth - w - edgeGap - centerX)
      const minY = Math.min(0, edgeGap - centerY)
      const maxY = Math.max(0, viewHeight - h - edgeGap - centerY)
      return {
        x: Math.min(Math.max(next.x, minX), maxX),
        y: Math.min(Math.max(next.y, minY), maxY),
      }
    },
    [panelRef, edgeGap],
  )

  const start = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (event.button !== 0) return
      // 头上还有真按钮（关闭这类）：点在按钮上不算拖动。
      if ((event.target as HTMLElement).closest('button')) return
      event.preventDefault()
      drag.current = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        baseX: offsetRef.current.x,
        baseY: offsetRef.current.y,
      }
      event.currentTarget.setPointerCapture(event.pointerId)
      setDragging(true)
    },
    [],
  )

  const move = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      const zoom = bodyZoom()
      pending.current = clamp({
        x: state.baseX + (event.clientX - state.startX) / zoom,
        y: state.baseY + (event.clientY - state.startY) / zoom,
      })
      if (rafPending.current) return
      rafPending.current = true
      requestAnimationFrame(() => {
        rafPending.current = false
        const next = pending.current
        if (!next) return
        pending.current = null
        offsetRef.current = next
        setOffset(next)
      })
    },
    [clamp],
  )

  const up = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const state = drag.current
    if (!state || state.pointerId !== event.pointerId) return
    drag.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    setDragging(false)
  }, [])

  const reset = useCallback(() => setOffset({ x: 0, y: 0 }), [])

  return { offset, dragging, start, move, up, reset }
}
