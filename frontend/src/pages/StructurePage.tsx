import { useEffect, useState } from "react";
import { KeyRound, Link2, Pencil, Plus, RefreshCw, Trash2, Wrench } from "lucide-react";
import * as api from "../api";
import { getErrorMessage } from "../api/errors";
import { ContextMenu } from "../components/ContextMenu";
import { CreateIndexModal } from "../components/CreateIndexModal";
import { RedesignModal } from "../components/RedesignModal";
import { useTableOpFlow } from "../components/TableOpFlow";
import { useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import type { ColumnInfo, ForeignKeyInfo, TableOp, TableStructure, Tab } from "../types";

/** 表结构页：字段、索引、DDL、行数与可编辑性；支持可视化结构编辑（V2.0）。 */
export function StructurePage({ tab }: { tab: Tab }) {
  const connId = tab.connId;
  const table = tab.table!;
  const [structure, setStructure] = useState<TableStructure | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [menu, setMenu] = useState<{ x: number; y: number; column: ColumnInfo } | null>(null);
  const [fks, setFks] = useState<ForeignKeyInfo[] | null>(null);
  const [createIndexOpen, setCreateIndexOpen] = useState(false);
  const [redesignOpen, setRedesignOpen] = useState(false);

  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const renameTableTabs = useTabsStore((s) => s.renameTableTabs);
  const closeTableTabs = useTabsStore((s) => s.closeTableTabs);

  const flow = useTableOpFlow({
    connId,
    onApplied: (op: TableOp) => {
      void load();
      void refreshSchema(connId);
      if (op.kind === "renameTable" && op.newName) renameTableTabs(connId, op.table, op.newName);
      if (op.kind === "dropTable") closeTableTabs(connId, op.table);
    },
  });

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      setStructure(await api.getTableStructure(connId, table));
      api
        .getTableForeignKeys(connId, table)
        .then((list) => setFks(list ?? []))
        .catch(() => setFks([]));
    } catch (err) {
      setError(getErrorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [table]);

  const canEdit = !!structure && structure.editable && !structure.isView;

  return (
    <div className="page structure-page">
      <div className="page-toolbar">
        <div className="page-toolbar-group">
          <span className="page-title">{table}</span>
          {structure && (
            <>
              <span className={`ro-badge ${structure.editable ? "" : "on"}`}>
                {structure.isView ? "视图" : structure.editable ? "可编辑" : "只读"}
              </span>
              {!structure.isView && (
                <span className="muted small">约 {structure.rowCount.toLocaleString()} 行</span>
              )}
              <span className="muted small">
                定位字段：{structure.keyColumns?.join(", ") || "无"}
              </span>
            </>
          )}
        </div>
        <div className="page-toolbar-group">
          {canEdit && (
            <>
              <button
                className="btn"
                onClick={() => setRedesignOpen(true)}
                title="重新设计表（改类型/约束/列序/主键/新增外键，经表重建实现）"
              >
                <Wrench size={13} /> 重新设计
              </button>
              <button className="btn" onClick={() => flow.beginRenameTable(table)} title="重命名表">
                <Pencil size={13} /> 重命名表
              </button>
              <button
                className="btn danger"
                onClick={() => flow.confirmDropTable(table)}
                title="删除表"
              >
                <Trash2 size={13} /> 删除表
              </button>
            </>
          )}
          <button className="btn" onClick={() => void load()} disabled={loading}>
            <RefreshCw size={13} className={loading ? "spin" : ""} /> 刷新
          </button>
        </div>
      </div>

      {error && <div className="error-banner">{error}</div>}

      {structure && (
        <div className="structure-body">
          <section className="structure-section">
            <div className="structure-section-head">
              <h3>字段（{structure.columns?.length ?? 0}）</h3>
              {canEdit && (
                <button className="btn text" onClick={() => flow.beginAddColumn(table)}>
                  <Plus size={13} /> 添加字段
                </button>
              )}
            </div>
            <table className="kv-table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>名称</th>
                  <th>类型</th>
                  <th>NOT NULL</th>
                  <th>默认值</th>
                  <th>主键</th>
                </tr>
              </thead>
              <tbody>
                {(structure.columns ?? []).map((c) => (
                  <tr
                    key={c.name}
                    onContextMenu={(e) => {
                      if (!canEdit) return;
                      e.preventDefault();
                      setMenu({ x: e.clientX, y: e.clientY, column: c });
                    }}
                    className={canEdit ? "row-ctx" : undefined}
                  >
                    <td>{c.cid}</td>
                    <td className="mono">{c.name}</td>
                    <td>{c.type || "ANY"}</td>
                    <td>{c.notNull ? "是" : ""}</td>
                    <td className="mono">{formatDefault(c.defaultValue)}</td>
                    <td>{c.primaryKey ? "是" : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>

          {!structure.isView && (
            <section className="structure-section">
              <div className="structure-section-head">
                <h3>
                  <KeyRound size={13} /> 索引（{structure.indexes?.length ?? 0}）
                </h3>
                {canEdit && (
                  <button className="btn text" onClick={() => setCreateIndexOpen(true)}>
                    <Plus size={13} /> 新建索引
                  </button>
                )}
              </div>
              {(structure.indexes?.length ?? 0) === 0 ? (
                <p className="muted small">该表没有索引</p>
              ) : (
                <table className="kv-table">
                  <thead>
                    <tr>
                      <th>名称</th>
                      <th>列</th>
                      <th>唯一</th>
                      <th>来源</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(structure.indexes ?? []).map((i) => (
                      <tr key={i.name}>
                        <td className="mono">{i.name}</td>
                        <td className="mono">{i.columns?.join(", ") || "-"}</td>
                        <td>{i.unique ? "是" : ""}</td>
                        <td>{indexOrigin(i.origin)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          )}

          {!structure.isView && (fks?.length ?? 0) > 0 && (
            <section className="structure-section">
              <h3>
                <Link2 size={13} /> 外键（{fks?.length ?? 0}）
              </h3>
              <table className="kv-table">
                <thead>
                  <tr>
                    <th>字段</th>
                    <th>引用</th>
                    <th>ON DELETE</th>
                    <th>ON UPDATE</th>
                  </tr>
                </thead>
                <tbody>
                  {(fks ?? []).map((fk) => (
                    <tr key={`${fk.id}-${fk.seq}`}>
                      <td className="mono">{fk.column}</td>
                      <td className="mono">
                        {fk.refTable}
                        {fk.refColumn ? ` (${fk.refColumn})` : "（主键）"}
                      </td>
                      <td>{fk.onDelete}</td>
                      <td>{fk.onUpdate}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          )}

          <section className="structure-section">
            <h3>{structure.isView ? "视图定义" : "建表语句 (DDL)"}</h3>
            <pre className="ddl">{structure.ddl || "（无）"}</pre>
          </section>
        </div>
      )}

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            {
              label: "重命名字段",
              icon: <Pencil size={13} />,
              onClick: () => flow.beginRenameColumn(table, menu.column.name),
            },
            {
              label: "删除字段",
              icon: <Trash2 size={13} />,
              danger: true,
              onClick: () => flow.confirmDropColumn(table, menu.column.name),
            },
          ]}
        />
      )}
      {redesignOpen && (
        <RedesignModal
          connId={connId}
          table={table}
          onClose={() => setRedesignOpen(false)}
          onApplied={(newTable) => {
            setRedesignOpen(false);
            if (newTable !== table) renameTableTabs(connId, table, newTable);
            void load();
            void refreshSchema(connId);
          }}
        />
      )}
      {createIndexOpen && (
        <CreateIndexModal
          connId={connId}
          table={table}
          onClose={() => setCreateIndexOpen(false)}
          onCreated={() => {
            setCreateIndexOpen(false);
            void load();
            void refreshSchema(connId);
          }}
        />
      )}
      {flow.element}
    </div>
  );
}

function formatDefault(v: unknown): string {
  if (v === null || v === undefined) return "";
  return String(v);
}

function indexOrigin(origin: string): string {
  switch (origin) {
    case "c":
      return "CREATE INDEX";
    case "u":
      return "UNIQUE 约束";
    case "pk":
      return "主键";
    default:
      return origin;
  }
}
