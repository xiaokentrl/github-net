import { useColumnResize } from './useColumnResize'
import type { ColumnResize } from './useColumnResize'

// 侧栏可拖范围与后端 internal/config 的 SidebarWidthMin/SidebarWidthMax 必须逐个相等，
// 这条相等断言在 internal/config/layout_bounds_parity_test.go。改这里不改后端会被门拦下。
export const SIDEBAR_MIN_WIDTH = 200
export const SIDEBAR_MAX_WIDTH = 600

export function clampSidebarWidth(value: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(value)))
}

// 侧栏 = 通用拖动条 + 这一对固定上下限。
export function useSidebarResize(width: number | null, onCommit: (width: number) => void): ColumnResize {
  return useColumnResize(width, onCommit, clampSidebarWidth, SIDEBAR_MIN_WIDTH)
}
