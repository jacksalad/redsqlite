import { useState } from "react";
import { Database, Plus, X } from "lucide-react";
import { useConnectionsStore } from "../stores/connectionsStore";
import { OpenDatabaseModal } from "./OpenDatabaseModal";

/** 数据库连接 Tab 条（V2.1 多库并存）。"+" 先弹最近列表（V3.0 UX 改进）。 */
export function ConnectionTabs() {
  const conns = useConnectionsStore((s) => s.conns);
  const activeId = useConnectionsStore((s) => s.activeId);
  const opening = useConnectionsStore((s) => s.opening);
  const activate = useConnectionsStore((s) => s.activate);
  const close = useConnectionsStore((s) => s.close);

  const [openDialog, setOpenDialog] = useState(false);

  return (
    <div className="conn-tabs" role="tablist">
      {conns.map((c) => (
        <div
          key={c.id}
          role="tab"
          aria-selected={c.id === activeId}
          tabIndex={0}
          className={`conn-tab ${c.id === activeId ? "active" : ""}`}
          onClick={() => activate(c.id)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              activate(c.id);
            }
          }}
          title={c.info.path}
        >
          <Database size={12} className="conn-tab-icon" />
          <span className="conn-tab-name">{c.info.fileName}</span>
          {c.info.readOnly && <span className="conn-tab-ro">只读</span>}
          <button
            className="conn-tab-close"
            onClick={(e) => {
              e.stopPropagation();
              void close(c.id);
            }}
            aria-label={`关闭 ${c.info.fileName}`}
          >
            <X size={11} />
          </button>
        </div>
      ))}
      <button
        className="conn-tab-add"
        disabled={opening}
        onClick={() => setOpenDialog(true)}
        title="打开另一个数据库"
      >
        <Plus size={12} />
      </button>

      {openDialog && <OpenDatabaseModal onClose={() => setOpenDialog(false)} />}
    </div>
  );
}
