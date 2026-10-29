import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Copy, Download, Filter, Plus, RefreshCw, Search, Trash2, X } from "lucide-react";
import * as api from "../api";
import { getErrorMessage } from "../api/errors";
import { DataGrid, type EditingCell, type GridSort } from "../components/DataGrid";
import { Pagination } from "../components/Pagination";
import { ConfirmDialog } from "../components/Modal";
import { InsertRowModal } from "../components/InsertRowModal";
import { BlobViewerModal } from "../components/BlobViewerModal";
import { useConnectionsStore } from "../stores/connectionsStore";
import { useSettingsStore } from "../stores/settingsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import { FilterOp, type CellValue, type FilterCondition, type TableData, type Tab } from "../types";
import { ROWID_COLUMN } from "../utils/constants";
import { keyMapOfRow, keyOfRow } from "../utils/rowKeys";
import { narrowRows } from "../utils/rows";

interface TableDataPageProps {
  tab: Tab;
}

/** 游标/页码导航状态：cursor 为当前页起始游标，stack 为之前各页的起始游标。 */
interface Nav {
  cursor: string;
  stack: string[];
  page: number;
}

const INITIAL_NAV: Nav = { cursor: "", stack: [], page: 1 };

/** 结构化筛选操作符（V3.0 列筛选构建器）。 */
const FILTER_OPS: { value: FilterOp; label: string }[] = [
  { value: FilterOp.FilterEq, label: "=" },
  { value: FilterOp.FilterNe, label: "≠" },
  { value: FilterOp.FilterGt, label: ">" },
  { value: FilterOp.FilterGe, label: "≥" },
  { value: FilterOp.FilterLt, label: "<" },
  { value: FilterOp.FilterLe, label: "≤" },
  { value: FilterOp.FilterLike, label: "包含" },
  { value: FilterOp.FilterRange, label: "范围" },
  { value: FilterOp.FilterIsNull, label: "为空" },
  { value: FilterOp.FilterNotNull, label: "非空" },
];

function newCondition(): FilterCondition {
  return { column: "", op: FilterOp.FilterEq, value: "", value2: "" };
}

/** 表数据浏览与编辑页（文档 6.3 / 6.4）。默认顺序走游标分页，排序时回退页码分页。 */
export function TableDataPage({ tab }: TableDataPageProps) {
  const connId = tab.connId;
  const table = tab.table!;
  const settings = useSettingsStore((s) => s.settings);

  const [data, setData] = useState<TableData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [totalRows, setTotalRows] = useState<number | null>(null);
  const countSeq = useRef(0);
  const loadSeq = useRef(0);

  const [nav, setNav] = useState<Nav>(INITIAL_NAV);
  const [pageSize, setPageSize] = useState(settings.pageSize);
  const [sort, setSort] = useState<GridSort | null>(null);
  const [filterInput, setFilterInput] = useState("");
  const [filter, setFilter] = useState("");
  /** 结构化筛选（V3.0）：draft 条件编辑中，conditions 已应用。 */
  const [showFilters, setShowFilters] = useState(false);
  const [draftConds, setDraftConds] = useState<FilterCondition[]>([]);
  const [conditions, setConditions] = useState<FilterCondition[]>([]);

  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [editing, setEditing] = useState<EditingCell | null>(null);
  const [insertOpen, setInsertOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  /** 打开查看器的 BLOB 单元格（V2.5）。 */
  const [blobCell, setBlobCell] = useState<{ rowKey: string; column: string } | null>(null);

  const readOnlyNote =
    useConnectionsStore((s) => s.conns.find((c) => c.id === connId)?.info.readOnly) ?? false;

  const load = useCallback(
    async (opts?: {
      nav?: Nav;
      sort?: GridSort | null;
      filter?: string;
      pageSize?: number;
      conditions?: FilterCondition[];
    }) => {
      const n = opts?.nav ?? nav;
      const ps = opts?.pageSize ?? pageSize;
      const s = opts?.sort !== undefined ? opts.sort : sort;
      const f = opts?.filter !== undefined ? opts.filter : filter;
      const conds = opts?.conditions !== undefined ? opts.conditions : conditions;
      // 始终携带游标：后端按 排序+游标列 可用性决定游标/OFFSET 模式（V3.0
      // 排序浏览也走复合游标分页），OFFSET 模式下 cursor 被忽略
      // 序号守卫：快速翻页/切排序/切筛选时丢弃后发先至的过期响应，防止旧数据覆盖新页
      const reqSeq = ++loadSeq.current;
      setLoading(true);
      setError(null);
      setEditing(null);
      try {
        const result = await api.getTableData(connId, {
          table,
          page: n.page,
          pageSize: ps,
          orderBy: s?.column === ROWID_COLUMN ? ROWID_COLUMN : s?.column ?? "",
          orderDesc: s?.desc ?? false,
          filter: f,
          conditions: conds,
          cursor: n.cursor,
        });
        if (loadSeq.current !== reqSeq) return;
        setData(result);
        // 行数统计异步进行，大表 COUNT(*) 不阻塞浏览（V1.1）
        const seq = ++countSeq.current;
        api
          .countTableRows(connId, table, f, conds)
          .then((total) => {
            if (countSeq.current === seq) setTotalRows(total);
          })
          .catch(() => {
            if (countSeq.current === seq) setTotalRows(null);
          });
      } catch (err) {
        if (loadSeq.current !== reqSeq) return;
        setError(getErrorMessage(err));
        setData(null);
      } finally {
        if (loadSeq.current === reqSeq) setLoading(false);
      }
    },
    [table, nav, pageSize, sort, filter, conditions],
  );

  useEffect(() => {
    void load({ nav: INITIAL_NAV });
    setSelectedKeys(new Set());
    setTotalRows(null);
    setConditions([]);
    setDraftConds([]);
    setShowFilters(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [table]);

  // F5 刷新（文档 5.2）：按当前导航位置重取本页
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "F5") {
        e.preventDefault();
        void load();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [load]);

  // 生成的绑定类型把每行标为可空，统一过滤收窄（见 utils/rows）
  const rows = useMemo(() => narrowRows(data?.rows), [data]);

  const rowKeys = useMemo(() => {
    if (!data) return undefined;
    return rows.map((row) => keyOfRow(row, data.columns ?? [], data.keyColumns));
  }, [data, rows]);

  const keyValueMap = useMemo(() => {
    const map = new Map<string, Record<string, unknown>>();
    if (!data) return map;
    rows.forEach((row, i) => {
      const key = rowKeys?.[i] ?? String(i);
      map.set(key, keyMapOfRow(row, data.columns ?? [], data.keyColumns));
    });
    return map;
  }, [data, rows, rowKeys]);

  const editable = data?.editable ?? false;
  const keyColumns = data?.keyColumns ?? [];

  const resetNav = useCallback(
    (extra?: {
      sort?: GridSort | null;
      filter?: string;
      pageSize?: number;
      conditions?: FilterCondition[];
    }) => {
      setNav(INITIAL_NAV);
      void load({ nav: INITIAL_NAV, ...extra });
    },
    [load],
  );

  const onSortClick = (column: string) => {
    const next: GridSort | null =
      sort?.column === column ? (sort.desc ? null : { column, desc: true }) : { column, desc: false };
    setSort(next);
    resetNav({ sort: next });
  };

  const applyFilter = (value: string) => {
    setFilter(value);
    resetNav({ filter: value });
  };

  // ---------- 结构化筛选（V3.0） ----------
  const filterableColumns = useMemo(
    () => (data?.columns ?? []).filter((c) => c !== ROWID_COLUMN),
    [data],
  );

  const updateDraft = (i: number, patch: Partial<FilterCondition>) =>
    setDraftConds((list) => list.map((c, idx) => (idx === i ? { ...c, ...patch } : c)));

  const applyConditions = () => {
    const next = draftConds.filter((c) => c.column);
    setConditions(next);
    resetNav({ conditions: next });
  };

  const clearConditions = () => {
    setDraftConds([]);
    setConditions([]);
    resetNav({ conditions: [] });
  };

  const toggleSelect = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (!rowKeys) return;
    setSelectedKeys((prev) => {
      const all = rowKeys.every((k) => prev.has(k));
      return all ? new Set() : new Set(rowKeys);
    });
  };

  // ---------- 翻页 ----------
  const isCursorMode = data?.cursorMode ?? false;
  const totalPages = totalRows != null ? Math.max(1, Math.ceil(totalRows / pageSize)) : null;
  const cursorHasNext = (data?.hasNext ?? false) && Boolean(data?.nextCursor);
  const offsetHasNext = totalPages != null ? nav.page < totalPages : (data?.hasNext ?? false);
  const hasPrev = isCursorMode ? nav.stack.length > 0 : nav.page > 1;

  const goNext = () => {
    if (!data) return;
    if (isCursorMode) {
      if (!cursorHasNext || !data.nextCursor) return;
      const next: Nav = { cursor: data.nextCursor, stack: [...nav.stack, nav.cursor], page: nav.page + 1 };
      setNav(next);
      void load({ nav: next });
    } else {
      if (!offsetHasNext) return;
      const next: Nav = { ...nav, page: nav.page + 1 };
      setNav(next);
      void load({ nav: next });
    }
  };

  const goPrev = () => {
    if (isCursorMode) {
      if (nav.stack.length === 0) return;
      const stack = [...nav.stack];
      const cursor = stack.pop()!;
      const next: Nav = { cursor, stack, page: nav.page - 1 };
      setNav(next);
      void load({ nav: next });
    } else {
      if (nav.page <= 1) return;
      const next: Nav = { ...nav, page: nav.page - 1 };
      setNav(next);
      void load({ nav: next });
    }
  };

  const jumpPage = (page: number) => {
    const next: Nav = { ...nav, page };
    setNav(next);
    void load({ nav: next });
  };

  const changePageSize = (ps: number) => {
    setPageSize(ps);
    resetNav({ pageSize: ps });
  };

  const commitEdit = async (rowKey: string, column: string, value: CellValue) => {
    if (!data) return;
    const keyMap = keyValueMap.get(rowKey);
    if (!keyMap) return;
    // __rowid__ 不可编辑；keyColumns 中的主键列不允许编辑（避免破坏行定位）
    if (column === ROWID_COLUMN || keyColumns.includes(column)) {
      toastError("行定位字段（主键 / rowid）不可编辑，请使用 SQL 修改");
      setEditing(null);
      return;
    }
    setEditing(null);
    try {
      await api.updateCell(connId, table, keyMap, column, value === undefined ? null : value);
      toastSuccess("已更新");
      await load();
    } catch (err) {
      toastError(err); // 保留原值并提示（文档 12）
    }
  };

  const doDelete = async () => {
    setConfirmDelete(false);
    const keys = [...selectedKeys]
      .map((k) => keyValueMap.get(k))
      .filter((m): m is Record<string, unknown> => Boolean(m));
    try {
      const n = await api.deleteRows(connId, table, keys);
      toastSuccess(`已删除 ${n} 行`);
      setSelectedKeys(new Set());
      await load();
    } catch (err) {
      toastError(err);
    }
  };

  const doDuplicate = async () => {
    const keys = [...selectedKeys]
      .map((k) => keyValueMap.get(k))
      .filter((m): m is Record<string, unknown> => Boolean(m));
    try {
      const n = await api.duplicateRows(connId, table, keys);
      toastSuccess(`已复制 ${n} 行`);
      setSelectedKeys(new Set());
      await load();
    } catch (err) {
      toastError(err);
    }
  };

  const exportData = async (format: "csv" | "json" | "xlsx") => {
    try {
      const path = await api.pickExportPath(`${table}-export`, format);
      if (!path) return;
      const result = await api.exportTable(connId, table, format, path);
      toastSuccess(`已导出 ${result.rows} 行到 ${result.path}`);
    } catch (err) {
      toastError(err);
    }
  };

  return (
    <div className="page table-data-page">
      <div className="page-toolbar">
        <div className="page-toolbar-group">
          <button className="btn" onClick={() => void load()} disabled={loading} title="刷新（F5）">
            <RefreshCw size={13} className={loading ? "spin" : ""} /> 刷新
          </button>
          {editable && (
            <>
              <button className="btn" onClick={() => setInsertOpen(true)}>
                <Plus size={13} /> 新增行
              </button>
              <button
                className="btn"
                disabled={selectedKeys.size === 0}
                onClick={() => void doDuplicate()}
                title="复制选中的行（单列 INTEGER 主键自动取新自增值）"
              >
                <Copy size={13} /> 复制行{selectedKeys.size > 0 ? `（${selectedKeys.size}）` : ""}
              </button>
              <button
                className="btn danger"
                disabled={selectedKeys.size === 0}
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 size={13} /> 删除{selectedKeys.size > 0 ? `（${selectedKeys.size}）` : ""}
              </button>
            </>
          )}
        </div>

        <div className="page-toolbar-group">
          <button
            className={`btn${showFilters || conditions.length > 0 ? " primary" : ""}`}
            onClick={() => setShowFilters((v) => !v)}
            title="列筛选构建器：按列/操作符组合条件（多条件 AND）"
          >
            <Filter size={13} /> 筛选{conditions.length > 0 ? `（${conditions.length}）` : ""}
          </button>
          <div className="filter-box">
            <Search size={13} />
            <input
              placeholder="快速过滤，回车应用"
              value={filterInput}
              onChange={(e) => setFilterInput(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && applyFilter(filterInput)}
            />
            {filterInput !== filter && (
              <button className="mini-toggle ok" onClick={() => applyFilter(filterInput)}>
                应用
              </button>
            )}
            {filter && (
              <button
                className="mini-toggle"
                onClick={() => {
                  setFilterInput("");
                  applyFilter("");
                }}
              >
                清除
              </button>
            )}
          </div>
          <button className="btn" onClick={() => void exportData("csv")} title="导出整表为 CSV">
            <Download size={13} /> CSV
          </button>
          <button className="btn" onClick={() => void exportData("json")} title="导出整表为 JSON">
            <Download size={13} /> JSON
          </button>
          <button className="btn" onClick={() => void exportData("xlsx")} title="导出整表为 Excel">
            <Download size={13} /> XLSX
          </button>
        </div>
      </div>

      {showFilters && (
        <div className="filter-builder">
          {draftConds.length === 0 && (
            <span className="muted small">尚未添加条件（多条件之间为 AND 关系）</span>
          )}
          {draftConds.map((c, i) => (
            <div className="filter-row" key={i}>
              <select
                value={c.column}
                onChange={(e) => updateDraft(i, { column: e.target.value })}
                aria-label="筛选列"
              >
                <option value="">选择列…</option>
                {filterableColumns.map((col) => (
                  <option key={col} value={col}>
                    {col}
                  </option>
                ))}
              </select>
              <select
                value={c.op}
                onChange={(e) => updateDraft(i, { op: e.target.value as FilterOp })}
                aria-label="操作符"
              >
                {FILTER_OPS.map((op) => (
                  <option key={op.value} value={op.value}>
                    {op.label}
                  </option>
                ))}
              </select>
              {c.op !== FilterOp.FilterIsNull && c.op !== FilterOp.FilterNotNull && (
                <>
                  <input
                    value={c.value}
                    placeholder={c.op === FilterOp.FilterRange ? "下界（含）" : "值"}
                    onChange={(e) => updateDraft(i, { value: e.target.value })}
                    onKeyDown={(e) => e.key === "Enter" && applyConditions()}
                  />
                  {c.op === FilterOp.FilterRange && (
                    <>
                      <span className="muted">~</span>
                      <input
                        value={c.value2}
                        placeholder="上界（含）"
                        onChange={(e) => updateDraft(i, { value2: e.target.value })}
                        onKeyDown={(e) => e.key === "Enter" && applyConditions()}
                      />
                    </>
                  )}
                </>
              )}
              <button
                className="icon-btn"
                title="移除条件"
                onClick={() => setDraftConds((list) => list.filter((_, idx) => idx !== i))}
              >
                <X size={12} />
              </button>
            </div>
          ))}
          <div className="filter-actions">
            <button className="btn" onClick={() => setDraftConds((l) => [...l, newCondition()])}>
              <Plus size={12} /> 添加条件
            </button>
            <button
              className="btn primary"
              onClick={applyConditions}
              disabled={loading || draftConds.every((c) => !c.column)}
            >
              应用筛选
            </button>
            {(draftConds.length > 0 || conditions.length > 0) && (
              <button className="btn" onClick={clearConditions}>
                清除全部
              </button>
            )}
          </div>
        </div>
      )}

      {!editable && data && (
        <div className="notice">
          {readOnlyNote
            ? "数据库以只读方式打开，仅可浏览数据。"
            : "当前对象仅支持浏览（视图或缺少可靠行定位字段）。如需修改数据请通过 SQL 查询页执行。"}
        </div>
      )}
      {error && <div className="error-banner">{error}</div>}

      <div className="page-body">
        {data ? (
          <DataGrid
            columns={data.columns ?? []}
            columnTypes={data.columnTypes ?? undefined}
            rows={rows}
            rowKeys={rowKeys}
            sort={sort}
            onSort={onSortClick}
            selectable={editable}
            selectedKeys={selectedKeys}
            onToggleSelect={toggleSelect}
            onToggleSelectAll={toggleSelectAll}
            editable={editable}
            editing={editing}
            onBeginEdit={(rowKey, column) => setEditing({ rowKey, column })}
            onCommitEdit={(rowKey, column, value) => void commitEdit(rowKey, column, value)}
            onCancelEdit={() => setEditing(null)}
            isColumnEditable={(column) => column !== ROWID_COLUMN && !keyColumns.includes(column)}
            onOpenBlob={(rowKey, column) => setBlobCell({ rowKey, column })}
            emptyText={filter || conditions.length > 0 ? "没有匹配过滤条件的数据" : "表中没有数据"}
          />
        ) : (
          !error && <div className="empty-state">{loading ? "加载中…" : "暂无数据"}</div>
        )}
      </div>

      {data && (
        <div className="page-footer">
          <Pagination
            mode={data.cursorMode ? "cursor" : "offset"}
            page={nav.page}
            pageSize={pageSize}
            shown={rows.length}
            total={totalRows}
            hasPrev={hasPrev}
            hasNext={data.cursorMode ? cursorHasNext : offsetHasNext}
            onPrev={goPrev}
            onNext={goNext}
            onJump={jumpPage}
            onPageSize={changePageSize}
          />
          <span className="elapsed">查询耗时 {data.elapsedMs} ms</span>
        </div>
      )}

      {insertOpen && (
        <InsertRowModal
          connId={connId}
          table={table}
          onClose={() => setInsertOpen(false)}
          onInserted={() => void load()}
        />
      )}

      {blobCell && data && (
        <BlobViewerModal
          connId={connId}
          table={table}
          keyMap={keyValueMap.get(blobCell.rowKey) ?? {}}
          column={blobCell.column}
          editable={
            editable &&
            blobCell.column !== ROWID_COLUMN &&
            !keyColumns.includes(blobCell.column)
          }
          onClose={() => setBlobCell(null)}
          onSaved={() => void load()}
        />
      )}

      {confirmDelete && (
        <ConfirmDialog
          title="删除行"
          message={`确定删除选中的 ${selectedKeys.size} 行吗？\n此操作无法自动恢复。`}
          confirmText="删除"
          onConfirm={() => void doDelete()}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </div>
  );
}
