import { useCallback, useEffect, useRef } from 'react'

/**
 * 统一失效入口（拍板 2026-11-09，对照《统一同步层技术方案》）。
 *
 * 架构定位：「事件驱动失效 + 兜底轮询」是有意选择的简化架构——推（任务事件）管快，
 * 拉（权威回读：loadCatalog/refreshResult）管准，兜底（延迟重刷）管不漏。
 * 本模块只做一件事：把「谁该刷什么」从散落的调用点收进一张登记表，
 * 命令落定后只声明失效哪些事实，刷新动作由登记表转发，遗漏变成可审计的缺口。
 *
 * 残余陈旧窗口（方案要求写明）：失效信号丢失或比落盘晚到时，镜像陈旧不超过
 * 兜底延迟（800ms～1.2s）；焦点恢复兜底再刷一轮，外部改动（手改 hosts 文件）
 * 的感知上限是「下一次任何任务落定或窗口聚焦」。
 *
 * 纪律（方案原文的三条）：
 * - 唯一入口：业务代码不再直接拼刷新组合，全部走 invalidate；
 * - 幂等：刷新动作全是权威回读，重复调用无副作用；同拍多次失效合并成一次；
 * - 从宽：宁可多失效一个相邻事实，不为省一次重取留一个陈旧点。
 */

/** 事实清单（方案第 3 问的答案，改事实清单先改这里）：
 *  - catalog  —— 目录 + 逐项 Hosts 状态（GetItems 现读），树/详情页/工具栏都在读
 *  - settings —— 配置快照，全部设置卡与行为开关在读
 *  - status   —— 应用状态（DNS 检测就绪、配置路径）
 *  - automation —— 启动自动化链的当前结论
 *  - results  —— 检测结论 store（可整体失效，或按 itemId 逐项失效） */
export type SyncFact = 'catalog' | 'settings' | 'status' | 'automation' | 'results'

type Refresher = (itemIds?: string[]) => void | Promise<void>

const refreshers = new Map<SyncFact, Refresher>()
const pendingTimers = new Map<string, number>()

/** 注册某事实的权威刷新函数（App 挂载时注册一次）；返回注销函数。 */
export function registerRefresher(fact: SyncFact, fn: Refresher): () => void {
  refreshers.set(fact, fn)
  return () => {
    if (refreshers.get(fact) === fn) refreshers.delete(fact)
  }
}

/** 同拍多次失效合并：0ms 定时器去抖，避免一个命令连环触发同一事实刷三遍。 */
function fire(fact: SyncFact, itemIds?: string[]) {
  const key = itemIds && itemIds.length > 0 ? `${fact}#${[...itemIds].sort().join(',')}` : fact
  if (pendingTimers.has(key)) return
  const timer = window.setTimeout(() => {
    pendingTimers.delete(key)
    const fn = refreshers.get(fact)
    if (!fn) return // 未注册（应用引导期）就忽略：此刻界面还没建，不存在陈旧镜像
    void fn(itemIds)
  }, 0)
  pendingTimers.set(key, timer)
}

/** 延迟兜底重刷：给「失效信号比落盘晚到」「级联变化后到」一个追上来的窗口。
 *  同 key 的兜底窗口去重：事件路和轮询路同时落定也只排一个兜底。 */
const fallbackTimers = new Map<string, number>()
function fireFallback(fact: SyncFact, itemIds: string[] | undefined, delayMs: number) {
  const key = `${fact}#${itemIds && itemIds.length > 0 ? [...itemIds].sort().join(',') : '*'}`
  if (fallbackTimers.has(key)) return
  const timer = window.setTimeout(() => {
    fallbackTimers.delete(key)
    fire(fact, itemIds)
  }, delayMs)
  fallbackTimers.set(key, timer)
}

/** 唯一失效入口：命令落定/生命周期事件只声明失效哪些事实，不自己拼刷新。
 *  opts.itemIds —— results 事实按项目失效；缺省＝整体重拉。
 *  opts.fallbackDelayMs —— 安排一次延迟重刷（落定即刻 + 延迟兜底的两拍）。 */
export function invalidate(fact: SyncFact, opts?: { itemIds?: string[]; fallbackDelayMs?: number }) {
  fire(fact, opts?.itemIds)
  if (opts?.fallbackDelayMs) fireFallback(fact, opts?.itemIds, opts.fallbackDelayMs)
}

/** 焦点恢复失效（方案生命周期第 1 条）：切回窗口补一轮，外部改动与休眠唤醒
 *  的残余陈旧在这里收敛。2 秒节流：弹框开合不会 blur/flicker 主窗口，但保险起见。 */
export function useFocusInvalidation() {
  const lastRef = useRef(0)
  const onFocus = useCallback(() => {
    const now = Date.now()
    if (now - lastRef.current < 2000) return
    lastRef.current = now
    invalidate('catalog')
    invalidate('results', { fallbackDelayMs: 1200 })
  }, [])
  useEffect(() => {
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [onFocus])
}
