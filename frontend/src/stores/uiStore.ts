import { create } from "zustand";

interface UiState {
  settingsOpen: boolean;
  setSettingsOpen: (open: boolean) => void;
  importOpen: boolean;
  setImportOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>((set) => ({
  settingsOpen: false,
  setSettingsOpen: (open) => set({ settingsOpen: open }),
  importOpen: false,
  setImportOpen: (open) => set({ importOpen: open }),
}));
