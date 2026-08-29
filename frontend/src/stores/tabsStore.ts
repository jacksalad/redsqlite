import { create } from "zustand";
import type { Tab, TabKind } from "../types";

interface TabsState {
  tabs: Tab[];
  activeId: string | null;

  openTable: (connId: string, name: string) => void;
  openStructure: (connId: string, name: string) => void;
  newSqlTab: (connId: string) => void;
  activate: (id: string) => void;
  closeTab: (id: string) => void;
  /** 连接激活时把 Tab 焦点切到该连接最近的 Tab（无则置空）。 */
  focusConn: (connId: string) => void;
  /** 更新某个 Tab 的 SQL 文本（仅 sql 类型）。 */
  setTabSql: (id: string, sql: string) => void;
  /** 表重命名后同步相关 Tab（V2.0 结构编辑联动；限定连接，避免跨库同名表误改）。 */
  renameTableTabs: (connId: string, oldName: string, newName: string) => void;
  /** 表删除后关闭相关 Tab。 */
  closeTableTabs: (connId: string, name: string) => void;
  /** 连接关闭后清理其全部 Tab（V2.1）。 */
  closeConnTabs: (connId: string) => void;
}

let nextSeq = 1;

function findExisting(tabs: Tab[], connId: string, kind: TabKind, table?: string): Tab | undefined {
  return tabs.find((t) => t.connId === connId && t.kind === kind && t.table === table);
}

function dropTabs(tabs: Tab[], activeId: string | null, keep: (t: Tab) => boolean) {
  const rest = tabs.filter(keep);
  const activeGone = activeId === null || !rest.some((t) => t.id === activeId);
  const nextActive = activeGone
    ? rest.length > 0
      ? rest[rest.length - 1].id
      : null
    : activeId;
  return { tabs: rest, activeId: nextActive };
}

export const useTabsStore = create<TabsState>((set, get) => ({
  tabs: [],
  activeId: null,

  openTable: (connId, name) => {
    const existing = findExisting(get().tabs, connId, "table", name);
    if (existing) {
      set({ activeId: existing.id });
      return;
    }
    const tab: Tab = {
      id: `table-${connId}-${name}-${nextSeq++}`,
      connId,
      kind: "table",
      title: name,
      table: name,
    };
    set((s) => ({ tabs: [...s.tabs, tab], activeId: tab.id }));
  },

  openStructure: (connId, name) => {
    const existing = findExisting(get().tabs, connId, "structure", name);
    if (existing) {
      set({ activeId: existing.id });
      return;
    }
    const tab: Tab = {
      id: `struct-${connId}-${name}-${nextSeq++}`,
      connId,
      kind: "structure",
      title: `${name} · 结构`,
      table: name,
    };
    set((s) => ({ tabs: [...s.tabs, tab], activeId: tab.id }));
  },

  newSqlTab: (connId) => {
    const n = get().tabs.filter((t) => t.connId === connId && t.kind === "sql").length + 1;
    const tab: Tab = {
      id: `sql-${connId}-${nextSeq++}`,
      connId,
      kind: "sql",
      title: `查询 ${n}`,
      sql: "",
    };
    set((s) => ({ tabs: [...s.tabs, tab], activeId: tab.id }));
  },

  activate: (id) => set({ activeId: id }),

  closeTab: (id) =>
    set((s) => dropTabs(s.tabs, s.activeId, (t) => t.id !== id)),

  focusConn: (connId) =>
    set((s) => {
      const active = s.tabs.find((t) => t.id === s.activeId);
      if (active && active.connId === connId) return {};
      const last = [...s.tabs].reverse().find((t) => t.connId === connId);
      return { activeId: last ? last.id : null };
    }),

  setTabSql: (id, sql) =>
    set((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? { ...t, sql } : t)) })),

  renameTableTabs: (connId, oldName, newName) =>
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.connId === connId && t.table === oldName
          ? { ...t, table: newName, title: t.kind === "structure" ? `${newName} · 结构` : newName }
          : t,
      ),
    })),

  closeTableTabs: (connId, name) =>
    set((s) => dropTabs(s.tabs, s.activeId, (t) => !(t.connId === connId && t.table === name))),

  closeConnTabs: (connId) =>
    set((s) => dropTabs(s.tabs, s.activeId, (t) => t.connId !== connId)),
}));
