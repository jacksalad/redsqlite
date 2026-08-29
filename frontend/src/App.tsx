import { useEffect } from "react";
import { Toolbar } from "./components/Toolbar";
import { SchemaTree } from "./components/SchemaTree";
import { TabsBar } from "./components/TabsBar";
import { ConnectionTabs } from "./components/ConnectionTabs";
import { ToastHost } from "./components/ToastHost";
import { SettingsModal } from "./components/SettingsModal";
import { ImportWizard } from "./components/ImportWizard";
import { WelcomePage } from "./pages/WelcomePage";
import { TableDataPage } from "./pages/TableDataPage";
import { StructurePage } from "./pages/StructurePage";
import { QueryPage } from "./pages/QueryPage";
import { useConnectionsStore } from "./stores/connectionsStore";
import { useSettingsStore } from "./stores/settingsStore";
import { useTabsStore } from "./stores/tabsStore";
import { useUiStore } from "./stores/uiStore";
import { applyTheme } from "./utils/theme";

function ActivePage() {
  const tabs = useTabsStore((s) => s.tabs);
  const activeId = useTabsStore((s) => s.activeId);
  const active = tabs.find((t) => t.id === activeId);
  if (!active) return <div className="empty-tab-state">从左侧选择一个表，或点击「新建查询」</div>;
  if (active.kind === "table") return <TableDataPage key={active.id} tab={active} />;
  if (active.kind === "structure") return <StructurePage key={active.id} tab={active} />;
  return <QueryPage key={active.id} tab={active} />;
}

export default function App() {
  const conns = useConnectionsStore((s) => s.conns);
  const activeId = useConnectionsStore((s) => s.activeId);
  const loadSettings = useSettingsStore((s) => s.load);
  const loadRecent = useConnectionsStore((s) => s.loadRecent);
  const newSqlTab = useTabsStore((s) => s.newSqlTab);
  const importOpen = useUiStore((s) => s.importOpen);
  const setImportOpen = useUiStore((s) => s.setImportOpen);

  const hasActive = conns.length > 0 && !!activeId;
  const theme = useSettingsStore((s) => s.settings.theme);

  useEffect(() => {
    void loadSettings();
    void loadRecent();
  }, [loadSettings, loadRecent]);

  // 主题：立即应用；跟随系统时监听系统切换
  useEffect(() => {
    applyTheme(theme);
    if (theme !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = () => applyTheme("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [theme]);

  // Ctrl+T 在当前连接下新建查询 Tab
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && (e.key === "t" || e.key === "T")) {
        e.preventDefault();
        const conn = useConnectionsStore.getState().activeId;
        if (conn) newSqlTab(conn);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [newSqlTab]);

  return (
    <div className="app-root">
      <Toolbar />
      {conns.length > 0 && <ConnectionTabs />}
      <div className="app-body">
        {hasActive ? (
          <>
            <SchemaTree />
            <main className="content">
              <TabsBar />
              <ActivePage />
            </main>
          </>
        ) : (
          <main className="content">
            <WelcomePage />
          </main>
        )}
      </div>
      <ToastHost />
      <SettingsModal />
      {importOpen && activeId && (
        <ImportWizard connId={activeId} onClose={() => setImportOpen(false)} />
      )}
    </div>
  );
}
