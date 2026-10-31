import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { Application, Window } from '@wailsio/runtime'
import { THEMES } from '../theme/themes'
import { t } from '../../i18n'
import styles from './TrayPill.module.css'

// 浏览器预览里没有 Wails 运行时，窗口操作会失败；安静接住。原生窗口里这是正常路径。
function runWindowAction(action: () => Promise<void>) {
  action().catch(() => {})
}

// 标题栏上的「托盘胶囊」，等价原型 #trayPill（拍板 2026-10-31）：
// 圆点表示有没有任务在跑（跑着就脉冲），点开是主题色点快切 + 最小化到托盘 + 退出；
// 点胶囊以外的地方收起。主题切换走 App 那条保存链路，和设置中心、系统托盘同一份事实。
export function TrayPill({
  appName,
  theme,
  running,
  onTheme,
}: {
  appName: string
  theme: string
  running: boolean
  onTheme: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  }, [open])

  return (
    <div className={styles.root} ref={rootRef}>
      <button
        type="button"
        className={styles.pill}
        aria-expanded={open}
        title={t('tray.pill.hint')}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`${styles.dot} ${running ? styles.running : ''}`} aria-hidden="true" />
        <span className={styles.label}>{appName}</span>
      </button>
      {open ? (
        <div className={styles.menu} role="menu">
          <div className={styles.dots} role="group" aria-label={t('tray.themes')}>
            {THEMES.map((th) => (
              <button
                key={th.id}
                type="button"
                className={`${styles.themeDot} ${th.id === theme ? styles.active : ''}`}
                style={{ '--dot-bg': th.swatches[0], '--dot-accent': th.swatches[2] } as CSSProperties}
                title={t(`theme.${th.id}.name`)}
                aria-label={t(`theme.${th.id}.name`)}
                onClick={() => {
                  if (th.id !== theme) onTheme(th.id)
                }}
              />
            ))}
          </div>
          <div className={styles.sep} aria-hidden="true" />
          <button
            type="button"
            className={styles.item}
            role="menuitem"
            onClick={() => {
              setOpen(false)
              runWindowAction(() => Window.Hide())
            }}
          >
            {t('tray.minimize')}
          </button>
          <button
            type="button"
            className={styles.item}
            role="menuitem"
            onClick={() => {
              setOpen(false)
              runWindowAction(() => Application.Quit())
            }}
          >
            {t('tray.quit')}
          </button>
        </div>
      ) : null}
    </div>
  )
}
