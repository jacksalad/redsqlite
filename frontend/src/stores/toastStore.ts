import { create } from "zustand";
import { getErrorMessage } from "../api/errors";

export type ToastKind = "error" | "success" | "info";

export interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastState {
  toasts: ToastItem[];
  push: (kind: ToastKind, message: string) => void;
  dismiss: (id: number) => void;
}

let nextId = 1;

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  push: (kind, message) => {
    const id = nextId++;
    set((s) => ({ toasts: [...s.toasts, { id, kind, message }] }));
    // 错误停留久一些，其余 3 秒自动消失
    const ttl = kind === "error" ? 6000 : 3000;
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
    }, ttl);
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

/** 便捷方法：在组件外报告错误。 */
export function toastError(error: unknown): void {
  useToastStore.getState().push("error", getErrorMessage(error));
}

export function toastSuccess(message: string): void {
  useToastStore.getState().push("success", message);
}

export function toastInfo(message: string): void {
  useToastStore.getState().push("info", message);
}
