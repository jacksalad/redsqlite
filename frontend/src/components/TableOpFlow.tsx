import { useState, type ReactNode } from "react";
import { Modal } from "./Modal";
import * as api from "../api";
import { toastError, toastSuccess } from "../stores/toastStore";
import type { ColumnSpec, TableOp, TableOpPreview } from "../types";

/** connId 目标连接；onApplied 在操作成功后回调（刷新结构/SchemaTree、联动 Tab 改名等）。 */
interface FlowOptions {
  connId: string;
  onApplied: (op: TableOp) => void;
}

type FormState =
  | { form: "renameTable"; table: string }
  | { form: "renameColumn"; table: string; column: string }
  | { form: "addColumn"; table: string }
  | null;

/**
 * 表结构操作流程：表单/直接预览 → SQL 预览弹窗（阻断原因/提示）→ 执行。
 * 结构页与对象树共用；SQL 全部由后端生成，前端只展示。
 */
export function useTableOpFlow({ connId, onApplied }: FlowOptions) {
  const [form, setForm] = useState<FormState>(null);
  const [previewState, setPreviewState] = useState<{ op: TableOp; preview: TableOpPreview } | null>(
    null,
  );
  const [busy, setBusy] = useState(false);

  const requestPreview = async (op: TableOp) => {
    try {
      const preview = await api.previewTableOp(connId, op);
      setPreviewState({ op, preview });
    } catch (error) {
      toastError(error);
    }
  };

  const beginRenameTable = (table: string) => setForm({ form: "renameTable", table });
  const beginRenameColumn = (table: string, column: string) =>
    setForm({ form: "renameColumn", table, column });
  const beginAddColumn = (table: string) => setForm({ form: "addColumn", table });
  const confirmDropColumn = (table: string, column: string) =>
    void requestPreview({ kind: "dropColumn", table, column });
  const confirmDropTable = (table: string) =>
    void requestPreview({ kind: "dropTable", table });

  const execute = async (op: TableOp) => {
    setBusy(true);
    try {
      const sql = await api.applyTableOp(connId, op);
      toastSuccess("结构变更已执行");
      setPreviewState(null);
      onApplied(op);
      return sql;
    } catch (error) {
      toastError(error);
      return null;
    } finally {
      setBusy(false);
    }
  };

  const isDanger = previewState?.op.kind === "dropTable" || previewState?.op.kind === "dropColumn";

  const element: ReactNode = (
    <>
      {form?.form === "renameTable" && (
        <RenameFormModal
          title="重命名表"
          label="新表名"
          initial={form.table}
          onClose={() => setForm(null)}
          onSubmit={(newName) => {
            setForm(null);
            void requestPreview({ kind: "renameTable", table: form.table, newName });
          }}
        />
      )}
      {form?.form === "renameColumn" && (
        <RenameFormModal
          title={`重命名字段 · ${form.column}`}
          label="新字段名"
          initial={form.column}
          onClose={() => setForm(null)}
          onSubmit={(newName) => {
            setForm(null);
            void requestPreview({
              kind: "renameColumn",
              table: form.table,
              column: form.column,
              newName,
            });
          }}
        />
      )}
      {form?.form === "addColumn" && (
        <AddColumnModal
          table={form.table}
          onClose={() => setForm(null)}
          onSubmit={(spec) => {
            setForm(null);
            void requestPreview({ kind: "addColumn", table: form.table, columnSpec: spec });
          }}
        />
      )}
      {previewState && (
        <SqlPreviewModal
          preview={previewState.preview}
          busy={busy}
          danger={isDanger}
          onClose={() => setPreviewState(null)}
          onConfirm={() => void execute(previewState.op)}
        />
      )}
    </>
  );

  return { beginRenameTable, beginRenameColumn, beginAddColumn, confirmDropColumn, confirmDropTable, element };
}

/** SQL 预览弹窗：阻断原因置顶（禁止执行）、警告列表、SQL 正文；
 * extra 为执行中的附加反馈（如进度条，V3.0）。 */
export function SqlPreviewModal({
  title = "SQL 预览",
  preview,
  busy,
  danger,
  extra,
  onClose,
  onConfirm,
}: {
  title?: string;
  preview: TableOpPreview;
  busy?: boolean;
  danger?: boolean;
  extra?: ReactNode;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const warnings = preview.warnings ?? [];
  return (
    <Modal title={title} onClose={onClose} width={560}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button
            className={danger ? "btn danger" : "btn primary"}
            onClick={onConfirm}
            disabled={!!preview.blocked || busy}
          >
            {busy ? "执行中…" : "执行"}
          </button>
        </>
      }
    >
      {preview.blocked && <div className="op-blocked">{preview.blocked}</div>}
      {warnings.length > 0 && (
        <ul className="op-warnings">
          {warnings.map((w, i) => (
            <li key={i}>{w}</li>
          ))}
        </ul>
      )}
      {extra}
      <pre className="ddl sql-preview">{preview.sql || "（无）"}</pre>
    </Modal>
  );
}

function RenameFormModal({
  title,
  label,
  initial,
  onClose,
  onSubmit,
}: {
  title: string;
  label: string;
  initial: string;
  onClose: () => void;
  onSubmit: (newName: string) => void;
}) {
  const [value, setValue] = useState(initial);
  const submit = () => {
    const name = value.trim();
    if (!name) {
      toastError("名称不能为空");
      return;
    }
    if (name === initial) {
      onClose();
      return;
    }
    onSubmit(name);
  };
  return (
    <Modal title={title} onClose={onClose} width={420}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            取消
          </button>
          <button className="btn primary" onClick={submit}>
            下一步（预览 SQL）
          </button>
        </>
      }
    >
      <div className="op-form">
        <label className="op-field">
          {label}
          <input
            autoFocus
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
            placeholder={initial}
          />
        </label>
        <p className="muted small">执行前会展示生成的 SQL 供确认。</p>
      </div>
    </Modal>
  );
}

function AddColumnModal({
  table,
  onClose,
  onSubmit,
}: {
  table: string;
  onClose: () => void;
  onSubmit: (spec: ColumnSpec) => void;
}) {
  const [name, setName] = useState("");
  const [type, setType] = useState("TEXT");
  const [notNull, setNotNull] = useState(false);
  const [defaultValue, setDefault] = useState("");

  const submit = async () => {
    const colName = name.trim();
    if (!colName) {
      toastError("字段名不能为空");
      return;
    }
    if (notNull && !defaultValue.trim()) {
      // 提前给出友好提示，后端会再次校验
      toastError("NOT NULL 字段必须填写非 NULL 默认值");
      return;
    }
    onSubmit({
      name: colName,
      type: type.trim(),
      notNull,
      primaryKey: false,
      autoIncrement: false,
      unique: false,
      default: defaultValue.trim(),
      defaultIsExpression: false,
      generated: "",
      generatedStored: false,
    });
  };

  return (
    <Modal title={`添加字段 · ${table}`} onClose={onClose} width={460}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            取消
          </button>
          <button className="btn primary" onClick={() => void submit()}>
            下一步（预览 SQL）
          </button>
        </>
      }
    >
      <div className="op-form">
        <label className="op-field">
          字段名
          <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="如 title" />
        </label>
        <label className="op-field">
          类型
          <input value={type} onChange={(e) => setType(e.target.value)} placeholder="TEXT / INTEGER / REAL …" />
        </label>
        <label className="op-check">
          <input type="checkbox" checked={notNull} onChange={(e) => setNotNull(e.target.checked)} />
          NOT NULL（需要默认值）
        </label>
        <label className="op-field">
          默认值（字面量）
          <input
            value={defaultValue}
            onChange={(e) => setDefault(e.target.value)}
            placeholder={'如 0、\'文本\'、NULL（SQLite 不支持加列用表达式默认值）'}
          />
        </label>
      </div>
    </Modal>
  );
}
