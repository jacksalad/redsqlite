import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { sql, SQLite } from "@codemirror/lang-sql";
import { oneDark } from "@codemirror/theme-one-dark";
import {
  Ban,
  Braces,
  Download,
  History,
  Play,
  Trash2,
} from "lucide-react";
import * as api from "../api";
import { getErrorMessage } from "../api/errors";
import { DataGrid } from "../components/DataGrid";
import { Modal } from "../components/Modal";
import { useConnectionsStore } from "../stores/connectionsStore";
import { useTabsStore } from "../stores/tabsStore";
import { toastError, toastInfo, toastSuccess } from "../stores/toastStore";
import type {
  BatchResult,
  HistoryEntry,
  QueryResult,
  StatementPreview,
  Tab,
} from "../types";
import { clearHistory, loadHistory, saveHistoryEntry } from "../utils/history";
import { narrowRows } from "../utils/rows";
import { formatSql } from "../utils/sqlFormat";
import { useResolvedTheme } from "../utils/theme";

/** SQL 查询页（文档 5.2 / 6.5）。支持多语句：分号拆分、事务执行、逐条结果（V1.2）。 */
export function QueryPage({ tab }: { tab: Tab }) {
  const connId = tab.connId;
  const resolvedTheme = useResolvedTheme();
  const setTabSql = useTabsStore((s) => s.setTabSql);
  const refreshSchema = useConnectionsStore((s) => s.refreshSchema);

  // SQL 补全数据源（V2.3）：表/视图名 → 列名，随结构刷新重载
  const schemaVersion = useConnectionsStore(
    (s) => s.conns.find((c) => c.id === connId)?.schema,
  );
  const [completion, setCompletion] = useState<Record<string, string[]>>({});
  useEffect(() => {
    let alive = true;
    api
      .getCompletionSchema(connId)
      .then((m) => {
        if (alive && m) setCompletion(m);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [connId, schemaVersion]);
  const editorExtensions = useMemo(
    () => [sql({ dialect: SQLite, schema: completion })],
    [completion],
  );

  const sqlText = tab.sql ?? "";
  const [result, setResult] = useState<QueryResult | null>(null);
  const [batch, setBatch] = useState<BatchResult | null>(null);
  const [batchGridIndex, setBatchGridIndex] = useState<number>(-1);
  /** 最近一次实际执行的 SQL：导出必须重放它而不是编辑器当前文本
   * （执行后再改编辑器，导出的应是界面上所展示的结果，审计 M-9） */
  const [executedSql, setExecutedSql] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [runningMs, setRunningMs] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [pendingDanger, setPendingDanger] = useState<StatementPreview[] | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const cancelRequested = useRef(false);
  // 执行中防重入：按钮 disabled 只挡鼠标，Ctrl+Enter 仍会触发，必须用 ref 挡住并发执行
  const runningRef = useRef(false);
  const editorRef = useRef<HTMLDivElement>(null);

  const setSql = (value: string) => setTabSql(tab.id, value);

  // 执行中的实时计时（V1.2：取消反馈的一部分）
  useEffect(() => {
    if (!running) return;
    const started = Date.now();
    setRunningMs(0);
    const id = window.setInterval(() => setRunningMs(Date.now() - started), 100);
    return () => window.clearInterval(id);
  }, [running]);

  // statements 传入已预检的结果（tryRun 已查询过），避免每次执行重复一次 IPC 往返
  const run = useCallback(async (statements?: StatementPreview[]) => {
    if (runningRef.current) return;
    const text = sqlText.trim();
    if (!text) {
      toastError("SQL 不能为空");
      return;
    }
    setError(null);
    setResult(null);
    setBatch(null);
    setBatchGridIndex(-1);
    runningRef.current = true;
    setRunning(true);
    cancelRequested.current = false;
    const startedAt = Date.now();
    try {
      const stmts = statements ?? (await api.previewSql(text));
      setExecutedSql(text);
      if (stmts.length > 1) {
        const batchResult = await api.executeSqlBatch(connId, text);
        setBatch(batchResult);
        saveHistoryEntry({ sql: text, at: startedAt, ok: batchResult.failedIndex < 0 });
        // 生成的绑定类型 items 可能为 null，统一收窄
        const items = batchResult.items ?? [];
        // 默认展示最后一条查询语句的结果
        for (let i = items.length - 1; i >= 0; i--) {
          if (items[i]?.isQuery && items[i]?.result) {
            setBatchGridIndex(i);
            break;
          }
        }
        if (items.some((it) => it != null && !it.isQuery)) {
          void refreshSchema(connId);
        }
      } else {
        const res = await api.executeSql(connId, text);
        setResult(res);
        saveHistoryEntry({ sql: text, at: startedAt, ok: true });
        if (!res.isQuery) {
          void refreshSchema(connId);
        }
      }
    } catch (err) {
      const msg = getErrorMessage(err);
      if (cancelRequested.current) {
        // 用户主动中止：不展示为错误
        toastInfo("查询已中止");
      } else {
        setError(msg);
        saveHistoryEntry({ sql: text, at: startedAt, ok: false });
      }
    } finally {
      runningRef.current = false;
      setRunning(false);
    }
  }, [sqlText, refreshSchema]);

  /** 先预检危险语句再执行（文档 13.2）；多语句时逐条确认。 */
  const tryRun = useCallback(async () => {
    if (runningRef.current) return;
    const text = sqlText.trim();
    if (!text) {
      toastError("SQL 不能为空");
      return;
    }
    let statements: StatementPreview[];
    try {
      statements = await api.previewSql(text);
    } catch (err) {
      toastError(err);
      return;
    }
    const dangerous = statements.filter((s) => s.dangerous);
    if (dangerous.length > 0) {
      setPendingDanger(dangerous);
      return;
    }
    void run(statements);
  }, [sqlText, run]);

  // Ctrl+Enter 执行 / Ctrl+L 清空（文档 5.2 快捷键）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const active = useTabsStore.getState().activeId === tab.id;
      if (!active) return;
      if (e.ctrlKey && e.key === "Enter") {
        e.preventDefault();
        void tryRun();
      } else if (e.ctrlKey && (e.key === "l" || e.key === "L")) {
        e.preventDefault();
        setSql("");
        setResult(null);
        setBatch(null);
        setError(null);
        setExecutedSql(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.id, tryRun, sqlText]);

  const cancel = async () => {
    cancelRequested.current = true;
    try {
      await api.cancelQuery(connId);
      toastInfo("已发送中止请求，等待查询返回…");
    } catch (err) {
      toastError(err);
    }
  };

  const format = () => {
    const formatted = formatSql(sqlText);
    if (formatted) setSql(formatted);
  };

  const exportResult = async (format: "csv" | "json" | "xlsx") => {
    if (batch) {
      toastError("多语句结果暂不支持导出，请单独执行要导出的查询");
      return;
    }
    if (!result) {
      toastError("请先执行查询后再导出");
      return;
    }
    if (!result.isQuery) {
      toastError("只有查询结果可以导出");
      return;
    }
    // 重放最近一次执行的 SQL（而非编辑器当前文本），保证导出内容与界面展示一致
    const sqlToExport = executedSql ?? sqlText.trim();
    try {
      const path = await api.pickExportPath("query-result", format);
      if (!path) return;
      const res = await api.exportQueryResult(connId, sqlToExport, format, path);
      toastSuccess(`已导出 ${res.rows} 行到 ${res.path}`);
    } catch (err) {
      toastError(err);
    }
  };

  const openHistory = () => {
    setHistory(loadHistory());
    setHistoryOpen(true);
  };

  const batchGridItem =
    batch && batchGridIndex >= 0 ? (batch.items ?? [])[batchGridIndex] : null;

  return (
    <div className="page query-page">
      <div className="query-editor-zone" ref={editorRef}>
        <div className="query-toolbar">
          <button className="btn primary" onClick={() => void tryRun()} disabled={running} title="执行（Ctrl+Enter）">
            <Play size={13} /> 执行
          </button>
          <button className="btn" onClick={() => void cancel()} disabled={!running} title="中止当前查询">
            <Ban size={13} /> 中止
          </button>
          <button className="btn" onClick={format} title="格式化 SQL" disabled={running}>
            <Braces size={13} /> 格式化
          </button>
          <button
            className="btn"
            onClick={() => {
              setSql("");
              setResult(null);
              setBatch(null);
              setError(null);
              setExecutedSql(null);
            }}
            title="清空（Ctrl+L）"
          >
            <Trash2 size={13} /> 清空
          </button>
          <button className="btn" onClick={openHistory} title="查询历史">
            <History size={13} /> 历史
          </button>
          <div className="spacer" />
          <button className="btn" onClick={() => void exportResult("csv")} title="导出查询结果为 CSV">
            <Download size={13} /> CSV
          </button>
          <button className="btn" onClick={() => void exportResult("json")} title="导出查询结果为 JSON">
            <Download size={13} /> JSON
          </button>
          <button className="btn" onClick={() => void exportResult("xlsx")} title="导出查询结果为 Excel">
            <Download size={13} /> XLSX
          </button>
        </div>
        <div className={`codemirror-host ${running ? "readonly" : ""}`}>
          <CodeMirror
            value={sqlText}
            onChange={setSql}
            extensions={editorExtensions}
            theme={resolvedTheme === "dark" ? oneDark : "light"}
            editable={!running}
            basicSetup={{
              lineNumbers: true,
              highlightActiveLine: true,
              autocompletion: true,
              bracketMatching: true,
              closeBrackets: true,
            }}
            placeholder="输入 SQL，Ctrl+Enter 执行；多条语句以分号分隔，将包在事务中逐条执行"
            height="220px"
          />
        </div>
      </div>

      <div className="query-result-zone">
        <div className="query-status">
          {running && (
            <span className="running-badge">
              执行中… {(runningMs / 1000).toFixed(1)}s（Ctrl+Enter 执行中可点「中止」）
            </span>
          )}
          {batch && !running && (
            <span>
              共 {(batch.items ?? []).length} 条语句 · 总耗时 {batch.totalElapsedMs} ms
              {batch.failedIndex >= 0 ? (
                <span className="warn-text"> · 第 {batch.failedIndex + 1} 条失败，已回滚全部语句</span>
              ) : (
                " · 全部成功"
              )}
            </span>
          )}
          {!batch && result?.isQuery && (
            <span>
              返回 {(result.rows?.length ?? 0).toLocaleString()} 行 · 耗时 {result.elapsedMs} ms
              {result.truncated && (
                <span className="warn-text">（已达最大返回行数，结果被截断，可导出查看全部）</span>
              )}
            </span>
          )}
          {!batch && result && !result.isQuery && (
            <span>
              影响 {result.rowsAffected} 行
              {result.lastInsertId > 0 && ` · LastInsertID ${result.lastInsertId}`}
              {" · 耗时 "}
              {result.elapsedMs} ms
            </span>
          )}
          {!result && !batch && !running && <span className="muted">尚未执行查询</span>}
        </div>

        {error && <div className="error-banner">{error}</div>}

        {batch && (
          <div className="batch-list">
            {(batch.items ?? []).map((item, i) => (
              <button
                key={i}
                className={`batch-item ${item.isQuery && item.result ? "clickable" : ""} ${
                  i === batchGridIndex ? "active" : ""
                } ${item.error ? "failed" : ""}`}
                onClick={() => item.isQuery && item.result && setBatchGridIndex(i)}
                title={item.sql}
              >
                <span className="batch-index">#{i + 1}</span>
                <span className="batch-kind">{item.isQuery ? "查询" : "写入"}</span>
                {item.error ? (
                  <span className="batch-summary error">失败：{item.error}</span>
                ) : (
                  <span className="batch-summary">
                    {item.isQuery
                      ? `${item.result?.rows?.length ?? 0} 行${item.result?.truncated ? "（截断）" : ""}`
                      : `影响 ${item.rowsAffected} 行`}
                  </span>
                )}
                <code className="batch-sql">{sqlSnippet(item.sql)}</code>
              </button>
            ))}
          </div>
        )}

        <div className="query-grid">
          {batchGridItem?.result ? (
            <DataGrid
              columns={batchGridItem.result.columns ?? []}
              columnTypes={batchGridItem.result.columnTypes ?? undefined}
              rows={narrowRows(batchGridItem.result.rows)}
              emptyText="该语句没有返回数据"
            />
          ) : result?.isQuery ? (
            <DataGrid
              columns={result.columns ?? []}
              columnTypes={result.columnTypes ?? undefined}
              rows={narrowRows(result.rows)}
              emptyText="查询没有返回数据"
            />
          ) : (
            !error && <div className="empty-state">查询结果将显示在这里</div>
          )}
        </div>
      </div>

      {pendingDanger && (
        <Modal
          title="危险操作确认"
          onClose={() => setPendingDanger(null)}
          width={560}
          footer={
            <>
              <button className="btn" onClick={() => setPendingDanger(null)}>
                取消
              </button>
              <button
                className="btn danger"
                onClick={() => {
                  setPendingDanger(null);
                  void run();
                }}
              >
                仍然执行
              </button>
            </>
          }
        >
          <p className="confirm-message">
            以下 {pendingDanger.length} 条语句属于危险操作，请确认后执行：
          </p>
          <ul className="danger-list">
            {pendingDanger.map((s, i) => (
              <li key={i}>
                <code>{sqlSnippet(s.sql, 120)}</code>
                <span className="danger-reason">{s.reason}</span>
              </li>
            ))}
          </ul>
        </Modal>
      )}

      {historyOpen && (
        <Modal
          title="查询历史"
          onClose={() => setHistoryOpen(false)}
          width={640}
          footer={
            <>
              <button
                className="btn"
                onClick={() => {
                  clearHistory();
                  setHistory([]);
                }}
              >
                清空历史
              </button>
              <button className="btn primary" onClick={() => setHistoryOpen(false)}>
                关闭
              </button>
            </>
          }
        >
          {history.length === 0 ? (
            <p className="muted">暂无历史记录</p>
          ) : (
            <ul className="history-list">
              {history.map((h, i) => (
                <li key={i} className={`history-item ${h.ok ? "" : "failed"}`}>
                  <button
                    className="history-sql"
                    onClick={() => {
                      setSql(h.sql);
                      setHistoryOpen(false);
                    }}
                    title="点击载入到编辑器"
                  >
                    <code>{h.sql}</code>
                  </button>
                  <span className="history-meta">
                    {new Date(h.at).toLocaleString()} {h.ok ? "" : "· 出错"}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Modal>
      )}
    </div>
  );
}

function sqlSnippet(sql: string, max = 60): string {
  const oneLine = sql.replace(/\s+/g, " ").trim();
  return oneLine.length > max ? oneLine.slice(0, max) + "…" : oneLine;
}
