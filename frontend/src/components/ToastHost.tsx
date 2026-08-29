import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";
import { useToastStore, type ToastKind } from "../stores/toastStore";

const ICONS: Record<ToastKind, typeof Info> = {
  error: AlertCircle,
  success: CheckCircle2,
  info: Info,
};

/** 全局错误/提示浮层（文档 12：错误分类的 UI 呈现）。 */
export function ToastHost() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);

  if (toasts.length === 0) return null;

  return (
    <div className="toast-host">
      {toasts.map((t) => {
        const Icon = ICONS[t.kind];
        return (
          <div key={t.id} className={`toast toast-${t.kind}`}>
            <Icon size={15} className="toast-icon" />
            <span className="toast-message">{t.message}</span>
            <button className="icon-btn" onClick={() => dismiss(t.id)} aria-label="关闭提示">
              <X size={13} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
