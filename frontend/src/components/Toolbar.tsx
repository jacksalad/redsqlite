import {
  Database,
  DatabaseBackup,
  FolderOpen,
  XCircle,
  RefreshCw,
  FilePlus2,
  Upload,
  Settings as SettingsIcon,
} from "lucide-react";
import * as api from "../api";
import { useActiveConn, useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import { useUiStore } from "../stores/uiStore";
import { toastError, toastSuccess } from "../stores/toastStore";

/** 顶部工具栏（文档 5.2）。V2.1：作用于当前激活的数据库连接。 */
export function Toolbar() {
  const active = useActiveConn();
  const opening = useConnectionsStore((s) => s.opening);
  const open = useConnectionsStore((s) => s.open);
  const close = useConnectionsStore((s) => s.close);
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const newSqlTab = useTabsStore((s) => s.newSqlTab);
  const setSettingsOpen = useUiStore((s) => s.setSettingsOpen);
  const setImportOpen = useUiStore((s) => s.setImportOpen);

  const info = active?.info ?? null;

  const backup = async () => {
    if (!active) return;
    try {
      const base = (info?.fileName ?? "database").replace(/\.(db|sqlite3?|db3)$/i, "");
      const ts = new Date().toISOString().replace(/[-:T]/g, "").slice(0, 14);
      const path = await api.pickBackupPath(`${base}-backup-${ts}.db`);
      if (!path) return;
      const res = await api.backupDatabase(active.id, path);
      toastSuccess(`已备份到 ${res.path}（${Math.round(res.size / 1024)} KB，耗时 ${res.elapsedMs} ms）`);
    } catch (err) {
      toastError(err);
    }
  };

  return (
    <header className="toolbar">
      <div className="toolbar-brand">
        <Database size={16} className="brand-icon" />
        <span className="brand-name">RedSQLite</span>
      </div>

      <div className="toolbar-actions">
        <button className="btn primary" disabled={opening} onClick={() => void open()}>
          <FolderOpen size={14} /> 打开数据库
        </button>
        <button
          className="btn"
          disabled={!active}
          onClick={() => active && void close(active.id)}
          title="关闭当前数据库连接"
        >
          <XCircle size={14} /> 关闭
        </button>
        <button
          className="btn"
          disabled={!active}
          onClick={() => void refreshSchema()}
          title="刷新数据库结构"
        >
          <RefreshCw size={14} /> 刷新
        </button>
        <button
          className="btn"
          disabled={!active}
          onClick={() => void backup()}
          title="VACUUM INTO 在线备份当前数据库（只读连接也可用）"
        >
          <DatabaseBackup size={14} /> 备份
        </button>
        <button
          className="btn"
          disabled={!info || info.readOnly}
          onClick={() => setImportOpen(true)}
          title="导入 CSV / Excel 文件到表"
        >
          <Upload size={14} /> 导入数据
        </button>
        <button
          className="btn"
          disabled={!active}
          onClick={() => active && newSqlTab(active.id)}
          title="新建 SQL 查询（Ctrl+T）"
        >
          <FilePlus2 size={14} /> 新建查询
        </button>
        <button className="icon-btn settings-btn" onClick={() => setSettingsOpen(true)} title="设置">
          <SettingsIcon size={15} />
        </button>
      </div>

      <div className="toolbar-path" title={info?.path}>
        {info ? (
          <>
            <span className={`ro-badge ${info.readOnly ? "on" : ""}`}>
              {info.readOnly ? "只读" : "可写"}
            </span>
            {info.path}
            <span className="db-meta">
              SQLite {info.sqliteVersion} · {info.journalMode} · {Math.round(info.size / 1024)} KB
            </span>
          </>
        ) : (
          <span className="muted">未打开数据库</span>
        )}
      </div>
    </header>
  );
}
