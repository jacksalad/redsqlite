import type { HistoryEntry } from "../types";

const KEY = "redsqlite.query-history";
const MAX = 50;

export function loadHistory(): HistoryEntry[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function saveHistoryEntry(entry: HistoryEntry): void {
  const list = [entry, ...loadHistory().filter((e) => e.sql !== entry.sql)].slice(0, MAX);
  try {
    localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    // 忽略持久化失败（隐私模式等）
  }
}

export function clearHistory(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    // ignore
  }
}
