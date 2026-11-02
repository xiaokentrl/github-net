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
/** 面板宽度：同上，样式里用变量拿这一份。 */
const PANEL_WIDTH = 360
/** 拖到边上时至少留这一点，免得面板被推出可视区、只剩一条边能抓。 */
const EDGE_GAP = 4

type Offset = { right: number; bottom: number }

function bodyZoom(): number {
  // 界面缩放是给 body 设 CSS zoom（App.tsx 的 applyZoom），缩放进这一层后
  // 指针坐标还是视口像素、元素里的长度是缩放前的 CSS 像素，两者差一个倍数。
  const raw = parseFloat(getComputedStyle(document.body).zoom || '1')
  return Number.isFinite(raw) && raw > 0 ? raw : 1
}

/** 把位置夹回可视区：参数和返回值都在缩放前的 CSS 像素这一层，视口尺寸先除回这一层。 */
function clampOffset(right: number, bottom: number, panel: HTMLElement | null): Offset {
  const zoom = bodyZoom()
  const viewWidth = window.innerWidth / zoom
  const viewHeight = window.innerHeight / zoom
  const width = panel?.offsetWidth ?? PANEL_WIDTH
  const height = panel?.offsetHeight ?? 96
  const maxRight = Math.max(EDGE_GAP, viewWidth - width - EDGE_GAP)
  const maxBottom = Math.max(EDGE_GAP, viewHeight - height - EDGE_GAP)
  return {
    right: Math.min(Math.max(EDGE_GAP, right), maxRight),
    bottom: Math.min(Math.max(EDGE_GAP, bottom), maxBottom),
  }
}

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
  const [offset, setOffset] = useState<Offset>({ right: RIGHT_DEFAULT, bottom: BOTTOM_DEFAULT })
  const [dragging, setDragging] = useState(false)
  // 刚点过复制的那条：按钮文案临时换成「已复制」，1.2 秒后回去。
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const drag = useRef<{ pointerId: number; startX: number; startY: number; startRight: number; startBottom: number } | null>(null)
  const offsetRef = useRef(offset)
  offsetRef.current = offset
  const rafPending = useRef(false)
  const pendingOffset = useRef<Offset | null>(null)

  // 窗口变小或改了缩放档位，原来那个位置可能已经出去了一半，这里夹回来。
  useEffect(() => {
    const reconcile = () => {
      const next = clampOffset(offsetRef.current.right, offsetRef.current.bottom, panelRef.current)
      if (next.right !== offsetRef.current.right || next.bottom !== offsetRef.current.bottom) {
        offsetRef.current = next
        setOffset(next)
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
    const next = pendingOffset.current
    if (!next) return
    pendingOffset.current = null
    offsetRef.current = next
    setOffset(next)
  }, [])

  const onPointerDown = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    // 头部那两颗按钮也在这一条里：点在按钮上就不算拖动。
    if ((event.target as HTMLElement).closest('button')) return
    event.preventDefault()
    const start = offsetRef.current
    drag.current = { pointerId: event.pointerId, startX: event.clientX, startY: event.clientY, startRight: start.right, startBottom: start.bottom }
    event.currentTarget.setPointerCapture(event.pointerId)
    setDragging(true)
  }, [])

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const state = drag.current
      if (!state || state.pointerId !== event.pointerId) return
      const zoom = bodyZoom()
      // 往右下拖＝离右边和底边更近，所以两个方向都是减。
      pendingOffset.current = clampOffset(
        state.startRight - (event.clientX - state.startX) / zoom,
        state.startBottom - (event.clientY - state.startY) / zoom,
        panelRef.current,
      )
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

  const backToCorner = useCallback(() => {
    const next = { right: RIGHT_DEFAULT, bottom: BOTTOM_DEFAULT }
    offsetRef.current = next
    setOffset(next)
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

  if (notices.length === 0) return null

  const rootStyle = {
    '--toast-right': `${offset.right}px`,
    '--toast-bottom': `${offset.bottom}px`,
    '--toast-width': `${PANEL_WIDTH}px`,
  } as CSSProperties

  if (collapsed) {
    return (
      <div className={styles.root} style={rootStyle}>
        <button type="button" className={styles.pill} onClick={toggleNoticeCollapsed} aria-expanded={false} title={t('ui.toast.expand')} aria-label={t('ui.toast.expand')}>
          <span className={styles.pillText}>{t('ui.toast.title')}</span>
          <span className={styles.pillCount}>{notices.length}</span>
          <span className={styles.pillChevron} aria-hidden="true">
            ▴
          </span>
        </button>
      </div>
    )
  }

  return (
    <div className={styles.root} style={rootStyle}>
      <section ref={panelRef} className={styles.panel} aria-label={t('ui.toast.title')}>
        <div
          className={`${styles.header} ${dragging ? styles.headerDragging : ''}`}
          title={t('ui.toast.dragHint')}
          onPointerDown={onPointerDown}
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
            onClick={toggleNoticeCollapsed}
            aria-expanded={true}
            title={t('ui.toast.collapse')}
            aria-label={t('ui.toast.collapse')}
          >
            ▾
          </button>
        </div>
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
      </section>
    </div>
  )
}

export default Toast
