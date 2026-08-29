import { useEffect, useRef, type ReactNode } from "react";

export interface MenuItem {
  label: string;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}

interface ContextMenuProps {
  x: number;
  y: number;
  items: MenuItem[];
  onClose: () => void;
}

/** 轻量右键菜单：点击外部或 Esc 关闭，位置自动收敛到窗口内。 */
export function ContextMenu({ x, y, items, onClose }: ContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("mousedown", onDown, true);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown, true);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const style: React.CSSProperties = {
    left: Math.min(x, window.innerWidth - 190),
    top: Math.min(y, window.innerHeight - items.length * 32 - 24),
  };

  return (
    <div className="context-menu" style={style} ref={ref} onContextMenu={(e) => e.preventDefault()}>
      {items.map((item) => (
        <button
          key={item.label}
          className={`context-menu-item${item.danger ? " danger" : ""}`}
          disabled={item.disabled}
          onClick={() => {
            onClose();
            item.onClick?.();
          }}
        >
          {item.icon}
          <span>{item.label}</span>
        </button>
      ))}
    </div>
  );
}
