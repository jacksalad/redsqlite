import { useState } from "react";
import { Copy, Trash2 } from "lucide-react";
import { Modal, ConfirmDialog } from "./Modal";
import { toastError, toastSuccess } from "../stores/toastStore";

/** 只读 DDL 查看弹窗（触发器/索引），可选删除（带二次确认）。 */
export function DdlModal({
  title,
  sql,
  meta,
  deleteLabel,
  onDelete,
  onClose,
}: {
  title: string;
  sql: string;
  meta?: string;
  deleteLabel?: string;
  /** 提供则显示删除按钮；返回 Promise，失败由调用方 toast 或在此处理 */
  onDelete?: () => Promise<void>;
  onClose: () => void;
}) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(sql);
      toastSuccess("已复制到剪贴板");
    } catch {
      toastError("复制失败");
    }
  };

  const doDelete = async () => {
    setBusy(true);
    try {
      await onDelete?.();
      setConfirmOpen(false);
      onClose();
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Modal title={title} onClose={onClose} width={560}
        footer={
          <>
            <button className="btn" onClick={() => void copy()}>
              <Copy size={13} /> 复制
            </button>
            {onDelete && (
              <button className="btn danger" onClick={() => setConfirmOpen(true)} disabled={busy}>
                <Trash2 size={13} /> {deleteLabel ?? "删除"}
              </button>
            )}
          </>
        }
      >
        {meta && <p className="muted small">{meta}</p>}
        <pre className="ddl">{sql || "（无定义语句：该对象由约束自动创建）"}</pre>
      </Modal>
      {confirmOpen && (
        <ConfirmDialog
          title={deleteLabel ? `确认${deleteLabel}` : "确认删除"}
          message={`此操作不可撤销，确定要执行吗？`}
          onConfirm={() => void doDelete()}
          onCancel={() => setConfirmOpen(false)}
        />
      )}
    </>
  );
}
