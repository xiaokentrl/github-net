import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { t } from '../../i18n'
import styles from './Dialog.module.css'

/** 默认尺寸与边界：面板未拖出高度时按内容撑，封顶窗口的 70%。 */
const WIDTH_DEFAULT = 440
const WIDTH_MIN = 260
const HEIGHT_MIN = 120
const HEIGHT_MAX_RATIO = 0.7
const EDGE_GAP = 8

/** 界面缩放是给 body 设 CSS zoom（App.tsx 的 applyZoom）：指针坐标是视口像素，
    元素里的长度是缩放前的 CSS 像素，两者差一个倍数，拖动换算要先除回来。 */
function bodyZoom(): number {
  const raw = parseFloat(getComputedStyle(document.body).zoom || '1')
  return Number.isFinite(raw) && raw > 0 ? raw : 1
}

/** 面板几何：left/top 是面板左上角离窗口左上的 CSS 像素；height 为 null＝按内容撑。 */
type Geom = { left: number; top: number; width: number; height: number | null }

/** 夹回可视区：宽高压在最小值和「窗口减去留白」之间，位置压在窗口内不出界。 */
function clampGeom(next: Geom, panel: HTMLElement | null): Geom {
  const zoom = bodyZoom()
  const viewWidth = window.innerWidth / zoom
  const viewHeight = window.innerHeight / zoom
  const width = Math.min(Math.max(WIDTH_MIN, next.width), Math.max(WIDTH_MIN, viewWidth - EDGE_GAP * 2))
  const height =
    next.height === null
      ? null
      : Math.min(Math.max(HEIGHT_MIN, next.height), Math.max(HEIGHT_MIN, Math.min(viewHeight * HEIGHT_MAX_RATIO, viewHeight - EDGE_GAP * 2)))
  const realHeight = height ?? panel?.offsetHeight ?? HEIGHT_MIN
  const maxLeft = Math.max(EDGE_GAP, viewWidth - width - EDGE_GAP)
  const maxTop = Math.max(EDGE_GAP, viewHeight - realHeight - EDGE_GAP)
  return {
    left: Math.min(Math.max(EDGE_GAP, next.left), maxLeft),
    top: Math.min(Math.max(EDGE_GAP, next.top), maxTop),
    width,
    height,
  }
}

/** 一次按下的起点：base 是按下时的整份几何，baseHeight 是量好的真实高度（内容撑的面板没有高度数字）。 */
type DragState = { pointerId: number; startX: number; startY: number; base: Geom; baseHeight: number; mode: DragMode }
type DragMode = 'move' | 'east' | 'south' | 'southeast'

/**
 * 按位移算新几何：
 *   move —— 整块跟着走（left/top 加位移）；
 *   east —— 只变宽；south —— 只变高；southeast —— 宽高一起。位置不动。
 */
function dragGeom(state: DragState, dx: number, dy: number): Geom {
  const { base, baseHeight, mode } = state
  switch (mode) {
    case 'move':
      return { left: base.left + dx, top: base.top + dy, width: base.width, height: base.height }
    case 'east':
      return { ...base, width: base.width + dx }
    case 'south':
      return { ...base, height: (base.height ?? baseHeight) + dy }
    case 'southeast':
      return { ...base, width: base.width + dx, height: (base.height ?? baseHeight) + dy }
  }
}

/**
 * 公共弹框（拍板 2026-11-03）：遮罩层 + 可拖动标题栏 + 右/下/右下三个方向调整大小 +
 * 右上角关闭按钮。位置和大小只存组件状态（本次运行有效，不落配置，重开回居中默认）。
 * 纯容器：不认识任何业务，文字走 props，动作全由调用方塞进 children/footer。
 */
export function Dialog({
  title,
  onClose,
  children,
  footer,
  width = WIDTH_DEFAULT,
  height = null,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  /** 底部动作区（确认/取消这类按钮），可选。 */
  footer?: ReactNode
  width?: number
  /** 初始高度；null＝按内容撑（封顶窗口 70%）。拖过之后就是固定像素。 */
  height?: number | null
}) {
  const panelRef = useRef<HTMLElement | null>(null)
  const [geom, setGeom] = useState<Geom>({ left: 0, top: 0, width, height })
  const [dragging, setDragging] = useState(false)
  const drag = useRef<DragState | null>(null)
  const geomRef = useRef(geom)
  geomRef.current = geom
  const rafPending = useRef(false)
  const pendingGeom = useRef<Geom | null>(null)

  // 挂载即居中：量一次真实宽高算 left/top（CSS 居中没法和拖动位移共用一套坐标系）。
  useLayoutEffect(() => {
    const el = panelRef.current
    if (!el) return
    const zoom = bodyZoom()
    const w = el.offsetWidth
    const h = el.offsetHeight
    const left = Math.max(EDGE_GAP, (window.innerWidth / zoom - w) / 2)
    const top = Math.max(EDGE_GAP, (window.innerHeight / zoom - h) / 2)
    setGeom((current) => clampGeom({ ...current, left, top }, el))
  }, [])

  // Esc 关闭听在 document 上：焦点没进弹框也能退。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const flushDrag = useCallback(() => {
    rafPending.current = false
    const next = pendingGeom.current
    if (!next) return
    pendingGeom.current = null
    geomRef.current = next
    setGeom(next)
  }, [])

  const startDrag = useCallback(
    (mode: DragMode) => (event: ReactPointerEvent<HTMLElement>) => {
      if (event.button !== 0) return
      // 关闭按钮也在标题栏这一条里：点在按钮上就不算拖动。
      if ((event.target as HTMLElement).closest('button')) return
      event.preventDefault()
      const panel = panelRef.current
      const base = { ...geomRef.current, height: mode === 'move' ? geomRef.current.height : geomRef.current.height ?? panel?.offsetHeight ?? HEIGHT_MIN }
      drag.current = { pointerId: event.pointerId, startX: event.clientX, startY: event.clientY, base, baseHeight: base.height ?? HEIGHT_MIN, mode }
      event.currentTarget.setPointerCapture(event.pointerId)
      setDragging(true)
    },
    [],
  )

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      const zoom = bodyZoom()
      const dx = (event.clientX - state.startX) / zoom
      const dy = (event.clientY - state.startY) / zoom
      pendingGeom.current = clampGeom(dragGeom(state, dx, dy), panelRef.current)
      if (rafPending.current) return
      rafPending.current = true
      requestAnimationFrame(flushDrag)
    },
    [flushDrag],
  )

  const onPointerUp = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      drag.current = null
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
      setDragging(false)
    },
    [],
  )

  const style: CSSProperties = {
    left: `${geom.left}px`,
    top: `${geom.top}px`,
    width: `${geom.width}px`,
    ...(geom.height === null ? {} : { height: `${geom.height}px` }),
  }

  return (
    <div
      className={styles.mask}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <section
        ref={panelRef}
        className={`${styles.panel} ${dragging ? styles.panelDragging : ''}`}
        style={style}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <div
          className={styles.header}
          onPointerDown={startDrag('move')}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onDoubleClick={() => {
            // 双击标题栏：回到居中（尺寸不动），和 Toast 双击回右下角同一口决。
            const el = panelRef.current
            if (!el) return
            const zoom = bodyZoom()
            setGeom((current) =>
              clampGeom({ ...current, left: (window.innerWidth / zoom - current.width) / 2, top: (window.innerHeight / zoom - (current.height ?? el.offsetHeight)) / 2 }, el),
            )
          }}
        >
          <span className={styles.title}>{title}</span>
          <span className={styles.spacer} />
          <button type="button" className={styles.closeBtn} onClick={onClose} title={t('ui.dialog.close')} aria-label={t('ui.dialog.close')}>
            ✕
          </button>
        </div>
        <div className={styles.body}>{children}</div>
        {footer ? <div className={styles.footer}>{footer}</div> : null}
        <span className={`${styles.grip} ${styles.gripE}`} onPointerDown={startDrag('east')} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp} />
        <span className={`${styles.grip} ${styles.gripS}`} onPointerDown={startDrag('south')} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp} />
        <span className={`${styles.grip} ${styles.gripSE}`} onPointerDown={startDrag('southeast')} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp} />
      </section>
    </div>
  )
}
