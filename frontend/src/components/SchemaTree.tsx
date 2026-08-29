import { useState, type ReactNode } from "react";
import {
  ArrowRightLeft,
  ChevronDown,
  ChevronRight,
  Database,
  Eye,
  Info,
  KeyRound,
  Pencil,
  Plus,
  Table2,
  Trash2,
  Zap,
} from "lucide-react";
import * as api from "../api";
import { useActiveConn, useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import { toastError } from "../stores/toastStore";
import { ContextMenu } from "./ContextMenu";
import { CreateTableModal } from "./CreateTableModal";
import { CreateIndexModal } from "./CreateIndexModal";
import { DdlModal } from "./DdlModal";
import { Modal } from "./Modal";
import { useTableOpFlow } from "./TableOpFlow";
import { TransferWizard } from "./TransferWizard";
import { ViewDesignerModal } from "./ViewDesignerModal";
import type { IndexInfo, TableOp, TriggerInfo, ViewOpPreview } from "../types";

interface DdlView {
  title: string;
  sql: string;
  meta?: string;
  deleteLabel?: string;
  onDelete?: () => Promise<void>;
}

/** 左侧数据库对象树：表/视图/索引/触发器，支持新建表与结构操作（V2.0）。 */
export function SchemaTree() {
  const conn = useActiveConn();
  const info = conn?.info ?? null;
  const schema = conn?.schema ?? null;
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const connId = conn?.id ?? "";
  const openTable = useTabsStore((s) => s.openTable);
  const openStructure = useTabsStore((s) => s.openStructure);
  const renameTableTabs = useTabsStore((s) => s.renameTableTabs);
  const closeTableTabs = useTabsStore((s) => s.closeTableTabs);

  const [sections, setSections] = useState<Record<string, boolean>>({
    tables: true,
    views: true,
    indexes: false,
    triggers: false,
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [createIndexFor, setCreateIndexFor] = useState<string | null>(null);
  const [ddlView, setDdlView] = useState<DdlView | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; table: string } | null>(null);
  /** 视图设计器（V3.0）：view 为空是新建，非空是编辑指定视图。 */
  const [viewDesigner, setViewDesigner] = useState<{ view?: string } | null>(null);
  const [viewMenu, setViewMenu] = useState<{ x: number; y: number; view: string } | null>(null);
  const [dropViewConfirm, setDropViewConfirm] = useState<{ view: string; preview: ViewOpPreview } | null>(null);
  const [dropViewBusy, setDropViewBusy] = useState(false);
  /** 跨连接传输向导（V3.0）：要传输的源表。 */
  const [transferTable, setTransferTable] = useState<string | null>(null);

  const writable = !!info && !info.readOnly;

  const flow = useTableOpFlow({
    connId,
    onApplied: (op: TableOp) => {
      void refreshSchema(connId);
      if (op.kind === "renameTable" && op.newName) renameTableTabs(connId, op.table, op.newName);
      if (op.kind === "dropTable") closeTableTabs(connId, op.table);
    },
  });

  if (!info || !schema) return null;

  const toggle = (key: string) => setSections((s) => ({ ...s, [key]: !s[key] }));

  const viewTrigger = (tr: TriggerInfo) => {
    setDdlView({
      title: `触发器 · ${tr.name}`,
      sql: tr.sql || "",
      meta: `所属表：${tr.table}`,
      deleteLabel: "删除触发器",
      onDelete: writable
        ? async () => {
            await api.dropTrigger(connId, tr.name);
            await refreshSchema();
          }
        : undefined,
    });
  };

  const viewIndex = (idx: IndexInfo) => {
    const manual = !!idx.sql;
    setDdlView({
      title: `索引 · ${idx.name}`,
      sql: idx.sql || "",
      meta: `所属表：${idx.table}${manual ? "" : "（该索引由表约束自动创建）"}`,
      deleteLabel: "删除索引",
      onDelete:
        writable && manual
          ? async () => {
              await api.dropIndex(connId, idx.name);
              await refreshSchema();
            }
          : undefined,
    });
  };

  /** 删除视图：先预检（含引用警告）再确认执行。 */
  const confirmDropView = async (view: string) => {
    try {
      const preview = await api.previewDropView(connId, view);
      if (preview.blocked) {
        toastError(preview.blocked);
        return;
      }
      setDropViewConfirm({ view, preview });
    } catch (err) {
      toastError(err);
    }
  };

  const doDropView = async () => {
    if (!dropViewConfirm) return;
    setDropViewBusy(true);
    try {
      await api.dropView(connId, dropViewConfirm.view);
      closeTableTabs(connId, dropViewConfirm.view);
      setDropViewConfirm(null);
      await refreshSchema(connId);
    } catch (err) {
      toastError(err);
    } finally {
      setDropViewBusy(false);
    }
  };

  return (
    <aside className="schema-tree">
      <div className="tree-root" title={info.path}>
        <Database size={14} className="tree-root-icon" />
        <span className="tree-root-name">{info.fileName}</span>
      </div>

      <Section
        title="表"
        count={schema.tables?.length ?? 0}
        open={sections.tables}
        onToggle={() => toggle("tables")}
        action={
          writable ? (
            <button
              className="icon-btn tree-item-action"
              title="新建表"
              onClick={() => setCreateOpen(true)}
            >
              <Plus size={12} />
            </button>
          ) : undefined
        }
      >
        {(schema.tables ?? []).map((t) => (
          <div
            key={t.name}
            className="tree-item-row"
            onContextMenu={(e) => {
              if (!writable) return;
              e.preventDefault();
              setMenu({ x: e.clientX, y: e.clientY, table: t.name });
            }}
          >
            <button className="tree-item" onClick={() => openTable(connId, t.name)} title={`浏览 ${t.name}`}>
              <Table2 size={13} className="tree-item-icon" />
              <span className="tree-item-label">{t.name}</span>
            </button>
            <button
              className="icon-btn tree-item-action"
              onClick={() => openStructure(connId, t.name)}
              title={`查看 ${t.name} 结构`}
            >
              <Info size={12} />
            </button>
          </div>
        ))}
        {(schema.tables?.length ?? 0) === 0 && <div className="tree-empty">（无表）</div>}
      </Section>

      <Section
        title="视图"
        count={schema.views?.length ?? 0}
        open={sections.views}
        onToggle={() => toggle("views")}
        action={
          writable ? (
            <button
              className="icon-btn tree-item-action"
              title="新建视图"
              onClick={() => setViewDesigner({})}
            >
              <Plus size={12} />
            </button>
          ) : undefined
        }
      >
        {(schema.views ?? []).map((v) => (
          <div
            key={v.name}
            className="tree-item-row"
            onContextMenu={(e) => {
              if (!writable) return;
              e.preventDefault();
              setViewMenu({ x: e.clientX, y: e.clientY, view: v.name });
            }}
          >
            <button className="tree-item" onClick={() => openTable(connId, v.name)} title={`浏览 ${v.name}`}>
              <Eye size={13} className="tree-item-icon dim" />
              <span className="tree-item-label">{v.name}</span>
            </button>
            <button
              className="icon-btn tree-item-action"
              onClick={() => openStructure(connId, v.name)}
              title={`查看 ${v.name} 定义`}
            >
              <Info size={12} />
            </button>
          </div>
        ))}
        {(schema.views?.length ?? 0) === 0 && <div className="tree-empty">（无视图）</div>}
      </Section>

      <Section
        title="索引"
        count={schema.indexes?.length ?? 0}
        open={sections.indexes}
        onToggle={() => toggle("indexes")}
      >
        {(schema.indexes ?? []).map((i) => (
          <div key={i.name} className="tree-item-row">
            <button
              className="tree-item static"
              style={{ width: "100%" }}
              onClick={() => viewIndex(i)}
              title={i.sql || `${i.name}（自动索引）`}
            >
              <KeyRound size={13} className="tree-item-icon dim" />
              <span className="tree-item-label">{i.name}</span>
              {i.table && <span className="tree-item-sub">{i.table}</span>}
            </button>
          </div>
        ))}
        {(schema.indexes?.length ?? 0) === 0 && <div className="tree-empty">（无索引）</div>}
      </Section>

      <Section
        title="触发器"
        count={schema.triggers?.length ?? 0}
        open={sections.triggers}
        onToggle={() => toggle("triggers")}
      >
        {(schema.triggers ?? []).map((tr) => (
          <div key={tr.name} className="tree-item-row">
            <button
              className="tree-item static"
              style={{ width: "100%" }}
              onClick={() => viewTrigger(tr)}
              title={tr.sql || tr.name}
            >
              <Zap size={13} className="tree-item-icon dim" />
              <span className="tree-item-label">{tr.name}</span>
              <span className="tree-item-sub">{tr.table}</span>
            </button>
          </div>
        ))}
        {(schema.triggers?.length ?? 0) === 0 && <div className="tree-empty">（无触发器）</div>}
      </Section>

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "浏览数据", onClick: () => openTable(connId, menu.table) },
            { label: "查看结构", onClick: () => openStructure(connId, menu.table) },
            {
              label: "新建索引",
              icon: <Plus size={13} />,
              onClick: () => setCreateIndexFor(menu.table),
            },
            {
              label: "传输到其他库…",
              icon: <ArrowRightLeft size={13} />,
              onClick: () => setTransferTable(menu.table),
            },
            {
              label: "重命名表",
              icon: <Pencil size={13} />,
              onClick: () => flow.beginRenameTable(menu.table),
            },
            {
              label: "删除表",
              icon: <Trash2 size={13} />,
              danger: true,
              onClick: () => flow.confirmDropTable(menu.table),
            },
          ]}
        />
      )}

      {viewMenu && (
        <ContextMenu
          x={viewMenu.x}
          y={viewMenu.y}
          onClose={() => setViewMenu(null)}
          items={[
            { label: "浏览数据", onClick: () => openTable(connId, viewMenu.view) },
            { label: "查看定义", onClick: () => openStructure(connId, viewMenu.view) },
            {
              label: "编辑视图",
              icon: <Pencil size={13} />,
              onClick: () => setViewDesigner({ view: viewMenu.view }),
            },
            {
              label: "删除视图",
              icon: <Trash2 size={13} />,
              danger: true,
              onClick: () => void confirmDropView(viewMenu.view),
            },
          ]}
        />
      )}

      {createOpen && (
        <CreateTableModal
          connId={connId}
          onClose={() => setCreateOpen(false)}
          onCreated={(name) => {
            setCreateOpen(false);
            void refreshSchema(connId);
            openStructure(connId, name);
          }}
        />
      )}
      {createIndexFor && (
        <CreateIndexModal
          connId={connId}
          table={createIndexFor}
          onClose={() => setCreateIndexFor(null)}
          onCreated={() => {
            setCreateIndexFor(null);
            void refreshSchema(connId);
          }}
        />
      )}
      {ddlView && (
        <DdlModal
          title={ddlView.title}
          sql={ddlView.sql}
          meta={ddlView.meta}
          deleteLabel={ddlView.deleteLabel}
          onDelete={ddlView.onDelete}
          onClose={() => setDdlView(null)}
        />
      )}

      {viewDesigner && (
        <ViewDesignerModal
          connId={connId}
          view={viewDesigner.view}
          onClose={() => setViewDesigner(null)}
          onApplied={(name, oldName) => {
            setViewDesigner(null);
            // 视图重命名后同步相关 Tab（与表重命名同一套联动）
            if (oldName && name !== oldName) renameTableTabs(connId, oldName, name);
          }}
        />
      )}

      {dropViewConfirm && (
        <Modal
          title={`删除视图 · ${dropViewConfirm.view}`}
          onClose={() => setDropViewConfirm(null)}
          width={520}
          footer={
            <>
              <button className="btn" onClick={() => setDropViewConfirm(null)} disabled={dropViewBusy}>
                取消
              </button>
              <button
                className="btn danger"
                onClick={() => void doDropView()}
                disabled={dropViewBusy}
              >
                {dropViewBusy ? "删除中…" : "删除"}
              </button>
            </>
          }
        >
          <p>确定删除视图 <span className="mono">{dropViewConfirm.view}</span> 吗？</p>
          {(dropViewConfirm.preview.warnings ?? []).length > 0 && (
            <ul className="op-warnings">
              {(dropViewConfirm.preview.warnings ?? []).map((w, i) => (
                <li key={i}>{w}</li>
              ))}
            </ul>
          )}
          <pre className="ddl sql-preview">{(dropViewConfirm.preview.sql ?? []).join(";\n")}</pre>
        </Modal>
      )}
      {transferTable && (
        <TransferWizard
          connId={connId}
          sourceTable={transferTable}
          onClose={() => setTransferTable(null)}
        />
      )}
      {flow.element}
    </aside>
  );
}

/** 树分区（表/视图/索引/触发器）。必须定义在模块级：写在组件体内会让
 *  React 每次渲染都视为新组件类型，整棵子树 unmount/remount。 */
function Section({
  title,
  count,
  action,
  open,
  onToggle,
  children,
}: {
  title: string;
  count: number;
  action?: ReactNode;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <div className="tree-section">
      <div className="tree-section-row">
        <button className="tree-section-header" onClick={onToggle}>
          {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
          <span className="tree-section-title">{title}</span>
          <span className="tree-count">{count}</span>
        </button>
        {action}
      </div>
      {open && <div className="tree-children">{children}</div>}
    </div>
  );
}
