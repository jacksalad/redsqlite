import { useEffect, useRef, useState } from "react";
import type { CellValue } from "../types";
import { CellView } from "./CellView";

export interface GridSort {
  column: string;
  desc: boolean;
}

export interface EditingCell {
  rowKey: string;
  column: string;
}

interface DataGridProps {
  columns: string[];
  columnTypes?: string[];
  rows: CellValue[][];
  /** 与 rows 等长的行键（用于选择与编辑定位） */
  rowKeys?: string[];
  showRowNumbers?: boolean;
  sort?: GridSort | null;
  onSort?: (column: string) => void;
  selectable?: boolean;
  selectedKeys?: Set<string>;
  onToggleSelect?: (key: string) => void;
  onToggleSelectAll?: () => void;
  editable?: boolean;
  editing?: EditingCell | null;
  onBeginEdit?: (rowKey: string, column: string) => void;
  onCommitEdit?: (rowKey: string, column: string, value: CellValue) => void;
  onCancelEdit?: () => void;
  isColumnEditable?: (column: string) => boolean;
  /** 点击 BLOB 单元格时打开查看器（V2.5）。 */
  onOpenBlob?: (rowKey: string, column: string) => void;
  emptyText?: string;
}

function parseEditedText(text: string, setNull: boolean): CellValue {
  if (setNull) return null;
  if (/^-?\d+$/.test(text)) {
    const n = Number.parseInt(text, 10);
    // 超出安全整数范围的整数以字符串回传，由 SQLite 亲和性/后端还原，
    // 避免 JS Number 丢精度（V3.0）
    return Number.isSafeInteger(n) ? n : text;
  }
  if (/^-?\d+\.\d+([eE][+-]?\d+)?$/.test(text)) return Number.parseFloat(text);
  return text;
}

/** 编辑态单元格：Enter 提交、Esc 取消、NULL 开关。 */
function EditCell({
  initial,
  setNullInitial,
  onCommit,
  onCancel,
}: {
  initial: string;
  setNullInitial: boolean;
  onCommit: (value: CellValue) => void;
  onCancel: () => void;
}) {
  const [text, setText] = useState(initial);
  const [isNull, setIsNull] = useState(setNullInitial);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const commit = () => onCommit(parseEditedText(text, isNull));

  return (
    <div className="cell-editor">
      <input
        ref={inputRef}
        value={text}
        disabled={isNull}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
          e.stopPropagation();
        }}
      />
      <button
        className={`mini-toggle ${isNull ? "active" : ""}`}
        title="写入 NULL"
        onClick={() => setIsNull(!isNull)}
      >
        NULL
      </button>
      <button className="mini-toggle ok" title="提交（Enter）" onClick={commit}>
        ✓
      </button>
      <button className="mini-toggle" title="取消（Esc）" onClick={onCancel}>
        ✕
      </button>
    </div>
  );
}

/** 通用只读/可编辑数据网格。 */
export function DataGrid({
  columns,
  columnTypes,
  rows,
  rowKeys,
  showRowNumbers = true,
  sort,
  onSort,
  selectable = false,
  selectedKeys,
  onToggleSelect,
  onToggleSelectAll,
  editable = false,
  editing,
  onBeginEdit,
  onCommitEdit,
  onCancelEdit,
  isColumnEditable,
  onOpenBlob,
  emptyText = "暂无数据",
}: DataGridProps) {
  const allSelected =
    selectable && rowKeys !== undefined && rowKeys.length > 0 &&
    rowKeys.every((k) => selectedKeys?.has(k));

  const handleHeaderClick = (column: string) => {
    if (onSort) onSort(column);
  };

  return (
    <div className="datagrid-wrap">
      <table className="datagrid">
        <thead>
          <tr>
            {(selectable || showRowNumbers) && (
              <th className="col-gutter">
                {selectable && rowKeys ? (
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={() => onToggleSelectAll?.()}
                    aria-label="全选"
                  />
                ) : (
                  "#"
                )}
              </th>
            )}
            {columns.map((col, i) => {
              const sortable = Boolean(onSort);
              const isSorted = sort?.column === col;
              return (
                <th
                  key={col + i}
                  className={sortable ? "sortable" : ""}
                  title={columnTypes?.[i] ? `${col} : ${columnTypes[i]}` : col}
                  tabIndex={sortable ? 0 : undefined}
                  aria-sort={
                    isSorted ? (sort?.desc ? "descending" : "ascending") : undefined
                  }
                  onClick={() => handleHeaderClick(col)}
                  onKeyDown={(e) => {
                    if (sortable && (e.key === "Enter" || e.key === " ")) {
                      e.preventDefault();
                      handleHeaderClick(col);
                    }
                  }}
                >
                  <span className="th-inner">
                    {col}
                    {columnTypes?.[i] && (
                      <span className="th-type">{columnTypes[i].toLowerCase()}</span>
                    )}
                    {isSorted && (
                      <span className="sort-arrow">{sort?.desc ? "▼" : "▲"}</span>
                    )}
                  </span>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={columns.length + 1} className="empty-cell">
                {emptyText}
              </td>
            </tr>
          )}
          {rows.map((row, r) => {
            const rowKey = rowKeys?.[r] ?? String(r);
            const selected = selectedKeys?.has(rowKey) ?? false;
            return (
              <tr key={rowKey} className={selected ? "selected" : ""}>
                {(selectable || showRowNumbers) && (
                  <td className="col-gutter">
                    {selectable && rowKeys ? (
                      <input
                        type="checkbox"
                        checked={selected}
                        onChange={() => onToggleSelect?.(rowKey)}
                        aria-label={`选择第 ${r + 1} 行`}
                      />
                    ) : (
                      r + 1
                    )}
                  </td>
                )}
                {row.map((cell, c) => {
                  const column = columns[c];
                  const isEditing =
                    editing !== null &&
                    editing !== undefined &&
                    editing.rowKey === rowKey &&
                    editing.column === column;
                  const canEdit =
                    editable && (isColumnEditable ? isColumnEditable(column) : true);
                  if (isEditing) {
                    return (
                      <td key={column + c} className="editing">
                        <EditCell
                          initial={cellToText(cell)}
                          setNullInitial={cell === null}
                          onCommit={(value) => onCommitEdit?.(rowKey, column, value)}
                          onCancel={() => onCancelEdit?.()}
                        />
                      </td>
                    );
                  }
                  return (
                    <td
                      key={column + c}
                      className={canEdit ? "editable" : ""}
                      onDoubleClick={() => {
                        if (canEdit && onBeginEdit) onBeginEdit(rowKey, column);
                      }}
                      title={canEdit ? "双击编辑" : undefined}
                    >
                      <CellView
                        value={cell}
                        onOpen={onOpenBlob ? () => onOpenBlob(rowKey, column) : undefined}
                      />
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function cellToText(cell: CellValue): string {
  if (cell === null || cell === undefined) return "";
  if (typeof cell === "object") {
    if ("__kind" in cell) {
      if (cell.__kind === "blob") return cell.b64 ?? "";
      if (cell.__kind === "bigint") return cell.str ?? "";
      return (cell as { value?: string }).value ?? "";
    }
  }
  return String(cell);
}
