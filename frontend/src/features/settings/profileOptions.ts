// 流量档位的文案与数字集中放这一处：工具栏、状态栏、设置页都读它，不各自复制一份。
// candidates 是「每个主机名最多试几个候选 IP」，要和后端 internal/config 的 ProfileCandidateLimit
// 保持一致（2/8/16）；并发数是界面说明用的目标值。改了数字要两边一起改。
import { t } from '../../i18n'

export type ProfileID = 'low' | 'standard' | 'full'

export type ProfileSpec = { candidates: number; concurrency: number }

export const PROFILE_IDS: ProfileID[] = ['low', 'standard', 'full']

export const PROFILE_SPECS: Record<ProfileID, ProfileSpec> = {
  low: { candidates: 2, concurrency: 2 },
  standard: { candidates: 8, concurrency: 8 },
  full: { candidates: 16, concurrency: 8 },
}

// 后端只认这三个值；万一拿到别的值，原样显示出来，不偷偷换成某一档。
export function profileLabel(id: string): string {
  return PROFILE_IDS.includes(id as ProfileID) ? t(`toolbar.profile.${id}`) : id
}

export function profileSpec(id: string): ProfileSpec {
  return PROFILE_SPECS[id as ProfileID] ?? PROFILE_SPECS.low
}

export function profileNote(id: string): string {
  if (!PROFILE_IDS.includes(id as ProfileID)) return t('toolbar.profile.unknown')
  const spec = profileSpec(id)
  return t(`toolbar.profile.note.${id}`, { candidates: spec.candidates, concurrency: spec.concurrency })
}

// 设置页三张卡片上那行硬数字。
export function profileLimitText(id: string): string {
  const spec = profileSpec(id)
  return t('settings.network.card.limit', { candidates: spec.candidates, concurrency: spec.concurrency })
}
