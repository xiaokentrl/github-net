// 队列左栏「任务列表」的宽度规则，前端唯一来源。
// 两个死数必须和后端 internal/config 的 QueueListWidthMin/Max 逐个相等，
// 这条相等断言在 internal/config/layout_bounds_parity_test.go：抄错一位的话，
// 界面拖到范围外会写进配置、后端判非法、界面回读旧值，用户看到的是「拖不动」。
export const QUEUE_LIST_MIN_WIDTH = 150
export const QUEUE_LIST_STATIC_MAX_WIDTH = 1200

// 真正的上限跟着队列区宽度走：右边明细至少留过半，否则明细被挤成一列没法看。
// 原型同一条规则（calcQueueListMaxW）取 45%，这里也照它。
export const QUEUE_LIST_MAX_RATIO = 0.45

// queueListMaxWidth 按队列区实测宽度算动态上限；量不到（首帧、面板还没摆好）时退回静态上限，
// 不能算出 0 把左栏压没。
export function queueListMaxWidth(bodyWidth: number): number {
  if (!Number.isFinite(bodyWidth) || bodyWidth <= 0) return QUEUE_LIST_STATIC_MAX_WIDTH
  return Math.max(QUEUE_LIST_MIN_WIDTH, Math.min(QUEUE_LIST_STATIC_MAX_WIDTH, Math.floor(bodyWidth * QUEUE_LIST_MAX_RATIO)))
}

export function clampQueueListWidth(value: number, bodyWidth: number): number {
  return Math.min(queueListMaxWidth(bodyWidth), Math.max(QUEUE_LIST_MIN_WIDTH, Math.round(value)))
}
