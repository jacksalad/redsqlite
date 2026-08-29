import { useEffect, useState } from "react";
import * as api from "../api";
import { getErrorMessage } from "../api/errors";
import { Modal } from "./Modal";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnInfo } from "../types";

interface InsertRowModalProps {
  connId: string;
  table: string;
  onClose: () => void;
  onInserted: () => void;
}

/** 新增行表单（文档 6.4：自增主键留空、支持 NULL）。 */
export function InsertRowModal({ connId, table, onClose, onInserted }: InsertRowModalProps) {
  const [columns, setColumns] = useState<ColumnInfo[] | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [nulls, setNulls] = useState<Record<string, boolean>>({});
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    api
      .getTableStructure(connId, table)
      .then((s) => {
        setColumns(s.columns ?? []);
        const init: Record<string, string> = {};
        for (const c of s.columns ?? []) {
          init[c.name] = "";
        }
        setValues(init);
      })
      .catch((err) => setError(getErrorMessage(err)));
  }, [table]);

  /** INTEGER PRIMARY KEY 通常为自增列，默认留空（后端省略该列）。 */
  const isAutoIncrement = (c: ColumnInfo) =>
    c.primaryKey && /integer/i.test(c.type);

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const payload: Record<string, unknown> = {};
      for (const c of columns ?? []) {
        if (isAutoIncrement(c)) continue; // 留空让 SQLite 自增
        const text = (values[c.name] ?? "").trim();
        if (nulls[c.name]) {
          if (!c.notNull) payload[c.name] = null;
          continue;
        }
        if (text === "") {
          if (c.notNull) {
            setError(`字段 "${c.name}" 为 NOT NULL，请填写值`);
            setSubmitting(false);
            return;
          }
          continue; // 空文本且允许 NULL：省略，交给默认值
        }
        payload[c.name] = parseByType(text, c.type);
      }
      await api.insertRow(connId, table, payload);
      toastSuccess("已插入 1 行");
      onInserted();
      onClose();
    } catch (err) {
      setError(getErrorMessage(err));
      toastError(err);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title={`向 "${table}" 插入新行`}
      onClose={onClose}
      width={560}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            取消
          </button>
          <button className="btn primary" disabled={submitting || !columns} onClick={() => void submit()}>
            {submitting ? "插入中…" : "插入"}
          </button>
        </>
      }
    >
      {error && <div className="error-banner">{error}</div>}
      {!columns ? (
        <p className="muted">加载字段中…</p>
      ) : (
        <div className="insert-form">
          {columns.map((c) => (
            <div key={c.name} className="insert-row">
              <label className="insert-label" title={c.type}>
                <span className="insert-col-name">{c.name}</span>
                <span className="insert-col-type">
                  {c.type || "ANY"}
                  {c.primaryKey ? " · PK" : ""}
                  {c.notNull ? " · NOT NULL" : ""}
                  {isAutoIncrement(c) ? " · 自增（留空）" : ""}
                </span>
              </label>
              <input
                value={values[c.name] ?? ""}
                disabled={isAutoIncrement(c) || nulls[c.name]}
                placeholder={isAutoIncrement(c) ? "自动生成" : ""}
                onChange={(e) => setValues((v) => ({ ...v, [c.name]: e.target.value }))}
              />
              <label className="null-toggle" title="写入 NULL">
                <input
                  type="checkbox"
                  disabled={c.notNull || isAutoIncrement(c)}
                  checked={nulls[c.name] ?? false}
                  onChange={(e) => setNulls((n) => ({ ...n, [c.name]: e.target.checked }))}
                />
                NULL
              </label>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}

function parseByType(text: string, type: string): string | number {
  const t = type.toUpperCase();
  // 超出 JS 安全整数范围的整数字符串原样回传，由 SQLite 亲和性转换（V3.0）
  const safeInt = (s: string): number | string => {
    const n = Number.parseInt(s, 10);
    return Number.isSafeInteger(n) ? n : s;
  };
  if (/INT/.test(t) && /^-?\d+$/.test(text)) return safeInt(text);
  if (/(REAL|FLOA|DOUB|NUM|DEC)/.test(t) && /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(text)) {
    return Number.parseFloat(text);
  }
  // 动态类型：数字样式的文本也转为数字，符合 SQLite 亲和性
  if (/^-?\d+$/.test(text)) return safeInt(text);
  if (/^-?\d+\.\d+$/.test(text)) return Number.parseFloat(text);
  return text;
}
