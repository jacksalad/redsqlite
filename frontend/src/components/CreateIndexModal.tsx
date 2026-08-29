import { useEffect, useState } from "react";
import * as api from "../api";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnInfo, CreateIndexPreview } from "../types";
import { Modal } from "./Modal";
import { SqlPreviewModal } from "./TableOpFlow";

/** 新建索引弹窗（V2.3）：选列 + 可选唯一；名称留空自动命名。 */
export function CreateIndexModal({
  connId,
  table,
  onClose,
  onCreated,
}: {
  connId: string;
  table: string;
  onClose: () => void;
  onCreated: (name: string) => void;
}) {
  const [columns, setColumns] = useState<ColumnInfo[] | null>(null);
  const [name, setName] = useState("");
  const [unique, setUnique] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [preview, setPreview] = useState<CreateIndexPreview | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .getTableColumns(connId, table)
      .then((cols) => alive && setColumns(cols ?? []))
      .catch((error) => {
        toastError(error);
        if (alive) onClose();
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connId, table]);

  const toggle = (col: string) =>
    setSelected((s) => (s.includes(col) ? s.filter((c) => c !== col) : [...s, col]));

  const generate = async () => {
    if (selected.length === 0) {
      toastError("至少选择一个索引列");
      return;
    }
    setBusy(true);
    try {
      const p = await api.previewCreateIndex(connId, {
        table,
        name: name.trim(),
        unique,
        columns: selected,
      });
      setPreview(p);
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!preview || preview.blocked) return;
    setBusy(true);
    try {
      const actual = await api.applyCreateIndex(connId, {
        table,
        name: name.trim(),
        unique,
        columns: selected,
      });
      toastSuccess(`索引 ${actual} 已创建`);
      onCreated(actual);
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Modal title={`新建索引 · ${table}`} onClose={onClose} width={460}
        footer={
          <>
            <button className="btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button className="btn primary" onClick={() => void generate()} disabled={busy || !columns}>
              生成 SQL 预览
            </button>
          </>
        }
      >
        <div className="op-form">
          <label className="op-field">
            索引名（留空自动命名）
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 idx_users_name" />
          </label>
          <label className="op-check">
            <input type="checkbox" checked={unique} onChange={(e) => setUnique(e.target.checked)} />
            唯一索引（UNIQUE）
          </label>
          <div className="op-field">
            索引列（{selected.length} 个）
            {columns === null ? (
              <p className="muted small">加载列中…</p>
            ) : (
              <div className="index-col-list">
                {(columns ?? []).map((c) => (
                  <label key={c.name} className="op-check">
                    <input type="checkbox" checked={selected.includes(c.name)} onChange={() => toggle(c.name)} />
                    <span className="mono">{c.name}</span>
                    <span className="muted small">{c.type || "ANY"}</span>
                  </label>
                ))}
              </div>
            )}
          </div>
        </div>
      </Modal>
      {preview && (
        <SqlPreviewModal
          title={`新建索引 · ${preview.name}`}
          preview={preview}
          busy={busy}
          onClose={() => setPreview(null)}
          onConfirm={() => void apply()}
        />
      )}
    </>
  );
}
