import { t } from '../../i18n'
import { THEMES } from './themes'
import styles from './theme.module.css'

// 主题选择网格：点一张卡立刻切换并保存，不用重启，也没有「选择主题…」按钮（原型需求②就是这个形状）。
// 这里只管呈现和点击，不判断哪些主题能用——能用的是哪几个由后端配置校验说了算。
export function ThemeGrid({
  current,
  disabled,
  onSelect,
}: {
  current: string
  disabled?: boolean
  onSelect: (id: string) => void
}) {
  return (
    <div className={styles.grid} role="group" aria-label={t('theme.grid.label')}>
      {THEMES.map((theme) => (
        <button
          key={theme.id}
          type="button"
          className={styles.card}
          aria-pressed={theme.id === current}
          disabled={disabled}
          onClick={() => onSelect(theme.id)}
        >
          <span className={styles.preview} aria-hidden="true">
            <span className={`${styles.swatch} ${styles.swatchBg}`} style={{ background: theme.swatches[0] }} />
            <span className={styles.swatch} style={{ background: theme.swatches[1] }} />
            <span className={styles.swatch} style={{ background: theme.swatches[2] }} />
            <span className={styles.swatch} style={{ background: theme.swatches[3] }} />
          </span>
          <span className={styles.name}>{t(`theme.${theme.id}.name`)}</span>
          <span className={styles.desc}>{t(`theme.${theme.id}.desc`)}</span>
        </button>
      ))}
    </div>
  )
}
