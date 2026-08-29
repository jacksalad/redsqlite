// 重新导出生成的领域模型，并补充 UI 专用类型。
export * from "../../bindings/github.com/redjack/redsqlite/domain/models.js";
export * from "../../bindings/github.com/redjack/redsqlite/internal/exporter/models.js";
// V2.5：CSV / Excel 共用的表格导入模型来自 tabular（Result 与 exporter 冲突，用别名），
// 工作表信息来自 xlsximport。
export type {
  Preview as ImportPreview,
  ImportRequest,
  Result as ImportResult,
} from "../../bindings/github.com/redjack/redsqlite/internal/tabular/models.js";
export type {
  SheetInfo,
} from "../../bindings/github.com/redjack/redsqlite/internal/xlsximport/models.js";

/** BLOB 单元格标记（后端 internal/cell 生成）。 */
export interface BlobMarker {
  __kind: "blob";
  size: number;
  b64: string;
}

/** 超长文本截断标记。 */
export interface TruncatedTextMarker {
  __kind: "text-truncated";
  size: number;
  truncated: true;
  value: string;
}

/** 大整数标记（V3.0）：超出 JS 安全整数范围的 int64 以十进制字符串传输。 */
export interface BigintMarker {
  __kind: "bigint";
  str: string;
}

export type CellValue =
  | null
  | boolean
  | number
  | string
  | BlobMarker
  | TruncatedTextMarker
  | BigintMarker;

export function isBlobMarker(v: unknown): v is BlobMarker {
  return typeof v === "object" && v !== null && (v as BlobMarker).__kind === "blob";
}

export function isTruncatedMarker(v: unknown): v is TruncatedTextMarker {
  return typeof v === "object" && v !== null && (v as TruncatedTextMarker).__kind === "text-truncated";
}

export function isBigintMarker(v: unknown): v is BigintMarker {
  return typeof v === "object" && v !== null && (v as BigintMarker).__kind === "bigint";
}

/** Tab 类型：表数据 / 表结构 / SQL 查询。 */
export type TabKind = "table" | "structure" | "sql";

export interface Tab {
  id: string;
  /** 所属数据库连接（V2.1 多库 Tab） */
  connId: string;
  kind: TabKind;
  title: string;
  /** 表/视图名（table 与 structure 类型使用） */
  table?: string;
  /** SQL 编辑器内容（sql 类型使用，随 Tab 持久于会话内） */
  sql?: string;
}

/** 查询历史条目（localStorage 持久化）。 */
export interface HistoryEntry {
  sql: string;
  at: number;
  ok: boolean;
}
