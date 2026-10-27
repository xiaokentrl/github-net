import { t } from '../../i18n'
import styles from './CategoryTree.module.css'

// 搜索是纯前端筛选：目录只有 54 个项目，不需要问后端。
export function SearchBox({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className={styles.search}>
      <input
        type="search"
        value={value}
        placeholder={t('tree.search.placeholder')}
        aria-label={t('tree.search.placeholder')}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}
