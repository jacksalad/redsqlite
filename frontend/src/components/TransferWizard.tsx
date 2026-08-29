import { useEffect, useState } from "react";
import { ArrowRightLeft } from "lucide-react";
import { Events } from "@wailsio/runtime";
import * as api from "../api";
import { useConnectionsStore } from "../stores/connectionsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { TransferPreview, TransferRequest } from "../types";
import { TRANSFER_PROGRESS_EVENT } from "../utils/constants";
import { Modal } from "./Modal";

/**
 * 跨连接传输向导（V3.0）：选源表 → 选目标连接与表名 → 冲突预检 → 进度执行。
 * 源/目标是两个已打开连接（同连接复制走 SQL 页）；目标侧单事务，
 * 失败整体回滚；rowid/自增计数不保留。
 */
export function TransferWizard({
  connId,
  sourceTable,
  onClose,
}: {
  connId: string;
  sourceTable: string;
  onClose: () => void;
}) {
  const conns = useConnectionsStore((s) => s.conns);
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const sourceConn = conns.find((c) => c.id === connId);

  const [table, setTable] = useState(sourceTable);
  const [targetConnId, setTargetConnId] = useState(
    conns.find((c) => c.id !== connId && !c.info.readOnly)?.id ?? "",
  );
  const [targetTable, setTargetTable] = useState(sourceTable);
  const [includeData, setIncludeData] = useState(true);
  const [includeIndexes, setIncludeIndexes] = useState(true);
  const [includeTriggers, setIncludeTriggers] = useState(true);

  const [preview, setPreview] = useState<TransferPreview | null>(null);
  const [busy, setBusy] = useState(false);
  /** 执行进度（后端 transfer:progress 事件）。 */
  const [progress, setProgress] = useState<{ phase: string; copied: number; total: number } | null>(
    null,
  );

  /** 候选目标连接（排除源连接；含只读连接，选项中禁用并提示）。 */
  const targetConns = conns.filter((c) => c.id !== connId);
  const sourceTables = (sourceConn?.schema?.tables ?? []).map((t) => t.name);

  const buildRequest = (): TransferRequest => ({
    sourceConnId: connId,
    sourceTable: table.trim(),
    targetConnId,
    targetTable: targetTable.trim(),
    includeData,
    includeIndexes,
    includeTriggers,
  });

  useEffect(() => {
    if (!busy) return;
    const off = Events.On(TRANSFER_PROGRESS_EVENT, (ev) => {
      const d = ev.data as {
        targetConnId?: string;
        phase?: string;
        copied?: number;
        total?: number;
      };
      if (d?.targetConnId === targetConnId) {
        setProgress({ phase: d.phase ?? "copy", copied: d.copied ?? 0, total: d.total ?? 0 });
      }
    });
    return () => {
      off();
      setProgress(null);
    };
  }, [busy, targetConnId]);

  const doPreview = async () => {
    if (!targetConnId) {
      toastError("请选择目标连接（同库复制请在 SQL 查询页执行）");
      return;
    }
    setBusy(true);
    try {
      const p = await api.previewTransfer(buildRequest());
      setPreview(p);
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const doApply = async () => {
    if (!preview || preview.blocked) return;
    setBusy(true);
    try {
      const result = await api.applyTransfer(buildRequest());
      toastSuccess(`传输完成：已复制 ${result.rowsCopied} 行（${result.elapsedMs} ms）`);
      await refreshSchema(targetConnId);
      onClose();
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const targetConn = conns.find((c) => c.id === targetConnId);

  return (
    <Modal
      title="跨连接传输表"
      onClose={onClose}
      width={620}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          {preview ? (
            <button
              className="btn primary"
              onClick={() => void doApply()}
              disabled={busy || !!preview.blocked}
            >
              {busy ? "传输中…" : "开始传输"}
            </button>
          ) : (
            <button
              className="btn primary"
              onClick={() => void doPreview()}
              disabled={busy || !table.trim() || !targetConnId || !targetTable.trim()}
            >
              预检
            </button>
          )}
        </>
      }
    >
      <div className="op-form">
        <div className="transfer-route">
          <div className="transfer-endpoint">
            <span className="muted small">源（{sourceConn?.info.fileName ?? connId}）</span>
            <select value={table} onChange={(e) => setTable(e.target.value)}>
              {sourceTables.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
          <ArrowRightLeft size={14} className="transfer-arrow" />
          <div className="transfer-endpoint">
            <span className="muted small">目标（{targetConn?.info.fileName ?? "未选择"}）</span>
            <div className="transfer-target-row">
              <select
                value={targetConnId}
                onChange={(e) => {
                  setTargetConnId(e.target.value);
                  setPreview(null);
                }}
              >
                <option value="">选择连接…</option>
                {targetConns.map((c) => (
                  <option key={c.id} value={c.id} disabled={c.info.readOnly}>
                    {c.info.fileName}
                    {c.info.readOnly ? "（只读）" : ""}
                  </option>
                ))}
              </select>
              <input
                value={targetTable}
                onChange={(e) => {
                  setTargetTable(e.target.value);
                  setPreview(null);
                }}
                placeholder="目标表名"
              />
            </div>
          </div>
        </div>

        <div className="transfer-options">
          <label className="op-check">
            <input
              type="checkbox"
              checked={includeData}
              onChange={(e) => {
                setIncludeData(e.target.checked);
                setPreview(null);
              }}
            />
            复制数据（取消则仅建结构）
          </label>
          <label className="op-check">
            <input
              type="checkbox"
              checked={includeIndexes}
              onChange={(e) => {
                setIncludeIndexes(e.target.checked);
                setPreview(null);
              }}
            />
            连带索引
          </label>
          <label className="op-check">
            <input
              type="checkbox"
              checked={includeTriggers}
              onChange={(e) => {
                setIncludeTriggers(e.target.checked);
                setPreview(null);
              }}
            />
            连带触发器
          </label>
        </div>

        {preview && (
          <div className="transfer-preview">
            {preview.blocked ? (
              <div className="op-blocked">{preview.blocked}</div>
            ) : (
              <p className="muted small">
                源表 {preview.columnCount} 列 / 约 {preview.rowCount} 行，目标表{" "}
                <span className="mono">{preview.targetTable}</span>
                {includeData ? "（将分批复制数据并回报进度）" : "（仅结构）"}。
              </p>
            )}
            {(preview.warnings ?? []).length > 0 && (
              <ul className="op-warnings">
                {(preview.warnings ?? []).map((w, i) => (
                  <li key={i}>{w}</li>
                ))}
              </ul>
            )}
            <details>
              <summary className="muted small">源表 DDL</summary>
              <pre className="ddl sql-preview">{preview.sourceDdl}</pre>
            </details>
          </div>
        )}

        {busy && progress && (
          <div className="redesign-progress">
            <div className="progress-text">
              {progress.phase === "finalize"
                ? "数据复制完成，正在重建索引/触发器…"
                : progress.total > 0
                  ? `正在复制数据：${progress.copied} / ${progress.total} 行（${Math.min(
                      100,
                      Math.round((progress.copied / progress.total) * 100),
                    )}%）`
                  : `正在复制数据：已复制 ${progress.copied} 行`}
            </div>
            {progress.total > 0 && progress.phase !== "finalize" && (
              <div className="progress-track">
                <div
                  className="progress-fill"
                  style={{ width: `${Math.min(100, (progress.copied / progress.total) * 100)}%` }}
                />
              </div>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
