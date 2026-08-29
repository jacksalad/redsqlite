import { useEffect, useMemo, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { sql, SQLite } from "@codemirror/lang-sql";
import { oneDark } from "@codemirror/theme-one-dark";
import * as api from "../api";
import { Modal } from "./Modal";
import { useConnectionsStore } from "../stores/connectionsStore";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ViewOpPreview } from "../types";
import { useResolvedTheme } from "../utils/theme";

/** 视图设计器弹窗（V3.0）：编辑 SELECT 主体 + 后端 SQL 预览确认。
 * view 传入视图名进入编辑模式（OldName 回填，可重命名）；为空是新建。 */
export function ViewDesignerModal({
  connId,
  view,
  onClose,
  onApplied,
}: {
  connId: string;
  view?: string;
  onClose: () => void;
  onApplied: (name: string, oldName: string) => void;
}) {
  const editing = Boolean(view);
  const resolvedTheme = useResolvedTheme();
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);
  const schemaVersion = useConnectionsStore(
    (s) => s.conns.find((c) => c.id === connId)?.schema,
  );

  const [name, setName] = useState(view ?? "");
  const [body, setBody] = useState("");
  const [loading, setLoading] = useState(editing);
  const [preview, setPreview] = useState<ViewOpPreview | null>(null);
  const [busy, setBusy] = useState(false);

  // 编辑模式回填现有定义
  useEffect(() => {
    if (!view) return;
    let alive = true;
    api
      .getViewDefinition(connId, view)
      .then((spec) => {
        if (!alive) return;
        setName(spec.name);
        setBody(spec.body);
      })
      .catch((err) => {
        toastError(err);
        if (alive) onClose();
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connId, view]);

  // SQL 补全数据源（与查询页一致，随结构刷新重载）
  const [completion, setCompletion] = useState<Record<string, string[]>>({});
  useEffect(() => {
    let alive = true;
    api
      .getCompletionSchema(connId)
      .then((m) => alive && setCompletion(m))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [connId, schemaVersion]);
  const extensions = useMemo(() => [sql({ dialect: SQLite, schema: completion })], [completion]);

  const spec = () => ({ name: name.trim(), body, oldName: view ?? "" });

  const generate = async () => {
    setBusy(true);
    try {
      const p = editing
        ? await api.previewAlterView(connId, spec())
        : await api.previewCreateView(connId, spec());
      setPreview(p);
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!preview || preview.blocked) return;
    setBusy(true);
    try {
      if (editing) {
        await api.applyAlterView(connId, spec());
      } else {
        await api.applyCreateView(connId, spec());
      }
      toastSuccess(`视图 ${name.trim()} 已保存`);
      setPreview(null);
      await refreshSchema(connId);
      onApplied(name.trim(), view ?? "");
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Modal
        title={editing ? `编辑视图 · ${view}` : "新建视图"}
        onClose={onClose}
        width={640}
        footer={
          <>
            <button className="btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button
              className="btn primary"
              onClick={() => void generate()}
              disabled={busy || loading || !name.trim() || !body.trim()}
            >
              生成 SQL 预览
            </button>
          </>
        }
      >
        <div className="op-form">
          <label className="op-field">
            视图名
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如 v_active_users"
            />
          </label>
          <div className="op-field">
            SELECT 语句（视图主体）
            {loading ? (
              <p className="muted small">加载定义中…</p>
            ) : (
              <div className="view-editor codemirror-host">
                <CodeMirror
                  value={body}
                  onChange={setBody}
                  extensions={extensions}
                  theme={resolvedTheme === "dark" ? oneDark : "light"}
                  placeholder="SELECT ..."
                  height="220px"
                  basicSetup={{ lineNumbers: true, foldGutter: false }}
                />
              </div>
            )}
          </div>
        </div>
      </Modal>
      {preview && (
        <Modal
          title={editing ? `保存视图 · ${name.trim()}` : `新建视图 · ${name.trim()}`}
          onClose={() => setPreview(null)}
          width={560}
          footer={
            <>
              <button className="btn" onClick={() => setPreview(null)} disabled={busy}>
                取消
              </button>
              <button
                className={editing ? "btn danger" : "btn primary"}
                onClick={() => void apply()}
                disabled={!!preview.blocked || busy}
              >
                {busy ? "执行中…" : "执行"}
              </button>
            </>
          }
        >
          {preview.blocked && <div className="op-blocked">{preview.blocked}</div>}
          {(preview.warnings ?? []).length > 0 && (
            <ul className="op-warnings">
              {(preview.warnings ?? []).map((w, i) => (
                <li key={i}>{w}</li>
              ))}
            </ul>
          )}
          <pre className="ddl sql-preview">{(preview.sql ?? []).join(";\n") || "（无）"}</pre>
        </Modal>
      )}
    </>
  );
}
