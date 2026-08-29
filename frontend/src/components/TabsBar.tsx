import { Code2, Table2, Info, X } from "lucide-react";
import { useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import type { Tab } from "../types";

function TabIcon({ tab }: { tab: Tab }) {
  if (tab.kind === "sql") return <Code2 size={13} />;
  if (tab.kind === "structure") return <Info size={13} />;
  return <Table2 size={13} />;
}

/** Tab 标签栏（只显示当前激活连接的 Tab，V2.1）。 */
export function TabsBar() {
  const activeConnId = useConnectionsStore((s) => s.activeId);
  const allTabs = useTabsStore((s) => s.tabs);
  const activeId = useTabsStore((s) => s.activeId);
  const activate = useTabsStore((s) => s.activate);
  const closeTab = useTabsStore((s) => s.closeTab);

  const tabs = allTabs.filter((t) => t.connId === activeConnId);
  if (tabs.length === 0) return null;

  return (
    <div className="tabs-bar" role="tablist">
      {tabs.map((tab) => (
        <div
          key={tab.id}
          role="tab"
          aria-selected={tab.id === activeId}
          tabIndex={0}
          className={`tab ${tab.id === activeId ? "active" : ""}`}
          onClick={() => activate(tab.id)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              activate(tab.id);
            }
          }}
          onMouseDown={(e) => {
            if (e.button === 1) {
              // 中键关闭
              e.preventDefault();
              closeTab(tab.id);
            }
          }}
        >
          <TabIcon tab={tab} />
          <span className="tab-title">{tab.title}</span>
          <button
            className="tab-close"
            onClick={(e) => {
              e.stopPropagation();
              closeTab(tab.id);
            }}
            aria-label={`关闭 ${tab.title}`}
          >
            <X size={12} />
          </button>
        </div>
      ))}
    </div>
  );
}
