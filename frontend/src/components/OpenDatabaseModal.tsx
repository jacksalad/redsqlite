import { useEffect, useState } from "react";
import { Clock, Database, FolderOpen, X } from "lucide-react";
import { useConnectionsStore } from "../stores/connectionsStore";
import { Modal } from "./Modal";

/**
 * 打开数据库弹窗：先从最近打开列表选择，再浏览文件兜底（V3.0 UX 改进）。
 * 只读开关对两种打开方式同时生效；已在当前会话打开的库标记「已打开」，
 * 点击即切换到对应 Tab（同路径后端自动复用连接）。
 */
export function OpenDatabaseModal({ onClose }: { onClose: () => void }) {
  const open = useConnectionsStore((s) => s.open);
  const recent = useConnectionsStore((s) => s.recent);
  const loadRecent = useConnectionsStore((s) => s.loadRecent);
  const removeRecent = useConnectionsStore((s) => s.removeRecent);
  const conns = useConnectionsStore((s) => s.conns);
  const opening = useConnectionsStore((s) => s.opening);
  const [readOnly, setReadOnly] = useState(false);

  useEffect(() => {
    void loadRecent();
  }, [loadRecent]);

  const openPath = async (path?: string) => {
    const ok = await open(path, readOnly);
    if (ok) onClose(); // 文件对话框取消（ok=false）时保持弹窗
  };

  return (
    <Modal
      title="打开数据库"
      onClose={onClose}
      width={560}
      footer={
        <>
          <label className="op-check open-readonly" title="以只读方式打开（对最近列表与浏览文件均生效）">
            <input
              type="checkbox"
              checked={readOnly}
              onChange={(e) => setReadOnly(e.target.checked)}
              disabled={opening}
            />
            以只读方式打开
          </label>
          <button className="btn" onClick={onClose} disabled={opening}>
            取消
          </button>
          <button className="btn primary" onClick={() => void openPath()} disabled={opening}>
            <FolderOpen size={13} /> {opening ? "打开中…" : "浏览文件…"}
          </button>
        </>
      }
    >
      <div className="open-db-header">
        <Clock size={13} />
        <span>最近打开</span>
      </div>
      {recent.length === 0 ? (
        <p className="muted small">暂无最近打开的数据库，可点击下方「浏览文件…」选择。</p>
      ) : (
        <ul className="recent-list">
          {recent.map((entry) => {
            const opened = conns.find((c) => c.info.path === entry.path);
            return (
              <li key={entry.path} className="recent-item">
                <button
                  className="recent-open"
                  disabled={opening}
                  onClick={() => void openPath(entry.path)}
                  title={entry.path}
                >
                  <Database size={13} className="recent-icon" />
                  <span className="recent-name">{entry.fileName}</span>
                  <span className="recent-path" title={entry.path}>
                    {entry.path}
                  </span>
                  {opened && <span className="recent-opened-badge">已打开</span>}
                </button>
                <button
                  className="icon-btn"
                  onClick={() => void removeRecent(entry.path)}
                  title="从列表移除"
                >
                  <X size={12} />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Modal>
  );
}
