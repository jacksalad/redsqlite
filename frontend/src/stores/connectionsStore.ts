import { create } from "zustand";
import * as api from "../api";
import type { DatabaseInfo, RecentEntry, SchemaInfo } from "../types";
import { toastError, toastInfo } from "./toastStore";
import { useTabsStore } from "./tabsStore";

/** 单个已打开的数据库连接（V2.1 多库 Tab）。 */
export interface Connection {
  id: string;
  info: DatabaseInfo;
  schema: SchemaInfo | null;
}

interface ConnectionsState {
  conns: Connection[];
  activeId: string | null;
  recent: RecentEntry[];
  opening: boolean;

  /** 打开数据库并激活；path 为空时弹出原生文件对话框。成功返回 true。 */
  open: (path?: string, readOnly?: boolean) => Promise<boolean>;
  /** 关闭连接并清理其全部 Tab。 */
  close: (id: string) => Promise<void>;
  activate: (id: string) => void;
  refreshSchema: (id?: string) => Promise<void>;
  loadRecent: () => Promise<void>;
  removeRecent: (path: string) => Promise<void>;
  clearRecent: () => Promise<void>;
}

export const useConnectionsStore = create<ConnectionsState>((set, get) => ({
  conns: [],
  activeId: null,
  recent: [],
  opening: false,

  open: async (path?: string, readOnly = false) => {
    let target = path ?? "";
    if (!target) {
      try {
        target = await api.pickDatabaseFile();
      } catch (error) {
        toastError(error);
        return false;
      }
      if (!target) return false; // 用户取消
    }

    set({ opening: true });
    try {
      const info = await api.openDatabase(target, readOnly);

      // 同路径复用：连接已打开则直接激活对应 Tab
      if (info.reused && get().conns.some((c) => c.id === info.connId)) {
        set({ activeId: info.connId, opening: false });
        toastInfo("该数据库已打开，已切换到对应标签页");
        return true;
      }

      let schema: SchemaInfo | null = null;
      try {
        schema = await api.getSchema(info.connId);
      } catch {
        // 结构加载失败不阻塞打开，可手动刷新
      }
      set((s) => ({
        conns: [...s.conns, { id: info.connId, info, schema }],
        activeId: info.connId,
        opening: false,
      }));
      void get().loadRecent();
      return true;
    } catch (error) {
      set({ opening: false });
      toastError(error);
      return false;
    }
  },

  close: async (id: string) => {
    try {
      await api.closeDatabase(id);
    } catch (error) {
      toastError(error);
    }
    set((s) => {
      const conns = s.conns.filter((c) => c.id !== id);
      const activeId =
        s.activeId === id ? (conns.length > 0 ? conns[conns.length - 1].id : null) : s.activeId;
      return { conns, activeId };
    });
    useTabsStore.getState().closeConnTabs(id);
  },

  activate: (id: string) => {
    set({ activeId: id });
    // 激活的连接若没有当前 Tab，则定位到该连接最近的 Tab（动作封装在 tabsStore）
    useTabsStore.getState().focusConn(id);
  },

  refreshSchema: async (id?: string) => {
    const connId = id ?? get().activeId;
    if (!connId || !get().conns.some((c) => c.id === connId)) return;
    try {
      const schema = await api.getSchema(connId);
      set((s) => ({
        conns: s.conns.map((c) => (c.id === connId ? { ...c, schema } : c)),
      }));
    } catch (error) {
      toastError(error);
    }
  },

  loadRecent: async () => {
    try {
      const recent = await api.listRecent();
      set({ recent: recent ?? [] });
    } catch {
      // 最近列表失败不打扰用户
    }
  },

  removeRecent: async (path: string) => {
    try {
      await api.removeRecent(path);
      await get().loadRecent();
    } catch (error) {
      toastError(error);
    }
  },

  clearRecent: async () => {
    try {
      await api.clearRecent();
      set({ recent: [] });
    } catch (error) {
      toastError(error);
    }
  },
}));

/** 当前激活的连接（无连接时为 null）。 */
export function useActiveConn(): Connection | null {
  return useConnectionsStore((s) => s.conns.find((c) => c.id === s.activeId) ?? null);
}
