// 队列面板高度的前端唯一来源。这三个死数必须逐个等于后端 internal/config 的
// QueueHeightMin / QueueHeightMax 与 Default() 的队列高度，相等断言在
// internal/config/layout_bounds_parity_test.go；抄错的后果是拖完被后端判非法、界面回读旧值（看着像「拖不动」）。
export const QUEUE_HEIGHT_MIN = 120
export const QUEUE_HEIGHT_STATIC_MAX = 2000
export const QUEUE_HEIGHT_DEFAULT = 252

// 界面上的天花板按窗口高度现算（原型 calcQueueMaxH）：队列最多占到窗口的 70%，
// 而且上面那一整栏（顶栏+工具栏+详情）至少要留 200px，否则详情被压没。
// 量不到窗口高度时退回静态上限，不会算出 0 把面板压平。
export function queueHeightMax(windowHeight: number): number {
  if (windowHeight <= 0) return QUEUE_HEIGHT_STATIC_MAX
  return Math.floor(Math.min(QUEUE_HEIGHT_STATIC_MAX, windowHeight * 0.7, windowHeight - 200))
}

// 夹到「下限优先」：窗口极矮时天花板会低于下限，这时保住下限（和原型一致），
// 否则每次夹都夹出一个比下限还小的值，写盘回读立刻又变回去，界面来回跳。
export function clampQueueHeight(value: number, windowHeight: number): number {
  return Math.round(Math.max(QUEUE_HEIGHT_MIN, Math.min(queueHeightMax(windowHeight), value)))
}
