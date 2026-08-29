import { create } from "zustand";
import * as api from "../api";
import type { Settings } from "../types";

/** 应用设置（独立小 store，V2.1 从 databaseStore 拆出）。 */
interface SettingsState {
  settings: Settings;
  loaded: boolean;
  load: () => Promise<void>;
  save: (next: Settings) => Promise<Settings | null>;
}

export const useSettingsStore = create<SettingsState>((set) => ({
  settings: {
    pageSize: 100,
    queryTimeoutMs: 30000,
    enableWAL: false,
    maxQueryRows: 10000,
    theme: "system",
  },
  loaded: false,
  load: async () => {
    try {
      const settings = await api.getSettings();
      set({ settings, loaded: true });
    } catch {
      set({ loaded: true });
    }
  },
  save: async (next: Settings) => {
    try {
      const saved = await api.updateSettings(next);
      set({ settings: saved });
      return saved;
    } catch (error) {
      const { toastError } = await import("./toastStore");
      toastError(error);
      return null;
    }
  },
}));
