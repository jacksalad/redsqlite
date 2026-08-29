import { ChevronLeft, ChevronRight } from "lucide-react";

interface PaginationProps {
  /** cursor：游标分页（默认顺序，无总页数）；offset：排序等场景的页码分页 */
  mode: "cursor" | "offset";
  /** 当前页码（游标模式下为客户端翻页计数） */
  page: number;
  pageSize: number;
  /** 当前页实际行数（偏移模式计算区间用） */
  shown: number;
  /** 异步行数统计；null 表示统计中或不可用 */
  total: number | null;
  hasPrev: boolean;
  hasNext: boolean;
  onPrev: () => void;
  onNext: () => void;
  /** 偏移模式且已知总行数时允许页码跳转 */
  onJump?: (page: number) => void;
  onPageSize?: (size: number) => void;
}

/** 分页条：游标分页只提供上一页/下一页；偏移分页在总行数就绪后支持跳页。 */
export function Pagination({
  mode,
  page,
  pageSize,
  shown,
  total,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onJump,
  onPageSize,
}: PaginationProps) {
  const totalPages = total != null ? Math.max(1, Math.ceil(total / pageSize)) : null;
  const from = shown === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = shown === 0 ? 0 : from + shown - 1;

  return (
    <div className="pagination">
      <span className="page-info">
        {total == null
          ? "行数统计中…"
          : `共 ${total.toLocaleString()} 行${mode === "offset" ? ` · 第 ${from}-${to} 行` : ""}`}
      </span>
      <div className="page-controls">
        <button className="icon-btn" disabled={!hasPrev} onClick={onPrev} aria-label="上一页">
          <ChevronLeft size={15} />
        </button>
        <span className="page-current">
          第 {page} 页{totalPages != null ? ` / ${totalPages}` : ""}
        </span>
        <button className="icon-btn" disabled={!hasNext} onClick={onNext} aria-label="下一页">
          <ChevronRight size={15} />
        </button>
        {mode === "offset" && totalPages != null && totalPages > 1 && onJump && (
          <input
            className="page-jump"
            type="number"
            min={1}
            max={totalPages}
            placeholder="跳页"
            title={`跳转到指定页（1-${totalPages}），回车确认`}
            onKeyDown={(e) => {
              if (e.key !== "Enter") return;
              const p = Number((e.target as HTMLInputElement).value);
              if (Number.isInteger(p) && p >= 1 && p <= (totalPages ?? 1)) {
                onJump(p);
                (e.target as HTMLInputElement).blur();
              }
            }}
          />
        )}
        {onPageSize && (
          <select
            className="page-size"
            value={pageSize}
            onChange={(e) => onPageSize(Number(e.target.value))}
            title="每页行数"
          >
            {[50, 100, 200, 500].map((n) => (
              <option key={n} value={n}>
                {n} 行/页
              </option>
            ))}
          </select>
        )}
      </div>
    </div>
  );
}
