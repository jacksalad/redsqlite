import { useEffect, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { Modal } from "./Modal";
import { SqlPreviewModal } from "./TableOpFlow";
import * as api from "../api";
import { useConnectionsStore } from "../stores/connectionsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnSpec, ForeignKeySpec, TableOpPreview } from "../types";

function emptyColumn(): ColumnSpec {
  return {
    name: "",
    type: "TEXT",
    notNull: false,
    primaryKey: false,
    autoIncrement: false,
    unique: false,
    default: "",
    defaultIsExpression: false,
    generated: "",
    generatedStored: false,
  };
}

interface FkRow extends ForeignKeySpec {
  key: number;
}

const FK_ACTIONS = ["", "NO ACTION", "RESTRICT", "SET NULL", "SET DEFAULT", "CASCADE"];
let fkSeq = 1;

/** 新建表设计器：列网格（含生成列）+ 外键定义 → 生成 SQL 预览 → 执行（V2.3 补齐外键/生成列）。 */
export function CreateTableModal({
  connId,
  onClose,
  onCreated,
}: {
  connId: string;
  onClose: () => void;
  onCreated: (table: string) => void;
}) {
  const [name, setName] = useState("");
  const [rows, setRows] = useState<ColumnSpec[]>([emptyColumn()]);
  const [fks, setFks] = useState<FkRow[]>([]);
  const [refColumns, setRefColumns] = useState<Record<string, string[]>>({});
  const [preview, setPreview] = useState<TableOpPreview | null>(null);
  const [busy, setBusy] = useState(false);

  const schema = useConnectionsStore((s) => s.conns.find((c) => c.id === connId)?.schema ?? null);
  const tableNames = (schema?.tables ?? []).map((t) => t.name);

  const updateRow = (i: number, patch: Partial<ColumnSpec>) =>
    setRows((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));

  // 加载外键引用表的列（供引用列下拉）
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

  const validate = (): string | null => {
    if (!name.trim()) return "表名不能为空";
    const seen = new Set<string>();
    for (const r of rows) {
      if (!r.name.trim()) return "存在未命名的字段";
      if (seen.has(r.name.trim())) return `字段名重复：${r.name}`;
      seen.add(r.name.trim());
    }
    for (const fk of fks) {
      if (!fk.column) return "外键未选择本表列";
      if (!fk.refTable) return "外键未选择引用表";
      if (!seen.has(fk.column)) return `外键列 ${fk.column} 未在字段中定义`;
    }
    return null;
  };

  const buildSpec = () => ({
    table: name.trim(),
    columns: rows.map((r) => ({
      ...r,
      name: r.name.trim(),
      type: r.type.trim(),
      default: r.default.trim(),
      generated: r.generated.trim(),
    })),
    foreignKeys:
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
    const problem = validate();
    if (problem) {
      toastError(problem);
      return;
    }
    setBusy(true);
    try {
      const p = await api.previewCreateTable(connId, buildSpec());
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
      await api.applyCreateTable(connId, buildSpec());
      toastSuccess(`表 ${name.trim()} 已创建`);
      onCreated(name.trim());
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const updateFk = (key: number, patch: Partial<FkRow>) =>
    setFks((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  return (
    <>
      <Modal title="新建表" onClose={onClose} width={920}
        footer={
          <>
            <button className="btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button className="btn primary" onClick={() => void generate()} disabled={busy}>
              生成 SQL 预览
            </button>
          </>
        }
      >
        <div className="op-form">
          <label className="op-field">
            表名
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如 products"
            />
          </label>
          <table className="designer-table">
            <thead>
              <tr>
                <th>字段名</th>
                <th>类型</th>
                <th title="NOT NULL">非空</th>
                <th title="PRIMARY KEY">主键</th>
                <th title="AUTOINCREMENT（仅单 INTEGER 主键）">自增</th>
                <th title="UNIQUE">唯一</th>
                <th>默认值</th>
                <th title="默认值按表达式处理">表达式</th>
                <th title="生成列表达式（留空为普通列）">生成列</th>
                <th title="STORED 生成列（默认 VIRTUAL）">存储</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, i) => {
                const generated = row.generated.trim() !== "";
                return (
                  <tr key={i}>
                    <td>
                      <input value={row.name} onChange={(e) => updateRow(i, { name: e.target.value })} />
                    </td>
                    <td>
                      <input value={row.type} onChange={(e) => updateRow(i, { type: e.target.value })} />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.notNull}
                        onChange={(e) => updateRow(i, { notNull: e.target.checked })}
                      />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.primaryKey}
                        disabled={generated}
                        onChange={(e) => {
                          const pk = e.target.checked;
                          const patch: Partial<ColumnSpec> = { primaryKey: pk };
                          if (!pk) patch.autoIncrement = false;
                          updateRow(i, patch);
                        }}
                      />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.autoIncrement}
                        disabled={!row.primaryKey || generated}
                        onChange={(e) => updateRow(i, { autoIncrement: e.target.checked })}
                      />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.unique}
                        disabled={generated}
                        onChange={(e) => updateRow(i, { unique: e.target.checked })}
                      />
                    </td>
                    <td>
                      <input
                        value={row.default}
                        disabled={generated}
                        onChange={(e) => updateRow(i, { default: e.target.value })}
                        placeholder="可空"
                      />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.defaultIsExpression}
                        disabled={generated}
                        onChange={(e) => updateRow(i, { defaultIsExpression: e.target.checked })}
                      />
                    </td>
                    <td>
                      <input
                        className="mono"
                        value={row.generated}
                        onChange={(e) => updateRow(i, { generated: e.target.value })}
                        placeholder="如 a * 2"
                      />
                    </td>
                    <td className="cell-center">
                      <input
                        type="checkbox"
                        checked={row.generatedStored}
                        disabled={!generated}
                        onChange={(e) => updateRow(i, { generatedStored: e.target.checked })}
                      />
                    </td>
                    <td>
                      <button
                        className="icon-btn"
                        title="删除该行"
                        disabled={rows.length === 1}
                        onClick={() => setRows((rs) => rs.filter((_, idx) => idx !== i))}
                      >
                        <Trash2 size={13} />
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <div>
            <button className="btn text" onClick={() => setRows((rs) => [...rs, emptyColumn()])}>
              <Plus size={13} /> 添加字段
            </button>
          </div>

          <div className="fk-section">
            <div className="fk-header">
              <span>外键</span>
              <button
                className="btn text"
                onClick={() =>
                  setFks((rs) => [
                    ...rs,
                    { key: fkSeq++, column: "", refTable: "", refColumn: "", onDelete: "", onUpdate: "" },
                  ])
                }
              >
                <Plus size={13} /> 添加外键
              </button>
            </div>
            {fks.length === 0 ? (
              <p className="muted small">暂无外键（可选）</p>
            ) : (
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
                    const colNames = rows.map((r) => r.name.trim()).filter(Boolean);
                    const refCols = refColumns[fk.refTable] ?? [];
                    return (
                      <tr key={fk.key}>
                        <td>
                          <select value={fk.column} onChange={(e) => updateFk(fk.key, { column: e.target.value })}>
                            <option value="">选择列…</option>
                            {colNames.map((c) => (
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
                          <select
                            value={fk.refColumn}
                            onChange={(e) => updateFk(fk.key, { refColumn: e.target.value })}
                          >
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
                          <button
                            className="icon-btn"
                            title="删除该外键"
                            onClick={() => setFks((rs) => rs.filter((r) => r.key !== fk.key))}
                          >
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

          <p className="muted small">
            复合主键生成表级 PRIMARY KEY；自增仅支持单个 INTEGER 主键；生成列不支持主键/唯一/默认值。执行前可预览完整 SQL。
          </p>
        </div>
      </Modal>
      {preview && (
        <SqlPreviewModal
          preview={preview}
          busy={busy}
          onClose={() => setPreview(null)}
          onConfirm={() => void apply()}
        />
      )}
    </>
  );
}
