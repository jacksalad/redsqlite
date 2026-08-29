import { useEffect, useMemo, useRef, useState } from "react";
import { FileUp, Upload } from "lucide-react";
import * as api from "../api";
import { Modal } from "./Modal";
import { useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnInfo, ImportPreview, SheetInfo } from "../types";

interface ImportWizardProps {
  /** 目标数据库连接（V2.1）。 */
  connId: string;
  onClose: () => void;
}

function isExcelPath(path: string): boolean {
  return /\.(xlsx|xlsm)$/i.test(path);
}

/**
 * 数据导入向导（V1.2 CSV；V2.5 增加 Excel）：选文件（Excel 先选工作表）
 * → 预览前 50 行 → 列映射 → 事务导入。导入在后端单个事务中流式执行，
 * 任一行失败整体回滚。
 */
export function ImportWizard({ connId, onClose }: ImportWizardProps) {
  const schema = useConnectionsStore((s) => s.conns.find((c) => c.id === connId)?.schema ?? null);
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const openTable = useTabsStore((s) => s.openTable);

  const [step, setStep] = useState<1 | 2>(1);
  const [path, setPath] = useState("");
  const [sheets, setSheets] = useState<SheetInfo[] | null>(null);
  const [sheet, setSheet] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [hasHeader, setHasHeader] = useState(true);
  const [emptyAsNull, setEmptyAsNull] = useState(true);
  const [table, setTable] = useState("");
  const [columns, setColumns] = useState<ColumnInfo[] | null>(null);
  /** 目标列名 -> 来源列下标（-1 = 不导入）。导入时反转为后端需要的 srcIndex -> 列名。 */
  const [targetToSrc, setTargetToSrc] = useState<Record<string, number>>({});
  const [busy, setBusy] = useState(false);

  const tables = useMemo(() => schema?.tables ?? [], [schema]);

  // 预览过期守卫：选文件/切工作表连续触发时，慢的旧响应不得覆盖新响应
  const previewSeq = useRef(0);
  const loadPreview = async (p: string, s: string, header: boolean) => {
    const seq = ++previewSeq.current;
    setBusy(true);
    try {
      const result = await api.previewImport(p, s, header);
      if (previewSeq.current !== seq) return;
      setPreview(result);
    } catch (err) {
      if (previewSeq.current !== seq) return;
      setPreview(null);
      toastError(err);
    } finally {
      if (previewSeq.current === seq) setBusy(false);
    }
  };

  const pickFile = async () => {
    try {
      const p = await api.pickImportFile();
      if (!p) return;
      setPath(p);
      setPreview(null);
      setStep(1);
      if (isExcelPath(p)) {
        try {
          const list = await api.listSheets(p);
          setSheets(list);
          const first = list[0]?.name ?? "";
          setSheet(first);
          // 直接取一次预览；setSheet 还会触发下方 effect 再取一次，
          // 序号守卫保证同参数的重复请求结果一致、旧响应不覆盖新响应
          await loadPreview(p, first, hasHeader);
        } catch (err) {
          setSheets([]);
          setSheet("");
          toastError(err);
        }
      } else {
        setSheets(null);
        setSheet("");
        await loadPreview(p, "", hasHeader);
      }
    } catch (err) {
      toastError(err);
    }
  };

  // 「首行为列名」/ 工作表切换后重新预览
  useEffect(() => {
    if (path) void loadPreview(path, sheet, hasHeader);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasHeader, sheet]);

  // 进入第 2 步：读取目标表结构并按列名自动匹配
  const goMapping = async () => {
    if (!preview) return;
    setBusy(true);
    try {
      const structure = await api.getTableStructure(connId, table);
      setColumns(structure.columns ?? []);
      const auto: Record<string, number> = {};
      for (const col of structure.columns ?? []) {
        const idx = (preview.columns ?? []).findIndex(
          (c) => c.toLowerCase() === col.name.toLowerCase(),
        );
        auto[col.name] = idx;
      }
      setTargetToSrc(auto);
      setStep(2);
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  const notNullUnmapped = useMemo(() => {
    if (!columns) return [];
    return columns.filter(
      (c) => c.notNull && !c.primaryKey && (targetToSrc[c.name] ?? -1) < 0,
    );
  }, [columns, targetToSrc]);

  const doImport = async () => {
    if (!preview) return;
    // 反转映射：来源列下标 -> 目标列名
    const mapping: string[] = new Array(preview.columns?.length ?? 0).fill("");
    for (const [target, srcIdx] of Object.entries(targetToSrc)) {
      if (srcIdx >= 0 && srcIdx < mapping.length) mapping[srcIdx] = target;
    }
    if (!mapping.some((m) => m !== "")) {
      toastError("请至少映射一个列");
      return;
    }
    setBusy(true);
    try {
      const result = await api.importTable(connId, { path, sheet, table, hasHeader, mapping, emptyAsNull });
      toastSuccess(`已导入 ${result.inserted} 行到 ${table}（耗时 ${result.elapsedMs} ms）`);
      await refreshSchema();
      openTable(connId, table);
      onClose();
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  const srcColumns = preview?.columns ?? [];

  return (
    <Modal
      title={`导入数据（CSV / Excel）${step === 2 ? " · 列映射" : ""}`}
      onClose={onClose}
      width={720}
      footer={
        <>
          {step === 2 && (
            <button className="btn" onClick={() => setStep(1)} disabled={busy}>
              上一步
            </button>
          )}
          <button className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          {step === 1 ? (
            <button
              className="btn primary"
              disabled={!path || !preview || !table || busy || (preview?.columns?.length ?? 0) === 0}
              onClick={() => void goMapping()}
            >
              下一步：映射列
            </button>
          ) : (
            <button
              className="btn primary"
              disabled={busy || notNullUnmapped.length > 0}
              onClick={() => void doImport()}
            >
              <Upload size={13} /> 开始导入
            </button>
          )}
        </>
      }
    >
      {step === 1 ? (
        <div className="import-step">
          <div className="import-file-row">
            <button className="btn" onClick={() => void pickFile()} disabled={busy}>
              <FileUp size={13} /> 选择文件
            </button>
            <span className="import-path" title={path}>
              {path || "未选择文件"}
            </span>
            {preview && preview.delim && (
              <span className="muted">分隔符 {preview.delim === "\t" ? "Tab" : preview.delim}</span>
            )}
            {preview && preview.truncated && <span className="muted">仅预览前 50 行</span>}
          </div>

          {sheets && sheets.length > 0 && (
            <div className="import-target-row">
              <span>工作表：</span>
              <select value={sheet} onChange={(e) => setSheet(e.target.value)} disabled={busy}>
                {sheets.map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name}
                    {s.rows > 0 ? `（约 ${s.rows} 行）` : ""}
                  </option>
                ))}
              </select>
            </div>
          )}

          <label className="import-option">
            <input
              type="checkbox"
              checked={hasHeader}
              onChange={(e) => setHasHeader(e.target.checked)}
              disabled={busy}
            />
            首行为列名
          </label>

          <div className="import-target-row">
            <span>导入到表：</span>
            <select value={table} onChange={(e) => setTable(e.target.value)} disabled={busy}>
              <option value="">选择目标表…</option>
              {tables.map((t) => (
                <option key={t.name} value={t.name}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>

          {busy && <p className="muted">解析中…</p>}
          {preview && (
            <div className="import-preview">
              <table className="import-table">
                <thead>
                  <tr>
                    {srcColumns.map((c, i) => (
                      <th key={i}>{c}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(preview.rows ?? []).slice(0, 20).map((row, i) => (
                    <tr key={i}>
                      {srcColumns.map((_, j) => (
                        <td key={j}>{row?.[j] ?? ""}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
              {(preview.rows?.length ?? 0) === 0 && (
                <p className="muted">文件没有数据行</p>
              )}
            </div>
          )}
        </div>
      ) : (
        <div className="import-step">
          <p className="muted">
            为目标表的每个字段选择来源列（按列名自动匹配，可调整；「不导入」表示留空）。
          </p>
          <label className="import-option">
            <input
              type="checkbox"
              checked={emptyAsNull}
              onChange={(e) => setEmptyAsNull(e.target.checked)}
              disabled={busy}
            />
            空单元格按 NULL 写入
          </label>
          {notNullUnmapped.length > 0 && (
            <div className="notice">
              以下 NOT NULL 字段未映射，无法导入：{notNullUnmapped.map((c) => c.name).join("、")}
            </div>
          )}
          <table className="import-map-table">
            <thead>
              <tr>
                <th>目标字段</th>
                <th>类型</th>
                <th>来源列</th>
              </tr>
            </thead>
            <tbody>
              {(columns ?? []).map((col) => (
                <tr key={col.name}>
                  <td>
                    {col.name}
                    {col.primaryKey && <span className="pk-badge">PK</span>}
                    {col.notNull && !col.primaryKey && <span className="nn-badge">NOT NULL</span>}
                  </td>
                  <td className="muted">{col.type || "ANY"}</td>
                  <td>
                    <select
                      value={String(targetToSrc[col.name] ?? -1)}
                      onChange={(e) =>
                        setTargetToSrc((prev) => ({ ...prev, [col.name]: Number(e.target.value) }))
                      }
                      disabled={busy}
                    >
                      <option value="-1">不导入</option>
                      {srcColumns.map((c, i) => (
                        <option key={i} value={i}>
                          {c}
                        </option>
                      ))}
                    </select>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Modal>
  );
}
