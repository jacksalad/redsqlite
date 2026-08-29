import type { CellValue } from "../types";

/**
 * 行定位工具：根据主键列（或回退的首列）从行数据提取定位映射。
 * BLOB/bigint 标记对象原样回传（后端 cell 层能还原），截断文本不可靠返回 null。
 */

/** 根据定位列生成行键字符串。 */
export function keyOfRow(
  row: CellValue[],
  columns: string[],
  keyColumns: string[] | null | undefined,
): string {
  return JSON.stringify(keyMapOfRow(row, columns, keyColumns));
}

/** 从行数据提取定位列 -> 值的映射。 */
export function keyMapOfRow(
  row: CellValue[],
  columns: string[],
  keyColumns: string[] | null | undefined,
): Record<string, unknown> {
  const map: Record<string, unknown> = {};
  const keys = keyColumns?.length ? keyColumns : columns.slice(0, 1);
  for (const key of keys) {
    const idx = columns.indexOf(key);
    if (idx >= 0) {
      map[key] = normalizeKeyValue(row[idx]);
    }
  }
  return map;
}

function normalizeKeyValue(v: CellValue): unknown {
  if (v === null || v === undefined) return null;
  if (typeof v === "object" && "__kind" in v) {
    if (v.__kind === "blob") return v; // 后端 cell.Decode 支持还原 b64
    if (v.__kind === "bigint") return v; // 后端按十进制字符串还原 int64
    return null; // 截断文本作为定位值不可靠
  }
  return v;
}
