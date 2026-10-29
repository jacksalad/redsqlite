import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";

interface ModalProps {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}

// Esc 关闭栈：层叠弹窗（如确认框叠在 DDL 弹窗上）只关最上层一次，
// 避免一次 Esc 把底下已填写的表单一并关掉。栈序 = 挂载序，后挂载者在顶。
const escStack: { close: () => void }[] = [];

/** 基础模态框：Esc 关闭最上层、遮罩点击不关闭（避免误触丢数据）。 */
export function Modal({ title, onClose, children, footer, width = 520 }: ModalProps) {
  // onClose 经 ref 读取：effect 只在挂载时注册一次，栈内条目身份稳定，
  // 父组件重渲染换新的 onClose 不会把本弹窗挪到栈顶
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    const item = { close: () => closeRef.current() };
    escStack.push(item);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && escStack[escStack.length - 1] === item) {
        closeRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      const i = escStack.indexOf(item);
      if (i >= 0) escStack.splice(i, 1);
    };
  }, []);

  return (
    <div className="modal-overlay">
      <div className="modal" style={{ width }} role="dialog" aria-modal="true">
        <div className="modal-header">
          <span className="modal-title">{title}</span>
          <button className="icon-btn" onClick={onClose} aria-label="关闭">
            <X size={16} />
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  );
}

interface ConfirmProps {
  title: string;
  message: string;
  confirmText?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/** 危险操作二次确认（文档 13.2）。 */
export function ConfirmDialog({
  title,
  message,
  confirmText = "确定",
  danger = true,
  onConfirm,
  onCancel,
}: ConfirmProps) {
  return (
    <Modal
      title={title}
      onClose={onCancel}
      width={420}
      footer={
        <>
          <button className="btn" onClick={onCancel}>
            取消
          </button>
          <button className={danger ? "btn danger" : "btn primary"} onClick={onConfirm}>
            {confirmText}
          </button>
        </>
      }
    >
      <p className="confirm-message">{message}</p>
    </Modal>
  );
}
