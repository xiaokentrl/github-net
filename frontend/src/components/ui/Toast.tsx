import { useCallback, useEffect, useRef, useState } from 'react'
import type { CSSProperties, PointerEvent as ReactPointerEvent } from 'react'
import { clearNotices, dismissNotice, pushNotice, toggleNoticeCollapsed, useNoticeCollapsed, useNotices } from './toastStore'
import type { NoticeKind } from './toastStore'
import { t } from '../../i18n'
import styles from './Toast.module.css'

// 公共提示面板：贴在应用窗口右下角，浮在内容之上。窗口移动它跟着走（它就在窗口自己的画面里，
// 用 fixed 定位，不需要问系统窗口在哪）。
// 一条提示由「哪一档 + 文字 + 这几个动作」组成，规则都在 toastStore 里：
// 这里只负责画出来和把按钮接回去，不认识检测、Hosts 这些业务。
// 原型那条 #toast 是居中、单条、几秒就淡掉的提示条，和这里不是一件事，
// 所以面板底用的是浮层那套令牌（--ctl/--border-strong/--pop），和设置对话框同一层长相。

/** 默认贴边距离：CSS 和夹取计算都读这两个数，不另在样式里写第二份。 */
const RIGHT_DEFAULT = 16
const BOTTOM_DEFAULT = 40
/** 面板默认宽度：样式里用变量拿这一份。高度没有默认数字——没拖过就跟着内容撑，封顶 45vh。 */
const WIDTH_DEFAULT = 360
/** 还没拖出高度时用来夹位置的经验值（一两条提示的高度）。 */
const HEIGHT_FALLBACK = 96
/** 拖到边上时至少留这一点，免得面板被推出可视区、只剩一条边能抓。 */
const EDGE_GAP = 4
/** 最小宽高（拍板 2026-11-02）：再小就装不下一行「档位＋文字＋两颗钮」，也按不出头部那排按钮。 */
const WIDTH_MIN = 260
const HEIGHT_MIN = 120

/** 面板的位置和大小：贴右下锚定，所以「右边/底边离窗口多远」+ 宽高。height 为 null 是「跟着内容撑」。 */
type Geom = { right: number; bottom: number; width: number; height: number | null }
/** 这一次按下去抓的是哪几条边：全 false 就是整块移动，四角是相邻两条边同时为真。 */
type Edges = { left: boolean; right: boolean; top: boolean; bottom: boolean }

function bodyZoom(): number {
  // 界面缩放是给 body 设 CSS zoom（App.tsx 的 applyZoom），缩放进这一层后
  // 指针坐标还是视口像素、元素里的长度是缩放前的 CSS 像素，两者差一个倍数。
  const raw = parseFloat(getComputedStyle(document.body).zoom || '1')
  return Number.isFinite(raw) && raw > 0 ? raw : 1
}

/**
 * 把位置和大小夹回可视区：参数和返回值都在缩放前的 CSS 像素这一层，视口尺寸先除回这一层。
 * 宽高压在最小值和「窗口减去两边留白」之间；夹完宽高再夹贴边距离，
 * 顺序反了会出现面板已经贴边、宽高又把它顶出去的状态。
 */
function clampGeom(next: Geom, panel: HTMLElement | null): Geom {
  const zoom = bodyZoom()
  const viewWidth = window.innerWidth / zoom
  const viewHeight = window.innerHeight / zoom
  const width = Math.min(Math.max(WIDTH_MIN, next.width), Math.max(WIDTH_MIN, viewWidth - EDGE_GAP * 2))
  const height =
    next.height === null
      ? null
      : Math.min(Math.max(HEIGHT_MIN, next.height), Math.max(HEIGHT_MIN, viewHeight - EDGE_GAP * 2))
  const realHeight = height ?? panel?.offsetHeight ?? HEIGHT_FALLBACK
  const maxRight = Math.max(EDGE_GAP, viewWidth - width - EDGE_GAP)
  const maxBottom = Math.max(EDGE_GAP, viewHeight - realHeight - EDGE_GAP)
  return {
    right: Math.min(Math.max(EDGE_GAP, next.right), maxRight),
    bottom: Math.min(Math.max(EDGE_GAP, next.bottom), maxBottom),
    width,
    height,
  }
}

/** 一次拖动的起点：按下时的整份几何 + 抓的哪几条边。baseHeight 是量好的起点高度，用于面板还没拖出高度数字的情况。 */
type DragState = { pointerId: number; startX: number; startY: number; base: Geom; baseHeight: number; edges: Edges }

/**
 * 按指针位移算出新的位置和宽高。面板贴右下锚定，所以：
 * 抓左边＝左边跟着走、右边不动（宽增减 dx 的反号，贴边距离不变）；
 * 抓右边＝右边跟着走、左边不动（宽 +dx，同时贴右距离 -dx，否则左边会被一起推走）；
 * 上边、下边同理，只是换成高度和贴底距离。四角就是相邻两条边的算法各算一遍。
 */
function dragGeom(state: DragState, dx: number, dy: number): Geom {
  const { base, baseHeight, edges } = state
  if (!edges.left && !edges.right && !edges.top && !edges.bottom) {
    return { right: base.right - dx, bottom: base.bottom - dy, width: base.width, height: base.height }
  }
  let { right, bottom, width } = base
  let height = base.height
  if (edges.left) width = base.width - dx
  if (edges.right) {
    width = base.width + dx
    right = base.right - dx
  }
  if (edges.top) height = baseHeight - dy
  if (edges.bottom) {
    height = baseHeight + dy
    bottom = base.bottom - dy
  }
  return { right, bottom, width, height }
}

/** 抓头部整块移动：四条边都不带。 */
const MOVE: Edges = { left: false, right: false, top: false, bottom: false }

/** 八条手柄：四条边 + 四个角，形状和鼠标指针在 CSS 里，这里只说每条抓哪几条边。 */
const HANDLES: { key: string; cls: string; edges: Edges }[] = [
  { key: 'w', cls: styles.handleW, edges: { left: true, right: false, top: false, bottom: false } },
  { key: 'e', cls: styles.handleE, edges: { left: false, right: true, top: false, bottom: false } },
  { key: 'n', cls: styles.handleN, edges: { left: false, right: false, top: true, bottom: false } },
  { key: 's', cls: styles.handleS, edges: { left: false, right: false, top: false, bottom: true } },
  { key: 'nw', cls: styles.handleNW, edges: { left: true, right: false, top: true, bottom: false } },
  { key: 'ne', cls: styles.handleNE, edges: { left: false, right: true, top: true, bottom: false } },
  { key: 'sw', cls: styles.handleSW, edges: { left: true, right: false, top: false, bottom: true } },
  { key: 'se', cls: styles.handleSE, edges: { left: false, right: true, top: false, bottom: true } },
]

// 复制：先走标准剪贴板 API，被拒或没有这条路时退到选中再复制。
// 两条都不成才报失败——复制这种事静默失败比报错更坑人。
async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // WebKit 在没有用户手势或页面不在安全上下文时会拒；继续试兜底那条。
  }
  try {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.top = '0'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(area)
    return ok
  } catch {
    return false
  }
}

/** 档位说法在渲染时现取：写在模块常量里会把语言冻在第一次求值那一份。 */
function kindText(kind: NoticeKind): string {
  if (kind === 'error') return t('ui.toast.kind.error')
  if (kind === 'warning') return t('ui.toast.kind.warning')
  if (kind === 'success') return t('ui.toast.kind.success')
  return t('ui.toast.kind.info')
}

function kindClass(kind: NoticeKind): string {
  if (kind === 'error') return styles.kindError
  if (kind === 'warning') return styles.kindWarning
  if (kind === 'success') return styles.kindSuccess
  return styles.kindInfo
}

export function Toast() {
  const notices = useNotices()
  const collapsed = useNoticeCollapsed()
  const panelRef = useRef<HTMLDivElement | null>(null)
  // 位置和宽高都只存在组件状态里（拍板 2026-11-02：宽高不落配置，重开程序回到默认）。
  const [geom, setGeom] = useState<Geom>({ right: RIGHT_DEFAULT, bottom: BOTTOM_DEFAULT, width: WIDTH_DEFAULT, height: null })
  const [dragging, setDragging] = useState(false)
  // 刚点过复制的那条：按钮文案临时换成「已复制」，1.2 秒后回去。
  const [copiedId, setCopiedId] = useState<number | null>(null)
  // 零条提示时点开小按钮：只用组件内的这个开关，不去动 store 里的收起状态——
  // 那一位管的是「有提示时面板开还是关」，下一批提示来了该按用户上次的选择出现。
  const [emptyOpen, setEmptyOpen] = useState(false)
  const drag = useRef<DragState | null>(null)
  const geomRef = useRef(geom)
  geomRef.current = geom
  const rafPending = useRef(false)
  const pendingGeom = useRef<Geom | null>(null)

  // 窗口变小或改了缩放档位，原来那个位置和大小可能已经出去了一半，这里夹回来。
  useEffect(() => {
    const reconcile = () => {
      const next = clampGeom(geomRef.current, panelRef.current)
      if (next.right !== geomRef.current.right || next.bottom !== geomRef.current.bottom || next.width !== geomRef.current.width || next.height !== geomRef.current.height) {
        geomRef.current = next
        setGeom(next)
      }
    }
    window.addEventListener('resize', reconcile)
    return () => window.removeEventListener('resize', reconcile)
  }, [notices.length, collapsed])

  useEffect(() => {
    if (copiedId === null) return
    const timer = setTimeout(() => setCopiedId(null), 1200)
    return () => clearTimeout(timer)
  }, [copiedId])

  const flushDrag = useCallback(() => {
    rafPending.current = false
    const next = pendingGeom.current
    if (!next) return
    pendingGeom.current = null
    geomRef.current = next
    setGeom(next)
  }, [])

  // 一次按下：edges 全 false 是抓头部整块移动，带边的是抓那条边/那个角调宽高。
  const startDrag = useCallback((edges: Edges) => (event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    // 头部那两颗按钮也在这一条里：点在按钮上就不算拖动。
    if ((event.target as HTMLElement).closest('button')) return
    event.preventDefault()
    const panel = panelRef.current
    // 还没拖出过高度时面板是内容撑的，这里量一次当下真实高度当作起点数字。
    const base = { ...geomRef.current, height: edges.top || edges.bottom ? (geomRef.current.height ?? panel?.offsetHeight ?? HEIGHT_FALLBACK) : geomRef.current.height }
    drag.current = { pointerId: event.pointerId, startX: event.clientX, startY: event.clientY, base, baseHeight: base.height ?? HEIGHT_FALLBACK, edges }
    event.currentTarget.setPointerCapture(event.pointerId)
    setDragging(!(edges.left || edges.right || edges.top || edges.bottom))
  }, [])

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      const zoom = bodyZoom()
      // 往右下拖＝离右边和底边更近，所以两个方向都是减。
      pendingGeom.current = clampGeom(dragGeom(state, (event.clientX - state.startX) / zoom, (event.clientY - state.startY) / zoom), panelRef.current)
      if (rafPending.current) return
      rafPending.current = true
      requestAnimationFrame(flushDrag)
    },
    [flushDrag],
  )

  const onPointerUp = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const state = drag.current
    if (!state || state.pointerId !== event.pointerId) return
    drag.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    setDragging(false)
  }, [])

  // 双击头部：回到右下角，同时把宽高恢复默认（宽高本来就不落配置，这里给一条回默认的路）。
  const backToCorner = useCallback(() => {
    const next: Geom = { right: RIGHT_DEFAULT, bottom: BOTTOM_DEFAULT, width: WIDTH_DEFAULT, height: null }
    geomRef.current = next
    setGeom(next)
  }, [])

  const copyNotice = useCallback((id: number, text: string) => {
    void copyToClipboard(text).then((ok) => {
      if (ok) {
        setCopiedId(id)
        return
      }
      pushNotice('warning', t('ui.toast.copyFailed'))
    })
  }, [])

  // 右下角那颗小按钮常驻：一条提示都没有时也在，点开看到的是「暂无提示」。
  const showPanel = notices.length > 0 ? !collapsed : emptyOpen

  const openPanel = () => {
    if (notices.length > 0) {
      toggleNoticeCollapsed()
      return
    }
    setEmptyOpen(true)
  }

  const closePanel = () => {
    if (notices.length === 0) {
      setEmptyOpen(false)
      return
    }
    toggleNoticeCollapsed()
  }

  // 关闭整块面板（含右下角小按钮）：提示全部清掉，下一条进来才重新出现。
  const hidePanel = () => {
    clearNotices()
    setEmptyOpen(false)
  }

  const rootStyle = {
    '--toast-right': `${geom.right}px`,
    '--toast-bottom': `${geom.bottom}px`,
    '--toast-width': `${geom.width}px`,
    // 没拖过高度就不给这个变量：面板跟着内容撑，列表另有 45vh 上限兜着。
    ...(geom.height === null ? {} : { '--toast-height': `${geom.height}px` }),
  } as CSSProperties

  if (!showPanel) {
    return (
      <div className={`${styles.root} ${styles.rootPill}`} style={rootStyle}>
        <button type="button" className={styles.pill} onClick={openPanel} aria-expanded={false} title={t('ui.toast.expand')} aria-label={t('ui.toast.expand')}>
          <span className={styles.pillText}>{t('ui.toast.title')}</span>
          {notices.length > 0 ? <span className={styles.pillCount}>{notices.length}</span> : null}
          <span className={styles.pillChevron} aria-hidden="true">
            ▴
          </span>
        </button>
      </div>
    )
  }

  return (
    <div className={styles.root} style={rootStyle}>
      <section ref={panelRef} className={`${styles.panel} ${geom.height === null ? '' : styles.panelSized}`} aria-label={t('ui.toast.title')}>
        <div
          className={`${styles.header} ${dragging ? styles.headerDragging : ''}`}
          title={t('ui.toast.dragHint')}
          onPointerDown={startDrag(MOVE)}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onDoubleClick={backToCorner}
        >
          <span className={styles.title}>{t('ui.toast.title')}</span>
          <span className={styles.count}>{t('ui.toast.count', { count: notices.length })}</span>
          <span className={styles.spacer} />
          <button type="button" className={styles.headBtn} onClick={clearNotices} title={t('ui.toast.clear')} aria-label={t('ui.toast.clear')}>
            {t('ui.toast.clear')}
          </button>
          <button
            type="button"
            className={styles.headBtn}
            onClick={closePanel}
            aria-expanded={true}
            title={t('ui.toast.collapse')}
            aria-label={t('ui.toast.collapse')}
          >
            ▾
          </button>
          {/* 关闭整块：清掉全部提示并收起小按钮，下一条提示进来才会再出现。 */}
          <button type="button" className={styles.headBtn} onClick={hidePanel} title={t('ui.toast.closePanel')} aria-label={t('ui.toast.closePanel')}>
            ✕
          </button>
        </div>
        {notices.length === 0 ? (
          <div className={styles.empty}>{t('ui.toast.empty')}</div>
        ) : (
          <ul className={styles.list} aria-live="polite">
            {notices.map((notice) => (
              <li key={notice.id} className={styles.item} role={notice.kind === 'error' ? 'alert' : 'status'}>
                <span className={`${styles.kind} ${kindClass(notice.kind)}`}>{kindText(notice.kind)}</span>
                <span className={styles.text}>{notice.text}</span>
                <span className={styles.actions}>
                  <button
                    type="button"
                    className={styles.itemBtn}
                    onClick={() => copyNotice(notice.id, notice.text)}
                    title={copiedId === notice.id ? t('ui.toast.copied') : t('ui.toast.copy')}
                    aria-label={copiedId === notice.id ? t('ui.toast.copied') : t('ui.toast.copy')}
                  >
                    {copiedId === notice.id ? '✓' : '⧉'}
                  </button>
                  <button
                    type="button"
                    className={styles.itemBtn}
                    onClick={() => dismissNotice(notice.id)}
                    title={t('ui.toast.close')}
                    aria-label={t('ui.toast.close')}
                  >
                    ×
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      {/* 八条调节手柄（拍板 2026-11-02：四个方向加四个角）：贴在面板外沿一圈，
          不遮头部的「清空/收起」和每行的「复制/关闭」，抓哪条就改哪条边对应的宽高。 */}
      {HANDLES.map((handle) => (
        <div
          key={handle.key}
          className={`${styles.handle} ${handle.cls}`}
          title={t('ui.toast.resizeHint')}
          onPointerDown={startDrag(handle.edges)}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
        />
      ))}
    </div>
  )
}

export default Toast
