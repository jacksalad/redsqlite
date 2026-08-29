import { useEffect, useState } from "react";
import { Modal } from "./Modal";
import { useSettingsStore } from "../stores/settingsStore";
import { useUiStore } from "../stores/uiStore";
import { toastSuccess } from "../stores/toastStore";
import type { Settings } from "../types";

/** 设置弹窗（文档 6.1：WAL 为可选；13.3：默认限制）。 */
export function SettingsModal() {
  const open = useUiStore((s) => s.settingsOpen);
  const setOpen = useUiStore((s) => s.setSettingsOpen);
  const settings = useSettingsStore((s) => s.settings);
  const save = useSettingsStore((s) => s.save);

  const [draft, setDraft] = useState<Settings>(settings);

  useEffect(() => {
    if (open) setDraft(settings);
  }, [open, settings]);

  if (!open) return null;

  const submit = async () => {
    const saved = await save(draft);
    if (saved) {
      toastSuccess("设置已保存");
      if (draft.enableWAL !== settings.enableWAL) {
        toastSuccess("WAL 设置将在下次打开数据库时生效");
      }
      setOpen(false);
    }
  };

  return (
    <Modal
      title="设置"
      onClose={() => setOpen(false)}
      width={460}
      footer={
        <>
          <button className="btn" onClick={() => setOpen(false)}>
            取消
          </button>
          <button className="btn primary" onClick={() => void submit()}>
            保存
          </button>
        </>
      }
    >
      <div className="settings-form">
        <label className="settings-row">
          <div>
            <span>表数据每页行数</span>
            <span className="muted small">10 – 1000</span>
          </div>
          <input
            type="number"
            min={10}
            max={1000}
            value={draft.pageSize}
            onChange={(e) =>
              setDraft((d) => ({ ...d, pageSize: clamp(Number(e.target.value), 10, 1000, 100) }))
            }
          />
        </label>

        <label className="settings-row">
          <div>
            <span>SQL 执行超时（秒）</span>
            <span className="muted small">1 – 600，超时自动中止</span>
          </div>
          <input
            type="number"
            min={1}
            max={600}
            value={Math.round(draft.queryTimeoutMs / 1000)}
            onChange={(e) =>
              setDraft((d) => ({
                ...d,
                queryTimeoutMs: clamp(Number(e.target.value), 1, 600, 30) * 1000,
              }))
            }
          />
        </label>

        <label className="settings-row">
          <div>
            <span>单次查询最大返回行数</span>
            <span className="muted small">100 – 1000000，超出部分截断（可导出全部）</span>
          </div>
          <input
            type="number"
            min={100}
            max={1000000}
            step={100}
            value={draft.maxQueryRows}
            onChange={(e) =>
              setDraft((d) => ({ ...d, maxQueryRows: clamp(Number(e.target.value), 100, 1000000, 10000) }))
            }
          />
        </label>

        <label className="settings-row">
          <div>
            <span>界面主题</span>
            <span className="muted small">跟随系统或固定浅色/深色</span>
          </div>
          <select
            value={draft.theme}
            onChange={(e) => setDraft((d) => ({ ...d, theme: e.target.value }))}
          >
            <option value="system">跟随系统</option>
            <option value="light">浅色</option>
            <option value="dark">深色</option>
          </select>
        </label>

        <label className="settings-row">
          <div>
            <span>打开数据库时启用 WAL 模式</span>
            <span className="muted small">
              提升写入并发，但会改变文件行为（生成 -wal/-shm 文件），下次打开生效
            </span>
          </div>
          <input
            type="checkbox"
            checked={draft.enableWAL}
            onChange={(e) => setDraft((d) => ({ ...d, enableWAL: e.target.checked }))}
          />
        </label>

        <p className="muted small settings-note">
          设置保存在用户配置目录（%APPDATA%\RedSQLite），修改失败会提示具体原因。
        </p>
      </div>
    </Modal>
  );
}

function clamp(v: number, min: number, max: number, fallback: number): number {
  if (!Number.isFinite(v)) return fallback;
  return Math.min(max, Math.max(min, v));
}
