import type { CellValue } from "../types";

/**
 * 把生成的 bindings 行类型收窄为非 null 的行数组。
 * Wails 生成的 TS 类型中 rows 为 `(any[] | null)[] | null`——外层与行元素
 * 都可能为 null，直接 `as CellValue[][]` 强转会在 null 行上崩溃
 * （DataGrid 对每行调 row.map）。所有结果网格使用前必须先过这里。
 */
export function narrowRows(
  rows: readonly (readonly any[] | null)[] | null | undefined,
): CellValue[][] {
  return (rows ?? []).filter((r) => r != null) as CellValue[][];
}
