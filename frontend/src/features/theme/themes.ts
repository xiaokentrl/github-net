import { t } from '../../i18n'

// 六套主题的唯一清单。
// id 必须和后端 internal/config 的 allowedThemes 逐个对上，config 的测试里有相等断言；
// 配色值不在这个文件里，它在 styles/themes.css 按令牌整块覆盖；
// 名字和说明是可见文案，走 i18n，这里只留色块（抄原型 THEMES 第 3760–3766 行的四格预览）。
export type Theme = {
  id: string
  swatches: [string, string, string, string]
}

// 顺序照原型：晨光白、樱花粉、森林绿、深海蓝、午夜蓝、极夜黑。
export const THEMES: Theme[] = [
  { id: 'light', swatches: ['#f6f7f9', '#ffffff', '#3b82f6', '#16a34a'] },
  { id: 'sakura', swatches: ['#fdf7f8', '#ffffff', '#ec4899', '#059669'] },
  { id: 'forest', swatches: ['#0b1512', '#101d18', '#4ade80', '#22c55e'] },
  { id: 'ocean', swatches: ['#080e1a', '#0d1625', '#38bdf8', '#2dd4bf'] },
  { id: 'midnight', swatches: ['#0a0c10', '#11141b', '#5b9cff', '#3dd68c'] },
  { id: 'oled', swatches: ['#000000', '#08090b', '#60a5fa', '#34d399'] },
]

export const THEME_IDS = THEMES.map((theme) => theme.id)

export function themeName(id: string): string {
  return t(`theme.${id}.name`)
}
