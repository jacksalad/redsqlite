import { useEffect } from "react";
import { Database, FolderOpen, Lock, Trash2, X, Clock } from "lucide-react";
import { useConnectionsStore } from "../stores/connectionsStore";

/** 欢迎页：打开数据库 + 最近打开列表（文档 6.1）。 */
export function WelcomePage() {
  const open = useConnectionsStore((s) => s.open);
  const recent = useConnectionsStore((s) => s.recent);
  const loadRecent = useConnectionsStore((s) => s.loadRecent);
  const removeRecent = useConnectionsStore((s) => s.removeRecent);
  const clearRecent = useConnectionsStore((s) => s.clearRecent);
  const opening = useConnectionsStore((s) => s.opening);

  useEffect(() => {
    void loadRecent();
  }, [loadRecent]);

  return (
    <div className="welcome">
      <div className="welcome-card">
        <div className="welcome-logo">
          <Database size={40} strokeWidth={1.4} />
        </div>
        <h1>RedSQLite</h1>
        <p className="welcome-sub">轻量级 SQLite 桌面客户端</p>
        <button className="btn primary big" disabled={opening} onClick={() => void open()}>
          <FolderOpen size={16} />
          {opening ? "打开中…" : "打开数据库文件"}
        </button>
        <button className="btn big" disabled={opening} onClick={() => void open(undefined, true)}>
          <Lock size={15} />
          以只读方式打开
        </button>
        <p className="muted small">支持 .db / .sqlite / .sqlite3 文件</p>
      </div>

      {recent.length > 0 && (
        <div className="recent-panel">
          <div className="recent-header">
            <Clock size={13} />
            <span>最近打开</span>
            <button className="btn text danger-text" onClick={() => void clearRecent()}>
              <Trash2 size={12} /> 清空
            </button>
          </div>
          <ul className="recent-list">
            {recent.map((entry) => (
              <li key={entry.path} className="recent-item">
                <button className="recent-open" onClick={() => void open(entry.path)}>
                  <Database size={13} className="recent-icon" />
                  <span className="recent-name">{entry.fileName}</span>
                  <span className="recent-path" title={entry.path}>
                    {entry.path}
                  </span>
                </button>
                <button
                  className="icon-btn"
                  onClick={() => void removeRecent(entry.path)}
                  title="从列表移除"
                >
                  <X size={12} />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
