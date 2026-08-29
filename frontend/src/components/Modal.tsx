import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";

// 弹窗栈：Esc 只关闭最上层的弹窗。叠放弹窗（如 SQL 预览浮在表设计器上）
// 若各自挂全局监听又无栈管理，一次 Esc 会把整叠全部关闭、丢掉底层表单的编辑。
const escStack: Array<() => void> = [];

interface ModalProps {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}

/** 基础模态框：Esc 关闭最上层弹窗、遮罩点击不关闭（避免误触丢数据）。 */
export function Modal({ title, onClose, children, footer, width = 520 }: ModalProps) {
  // onClose 走 ref：注册一次保持挂载顺序（栈序），父组件内联箭头函数不会重订阅
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const close = () => closeRef.current();
    escStack.push(close);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && escStack[escStack.length - 1] === close) {
        close();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      const i = escStack.indexOf(close);
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
