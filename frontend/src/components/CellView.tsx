import { useState } from "react";
import { Binary } from "lucide-react";
import type { CellValue } from "../types";
import { isBigintMarker, isBlobMarker, isTruncatedMarker } from "../types";
import { formatBytes } from "../utils/format";
import { Modal } from "./Modal";

/** 单元格展示：NULL / BLOB / 截断文本 / 普通值（文档 6.3 / 13.3）。
 * onOpen 提供时（表数据页），点击 BLOB 徽标交给外部查看器（V2.5：
 * 图片预览 / 另存为 / 从文件替换）；否则用内置 Base64 弹窗（查询结果页）。 */
export function CellView({ value, onOpen }: { value: CellValue; onOpen?: () => void }) {
  const [showBlob, setShowBlob] = useState(false);

  if (value === null || value === undefined) {
    return <span className="cell-null">NULL</span>;
  }

  if (isBlobMarker(value)) {
    return (
      <>
        <button
          className="cell-blob-badge"
          onClick={() => (onOpen ? onOpen() : setShowBlob(true))}
          title={onOpen ? "点击查看 / 编辑 BLOB" : "点击查看 BLOB 预览（Base64）"}
        >
          <Binary size={11} /> BLOB {formatBytes(value.size)}
        </button>
        {showBlob && (
          <Modal title="BLOB 预览" onClose={() => setShowBlob(false)} width={640}>
            <p className="muted small">大小：{formatBytes(value.size)}，以下为 Base64 编码预览（前 64 KB）。</p>
            <pre className="blob-preview">{value.b64 || "(空)"}</pre>
          </Modal>
        )}
      </>
    );
  }

  if (isTruncatedMarker(value)) {
    return (
      <span title={`文本过长已截断，实际 ${formatBytes(value.size)}`}>
        {value.value}
        <span className="cell-truncated">…（已截断）</span>
      </span>
    );
  }

  if (isBigintMarker(value)) {
    return (
      <span className="cell-bigint" title="超出 JS 安全整数范围的大整数，按字符串精确显示">
        {value.str}
      </span>
    );
  }

  if (typeof value === "boolean") {
    return <span className="cell-bool">{value ? "true" : "false"}</span>;
  }

  return <>{String(value)}</>;
}
