// 流量档位的文案集中放这一处：工具栏、状态栏和后面的设置页都读它，不各自复制一份。
// 档位取值必须和后端 internal/config 的 ProfileLow/Standard/Full 一致，改了要两边一起改。
import { t } from '../../i18n'

export type ProfileID = 'low' | 'standard' | 'full'

export const PROFILE_IDS: ProfileID[] = ['low', 'standard', 'full']

// 后端只认这三个值；万一拿到别的值，原样显示出来，不偷偷换成某一档。
export function profileLabel(id: string): string {
  return PROFILE_IDS.includes(id as ProfileID) ? t(`toolbar.profile.${id}`) : id
}

export function profileNote(id: string): string {
  return PROFILE_IDS.includes(id as ProfileID) ? t(`toolbar.profile.note.${id}`) : t('toolbar.profile.unknown')
}
