import { useEffect, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2, Wrench } from "lucide-react";
import { Events } from "@wailsio/runtime";
import * as api from "../api";
import { useConnectionsStore } from "../stores/connectionsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnInfo, ForeignKeySpec, RedesignColumn, RedesignPreview } from "../types";
import { REDESIGN_PROGRESS_EVENT } from "../utils/constants";
import { Modal } from "./Modal";
import { SqlPreviewModal } from "./TableOpFlow";

const FK_ACTIONS = ["", "NO ACTION", "RESTRICT", "SET NULL", "SET DEFAULT", "CASCADE"];
let fkSeq = 1;

interface FkRow extends ForeignKeySpec {
  key: number;
}

/** 默认值预填：PRAGMA 返回的 dflt_value 即 SQL 文本（'带引号' / CURRENT_TIMESTAMP / 123）。 */
function prefillDefault(v: unknown): { text: string; isExpr: boolean } {
  if (v === null || v === undefined) return { text: "", isExpr: false };
  const s = String(v);
  if (s.startsWith("'") || /^-?\d/.test(s) || /^0x/i.test(s) || /^x'/i.test(s)) {
    return { text: s, isExpr: false };
  }
  return { text: s, isExpr: true };
}

/**
 * 表重设计弹窗（V2.4）：改类型/约束/列序/主键/表名/新增外键。
 * 后端通过"建新表 → 拷数据 → 删旧改名 → 重建索引/触发器"单事务重建，
 * 预览展示全部语句与风险提示，任一步失败整体回滚。
 */
export function RedesignModal({
  connId,
  table,
  onClose,
  onApplied,
}: {
  connId: string;
  table: string;
  onClose: () => void;
  onApplied: (newTable: string) => void;
}) {
  const [rows, setRows] = useState<RedesignColumn[] | null>(null);
  const [newTable, setNewTable] = useState("");
  const [fks, setFks] = useState<FkRow[]>([]);
  const [refColumns, setRefColumns] = useState<Record<string, string[]>>({});
  const [preview, setPreview] = useState<RedesignPreview | null>(null);
  const [busy, setBusy] = useState(false);
  /** 重建进度（V3.0）：后端分块拷贝时经事件回报。 */
  const [progress, setProgress] = useState<{ phase: string; copied: number; total: number } | null>(
    null,
  );

  // 执行期间订阅后端进度事件（按连接过滤，避免多库 Tab 串扰）
  useEffect(() => {
    if (!busy) return;
    const off = Events.On(REDESIGN_PROGRESS_EVENT, (ev) => {
      const d = ev.data as { connId?: string; phase?: string; copied?: number; total?: number };
      if (d?.connId === connId) {
        setProgress({ phase: d.phase ?? "copy", copied: d.copied ?? 0, total: d.total ?? 0 });
      }
    });
    return () => {
      off();
      setProgress(null);
    };
  }, [busy, connId]);

  const schema = useConnectionsStore((s) => s.conns.find((c) => c.id === connId)?.schema ?? null);
  const tableNames = (schema?.tables ?? []).map((t) => t.name);

  useEffect(() => {
    let alive = true;
    api
      .getTableColumns(connId, table)
      .then((cols: ColumnInfo[]) => {
        if (!alive) return;
        setRows(
          (cols ?? []).map((c) => {
            const d = prefillDefault(c.defaultValue);
            return {
              name: c.name,
              newName: "",
              type: c.type ?? "",
              notNull: c.notNull,
              primaryKey: c.primaryKey,
              default: d.text,
              defaultIsExpression: d.isExpr,
              drop: false,
            };
          }),
        );
      })
      .catch((error) => {
        toastError(error);
        if (alive) onClose();
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connId, table]);

  // 引用表列加载（外键下拉）
  useEffect(() => {
    let alive = true;
    for (const fk of fks) {
      if (!fk.refTable || refColumns[fk.refTable]) continue;
      api
        .getTableColumns(connId, fk.refTable)
        .then((cols) => {
          if (alive) setRefColumns((m) => ({ ...m, [fk.refTable]: (cols ?? []).map((c) => c.name) }));
        })
        .catch(() => {
          if (alive) setRefColumns((m) => ({ ...m, [fk.refTable]: [] }));
        });
    }
    return () => {
      alive = false;
    };
  }, [fks, connId, refColumns]);

  if (rows === null) return null;

  const updateRow = (i: number, patch: Partial<RedesignColumn>) =>
    setRows((rs) => rs!.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));

  const moveRow = (i: number, delta: number) =>
    setRows((rs) => {
      const j = i + delta;
      if (j < 0 || j >= rs!.length) return rs;
      const next = [...rs!];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });

  const addRow = () =>
    setRows((rs) => [
      ...rs!,
      { name: "", newName: "", type: "TEXT", notNull: false, primaryKey: false, default: "", defaultIsExpression: false, drop: false },
    ]);

  const updateFk = (key: number, patch: Partial<FkRow>) =>
    setFks((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  const buildRedesign = () => ({
    table,
    newTable: newTable.trim(),
    columns: rows!.map((r) => ({
      name: r.name,
      newName: r.newName.trim(),
      type: r.type.trim(),
      notNull: r.notNull,
      primaryKey: r.primaryKey,
      default: r.default.trim(),
      defaultIsExpression: r.defaultIsExpression,
      drop: r.drop,
    })),
    addForeignKeys:
      fks.length > 0
        ? fks.map((fk) => ({
            column: fk.column,
            refTable: fk.refTable,
            refColumn: fk.refColumn,
            onDelete: fk.onDelete,
            onUpdate: fk.onUpdate,
          }))
        : undefined,
  });

  const generate = async () => {
    const finals = new Set<string>();
    for (const r of rows!) {
      const final = r.newName.trim() || r.name;
      if (!r.drop && final) {
        if (finals.has(final)) {
          toastError(`新列名 ${final} 重复`);
          return;
        }
        finals.add(final);
      }
      if (r.name === "" && r.newName.trim() === "" && !r.drop) {
        toastError("新增列必须填写新列名");
        return;
      }
    }
    setBusy(true);
    try {
      const p = await api.previewTableRedesign(connId, buildRedesign());
      setPreview(p);
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!preview || preview.blocked) return;
    setBusy(true);
    try {
      await api.applyTableRedesign(connId, buildRedesign());
      toastSuccess("表重建完成");
      onApplied(newTable.trim() || table);
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const finalNames = rows!.map((r) => r.newName.trim() || r.name).filter(Boolean);

  return (
    <>
      <Modal title={`重新设计表 · ${table}`} onClose={onClose} width={940}
        footer={
          <>
            <button className="btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button className="btn primary" onClick={() => void generate()} disabled={busy}>
              <Wrench size={13} /> 生成重建 SQL 预览
            </button>
          </>
        }
      >
        <div className="op-form">
          <p className="muted small">
            重建通过"建新表 → 拷数据 → 删旧改名 → 重建索引/触发器"完成，单事务执行，
            任一步失败整体回滚，大数据量分块拷贝并实时回报进度；
            含 CHECK/COLLATE/生成列的表暂不支持（复合外键已支持）。
          </p>
          <label className="op-field">
            新表名（可选，留空保持不变；被外键/触发器/视图引用的表不可改名）
            <input value={newTable} onChange={(e) => setNewTable(e.target.value)} placeholder={table} />
          </label>
          <table className="designer-table">
            <thead>
              <tr>
                <th>当前列</th>
                <th>新列名（空=不变）</th>
                <th>类型</th>
                <th>非空</th>
                <th>主键</th>
                <th>默认值</th>
                <th>表达式</th>
                <th>删</th>
                <th>排序</th>
              </tr>
            </thead>
            <tbody>
              {rows!.map((row, i) => {
                const isNew = row.name === "";
                return (
                  <tr key={i} className={row.drop ? "row-dropped" : undefined}>
                    <td className="mono">{isNew ? "（新增）" : row.name}</td>
                    <td>
                      <input value={row.newName} onChange={(e) => updateRow(i, { newName: e.target.value })} placeholder={row.name} />
                    </td>
                    <td>
                      <input value={row.type} onChange={(e) => updateRow(i, { type: e.target.value })} />
                    </td>
                    <td className="cell-center">
                      <input type="checkbox" checked={row.notNull} disabled={row.drop} onChange={(e) => updateRow(i, { notNull: e.target.checked })} />
                    </td>
                    <td className="cell-center">
                      <input type="checkbox" checked={row.primaryKey} disabled={row.drop} onChange={(e) => updateRow(i, { primaryKey: e.target.checked })} />
                    </td>
                    <td>
                      <input value={row.default} disabled={row.drop} onChange={(e) => updateRow(i, { default: e.target.value })} placeholder="可空" />
                    </td>
                    <td className="cell-center">
                      <input type="checkbox" checked={row.defaultIsExpression} disabled={row.drop} onChange={(e) => updateRow(i, { defaultIsExpression: e.target.checked })} />
                    </td>
                    <td className="cell-center">
                      <input type="checkbox" checked={row.drop} disabled={isNew} onChange={(e) => updateRow(i, { drop: e.target.checked })} />
                    </td>
                    <td>
                      <button className="icon-btn" title="上移" disabled={i === 0} onClick={() => moveRow(i, -1)}>
                        <ArrowUp size={12} />
                      </button>
                      <button className="icon-btn" title="下移" disabled={i === rows!.length - 1} onClick={() => moveRow(i, 1)}>
                        <ArrowDown size={12} />
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <div>
            <button className="btn text" onClick={addRow}>
              <Plus size={13} /> 新增列
            </button>
          </div>

          <div className="fk-section">
            <div className="fk-header">
              <span>新增外键（现有外键将原样保留）</span>
              <button
                className="btn text"
                onClick={() =>
                  setFks((rs) => [
                    ...rs,
                    { key: fkSeq++, column: "", refTable: "", refColumn: "", onDelete: "", onUpdate: "" },
                  ])
                }
              >
                <Plus size={13} /> 添加
              </button>
            </div>
            {fks.length > 0 && (
              <table className="designer-table">
                <thead>
                  <tr>
                    <th>本表列</th>
                    <th>引用表</th>
                    <th>引用列（空 = 主键）</th>
                    <th>ON DELETE</th>
                    <th>ON UPDATE</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {fks.map((fk) => {
                    const refCols = refColumns[fk.refTable] ?? [];
                    return (
                      <tr key={fk.key}>
                        <td>
                          <select value={fk.column} onChange={(e) => updateFk(fk.key, { column: e.target.value })}>
                            <option value="">选择列…</option>
                            {finalNames.map((c) => (
                              <option key={c} value={c}>
                                {c}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <select
                            value={fk.refTable}
                            onChange={(e) => updateFk(fk.key, { refTable: e.target.value, refColumn: "" })}
                          >
                            <option value="">选择表…</option>
                            {tableNames.map((t) => (
                              <option key={t} value={t}>
                                {t}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <select value={fk.refColumn} onChange={(e) => updateFk(fk.key, { refColumn: e.target.value })}>
                            <option value="">（主键）</option>
                            {refCols.map((c) => (
                              <option key={c} value={c}>
                                {c}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <select value={fk.onDelete} onChange={(e) => updateFk(fk.key, { onDelete: e.target.value })}>
                            {FK_ACTIONS.map((a) => (
                              <option key={a} value={a}>
                                {a || "（默认）"}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <select value={fk.onUpdate} onChange={(e) => updateFk(fk.key, { onUpdate: e.target.value })}>
                            {FK_ACTIONS.map((a) => (
                              <option key={a} value={a}>
                                {a || "（默认）"}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <button className="icon-btn" title="删除" onClick={() => setFks((rs) => rs.filter((r) => r.key !== fk.key))}>
                            <Trash2 size={13} />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </Modal>
      {preview && (
        <SqlPreviewModal
          title="表重建 SQL 预览"
          danger
          preview={{
            sql: (preview.sql ?? []).join(";\n\n"),
            warnings: preview.warnings ?? [],
            blocked: preview.blocked ?? "",
          }}
          busy={busy}
          extra={
            busy && progress ? (
              <div className="redesign-progress">
                <div className="progress-text">
                  {progress.phase === "finalize"
                    ? "数据拷贝完成，正在重建索引/触发器（不可分块，可能耗时较长）…"
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
                      style={{
                        width: `${Math.min(100, (progress.copied / progress.total) * 100)}%`,
                      }}
                    />
                  </div>
                )}
              </div>
            ) : undefined
          }
          onClose={() => setPreview(null)}
          onConfirm={() => void apply()}
        />
      )}
    </>
  );
}
