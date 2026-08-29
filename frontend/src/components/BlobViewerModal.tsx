import { useEffect, useState } from "react";
import { FileDown, FolderOpen } from "lucide-react";
import * as api from "../api";
import { getErrorMessage } from "../api/errors";
import { Modal } from "./Modal";
import { toastError, toastSuccess } from "../stores/toastStore";
import { detectImageKind } from "../utils/blobkind";
import { formatBytes } from "../utils/format";

interface BlobViewerModalProps {
  connId: string;
  table: string;
  /** 行定位（主键 / rowid 映射）。 */
  keyMap: Record<string, unknown>;
  column: string;
  /** false 时只读（视图 / 主键列 / 只读库），不提供「从文件替换」。 */
  editable: boolean;
  onClose: () => void;
  /** 替换成功后回调（刷新数据）。 */
  onSaved: () => void;
}

/** 在线预览大小上限：超过则只显示信息不渲染 <img>。 */
const MAX_RENDER_BYTES = 10 * 1024 * 1024;

/**
 * BLOB 查看器（V2.5）：完整内容在线读取（列表标记只有 64 KB 预览），
 * 图片魔数识别后直接预览；支持另存为文件 / 从文件替换（可编辑时）。
 */
export function BlobViewerModal({
  connId,
  table,
  keyMap,
  column,
  editable,
  onClose,
  onSaved,
}: BlobViewerModalProps) {
  const [content, setContent] = useState<{ size: number; b64: string } | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const result = await api.getCellBlob(connId, table, keyMap, column);
        if (!cancelled) setContent(result);
      } catch (err) {
        if (!cancelled) setLoadError(getErrorMessage(err));
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const kind = content ? detectImageKind(content.b64) : null;
  const renderable = kind !== null && content !== null && content.size <= MAX_RENDER_BYTES;

  const saveToFile = async () => {
    if (!content) return;
    setBusy(true);
    try {
      const path = await api.pickBlobSavePath(`${table}-${column}.${kind?.ext ?? "bin"}`);
      if (!path) return;
      await api.writeFileBase64(path, content.b64);
      toastSuccess(`已保存 ${formatBytes(content.size)} 到 ${path}`);
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  const replaceFromFile = async () => {
    setBusy(true);
    try {
      const path = await api.pickBlobOpenPath();
      if (!path) return;
      const file = await api.readFileBase64(path);
      await api.updateCell(connId, table, keyMap, column, {
        __kind: "blob",
        size: file.size,
        b64: file.b64,
      });
      toastSuccess(`已用 ${path} 替换单元格内容（${formatBytes(file.size)}）`);
      onSaved();
      onClose();
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title={`BLOB 查看 · ${column}`}
      onClose={onClose}
      width={720}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            关闭
          </button>
          <button
            className="btn"
            onClick={() => void saveToFile()}
            disabled={busy || !content}
            title="把完整内容保存到文件"
          >
            <FileDown size={13} /> 另存为…
          </button>
          {editable && (
            <button
              className="btn primary"
              onClick={() => void replaceFromFile()}
              disabled={busy}
              title="选择文件替换该单元格内容"
            >
              <FolderOpen size={13} /> 从文件替换…
            </button>
          )}
        </>
      }
    >
      {loadError ? (
        <div className="error-banner">{loadError}</div>
      ) : !content ? (
        <p className="muted">读取单元格内容…</p>
      ) : (
        <>
          <p className="muted small">
            大小：{formatBytes(content.size)}
            {kind ? ` · 识别为 ${kind.label} 图片` : " · 未识别为常见图片格式"}
            {!renderable && kind ? "（超过 10 MB 不在线渲染）" : ""}
          </p>
          {renderable && kind && (
            <div className="blob-image-wrap">
              <img
                className="blob-image"
                src={`data:${kind.mime};base64,${content.b64}`}
                alt={`${column} 图片预览`}
              />
            </div>
          )}
          <details className="blob-details">
            <summary>Base64 编码（{formatBytes(content.size)}）</summary>
            <pre className="blob-preview">{content.b64 || "(空)"}</pre>
          </details>
        </>
      )}
    </Modal>
  );
}
