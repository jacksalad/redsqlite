// Go 服务绑定的薄封装（文档 9：组件不直接依赖生成 bindings）。
// V2.1：连接型 API 一律以 connID 开头指定目标数据库连接。
import * as DatabaseService from "../../bindings/github.com/redjack/redsqlite/services/databaseservice.js";
import * as SchemaService from "../../bindings/github.com/redjack/redsqlite/services/schemaservice.js";
import * as DataService from "../../bindings/github.com/redjack/redsqlite/services/dataservice.js";
import * as QueryService from "../../bindings/github.com/redjack/redsqlite/services/queryservice.js";
import * as ExportService from "../../bindings/github.com/redjack/redsqlite/services/exportservice.js";
import * as ImportService from "../../bindings/github.com/redjack/redsqlite/services/importservice.js";
import * as RecentService from "../../bindings/github.com/redjack/redsqlite/services/recentservice.js";
import * as SettingsService from "../../bindings/github.com/redjack/redsqlite/services/settingsservice.js";
import * as FileService from "../../bindings/github.com/redjack/redsqlite/services/fileservice.js";
import * as TransferService from "../../bindings/github.com/redjack/redsqlite/services/transferservice.js";
import type {
  BackupResult,
  BatchResult,
  BinaryContent,
  ColumnInfo,
  CreateIndexPreview,
  CreateIndexSpec,
  CreateTableSpec,
  DatabaseInfo,
  FilterCondition,
  ForeignKeyInfo,
  ImportPreview,
  ImportRequest,
  ImportResult,
  QueryResult,
  RecentEntry,
  RedesignPreview,
  SchemaInfo,
  Settings,
  SheetInfo,
  StatementPreview,
  TableData,
  TableDataRequest,
  TableOp,
  TableOpPreview,
  TableRedesign,
  TableStructure,
  TransferPreview,
  TransferRequest,
  TransferResult,
  ViewOpPreview,
  ViewSpec,
} from "../types";

// ---------- Database ----------
export const pickDatabaseFile = (): Promise<string> => DatabaseService.PickDatabaseFile();
/** 打开数据库（可多个并存）。readOnly 为 true 时强制只读；同路径复用连接（reused=true）。 */
export const openDatabase = (path: string, readOnly = false): Promise<DatabaseInfo> =>
  DatabaseService.Open(path, readOnly);
export const closeDatabase = (connId: string): Promise<void> => DatabaseService.Close(connId);
/** 选择备份保存位置（V2.5），取消返回空字符串。 */
export const pickBackupPath = (defaultName: string): Promise<string> =>
  DatabaseService.PickBackupPath(defaultName);
/** VACUUM INTO 在线备份（生成紧凑独立副本，只读连接也可用）。 */
export const backupDatabase = (connId: string, path: string): Promise<BackupResult> =>
  DatabaseService.BackupDatabase(connId, path);

// ---------- Schema ----------
export const getSchema = (connId: string): Promise<SchemaInfo> => SchemaService.GetSchema(connId);
export const getTableStructure = (connId: string, name: string): Promise<TableStructure> =>
  SchemaService.GetTableStructure(connId, name);
export async function getTableColumns(connId: string, name: string): Promise<ColumnInfo[]> {
  const cols = await SchemaService.GetTableColumns(connId, name);
  return cols ?? [];
}
/** 预览表结构操作：SQL/阻断原因/提示（SQL 由后端生成）。 */
export const previewTableOp = (connId: string, op: TableOp): Promise<TableOpPreview> =>
  SchemaService.PreviewTableOp(connId, op);
/** 执行表结构操作，返回已执行的 SQL。 */
export const applyTableOp = (connId: string, op: TableOp): Promise<string> =>
  SchemaService.ApplyTableOp(connId, op);
export const previewCreateTable = (connId: string, spec: CreateTableSpec): Promise<TableOpPreview> =>
  SchemaService.PreviewCreateTable(connId, spec);
export const applyCreateTable = (connId: string, spec: CreateTableSpec): Promise<string> =>
  SchemaService.ApplyCreateTable(connId, spec);
/** 预览新建索引（名称留空自动命名）。 */
export const previewCreateIndex = (
  connId: string,
  spec: CreateIndexSpec,
): Promise<CreateIndexPreview> => SchemaService.PreviewCreateIndex(connId, spec);
/** 执行新建索引，返回实际索引名。 */
export const applyCreateIndex = (connId: string, spec: CreateIndexSpec): Promise<string> =>
  SchemaService.ApplyCreateIndex(connId, spec);
/** 表自身的外键定义（结构页展示）。 */
export async function getTableForeignKeys(
  connId: string,
  table: string,
): Promise<ForeignKeyInfo[]> {
  const list = await SchemaService.GetTableForeignKeys(connId, table);
  return list ?? [];
}
/** 表/视图名 → 列名，SQL 编辑器自动补全数据源。 */
export async function getCompletionSchema(connId: string): Promise<Record<string, string[]>> {
  const raw = await SchemaService.GetCompletionSchema(connId);
  const out: Record<string, string[]> = {};
  for (const [k, v] of Object.entries(raw ?? {})) {
    out[k] = v ?? [];
  }
  return out;
}
export const dropTrigger = (connId: string, name: string): Promise<void> =>
  SchemaService.DropTrigger(connId, name);
/** 预览表重建（改类型/约束/列序/主键/表名/新增外键，V2.4）。 */
export const previewTableRedesign = (connId: string, rd: TableRedesign): Promise<RedesignPreview> =>
  SchemaService.PreviewTableRedesign(connId, rd);
/** 执行表重建（单事务，任一步失败整体回滚）。 */
export const applyTableRedesign = (connId: string, rd: TableRedesign): Promise<void> =>
  SchemaService.ApplyTableRedesign(connId, rd);
export const dropIndex = (connId: string, name: string): Promise<void> =>
  SchemaService.DropIndex(connId, name);
/** 视图定义回填（编辑模式，V3.0）。 */
export const getViewDefinition = (connId: string, name: string): Promise<ViewSpec> =>
  SchemaService.GetViewDefinition(connId, name);
/** 预览新建视图（SQL 由后端生成）。 */
export const previewCreateView = (connId: string, spec: ViewSpec): Promise<ViewOpPreview> =>
  SchemaService.PreviewCreateView(connId, spec);
/** 执行新建视图，返回已执行的 SQL。 */
export const applyCreateView = (connId: string, spec: ViewSpec): Promise<string> =>
  SchemaService.ApplyCreateView(connId, spec);
/** 预览修改视图（DROP+CREATE 单事务；OldName 与 Name 不同即重命名）。 */
export const previewAlterView = (connId: string, spec: ViewSpec): Promise<ViewOpPreview> =>
  SchemaService.PreviewAlterView(connId, spec);
/** 执行修改视图（单事务，任一步失败整体回滚）。 */
export const applyAlterView = (connId: string, spec: ViewSpec): Promise<void> =>
  SchemaService.ApplyAlterView(connId, spec);
/** 预览删除视图（含引用警告）。 */
export const previewDropView = (connId: string, name: string): Promise<ViewOpPreview> =>
  SchemaService.PreviewDropView(connId, name);
export const dropView = (connId: string, name: string): Promise<void> =>
  SchemaService.DropView(connId, name);

// ---------- Data ----------
export const getTableData = (connId: string, req: TableDataRequest): Promise<TableData> =>
  DataService.GetTableData(connId, req);
/** 异步统计行数（可带快速文本过滤与结构化筛选）。大表 COUNT(*) 不阻塞浏览。 */
export const countTableRows = (
  connId: string,
  table: string,
  filter: string,
  conditions?: FilterCondition[] | null,
): Promise<number> => DataService.CountTableRows(connId, table, filter, conditions ?? []);
export const insertRow = (
  connId: string,
  table: string,
  values: Record<string, unknown>,
): Promise<number> => DataService.InsertRow(connId, table, values);
export const updateCell = (
  connId: string,
  table: string,
  key: Record<string, unknown>,
  column: string,
  value: unknown,
): Promise<number> => DataService.UpdateCell(connId, table, key, column, value);
export const deleteRows = (
  connId: string,
  table: string,
  keys: Record<string, unknown>[],
): Promise<number> => DataService.DeleteRows(connId, table, keys);
/** 复制选中的行（单列 INTEGER 主键取新自增值，V2.5）。 */
export const duplicateRows = (
  connId: string,
  table: string,
  keys: Record<string, unknown>[],
): Promise<number> => DataService.DuplicateRows(connId, table, keys);
/** 读取单元格完整 BLOB（列表标记只有 64 KB 预览，V2.5）。 */
export const getCellBlob = (
  connId: string,
  table: string,
  key: Record<string, unknown>,
  column: string,
): Promise<BinaryContent> => DataService.GetCellBlob(connId, table, key, column);

// ---------- File（BLOB 查看器，V2.5） ----------
export const pickBlobOpenPath = (): Promise<string> => FileService.PickBlobOpenPath();
export const readFileBase64 = (path: string): Promise<BinaryContent> =>
  FileService.ReadFileBase64(path);
export const pickBlobSavePath = (defaultName: string): Promise<string> =>
  FileService.PickBlobSavePath(defaultName);
export const writeFileBase64 = (path: string, b64: string): Promise<void> =>
  FileService.WriteFileBase64(path, b64);

// ---------- Query ----------
export const executeSql = (connId: string, sql: string): Promise<QueryResult> =>
  QueryService.Execute(connId, sql);
export const cancelQuery = (connId: string): Promise<void> => QueryService.Cancel(connId);
/** 逐条预检 SQL：语句数、类型与危险标记（执行前确认用，规则与后端一致；纯文本分析）。 */
export async function previewSql(sql: string): Promise<StatementPreview[]> {
  const list = await QueryService.Preview(sql);
  return list ?? [];
}
/** 多语句事务执行：任一失败整体回滚，返回逐条结果。 */
export const executeSqlBatch = (connId: string, sql: string): Promise<BatchResult> =>
  QueryService.ExecuteBatch(connId, sql);

// ---------- Export ----------
export const pickExportPath = (defaultName: string, format: string): Promise<string> =>
  ExportService.PickExportPath(defaultName, format);
export const exportQueryResult = (
  connId: string,
  sql: string,
  format: string,
  path: string,
): Promise<{ path: string; rows: number }> =>
  ExportService.ExportQueryResult(connId, sql, format, path);
export const exportTable = (
  connId: string,
  table: string,
  format: string,
  path: string,
): Promise<{ path: string; rows: number }> =>
  ExportService.ExportTable(connId, table, format, path);

// ---------- Import（CSV / Excel） ----------
/** 弹出文件选择对话框（CSV / Excel），取消返回空字符串。 */
export const pickImportFile = (): Promise<string> => ImportService.PickImportFile();
/** 列出 Excel 工作簿的工作表。 */
export async function listSheets(path: string): Promise<SheetInfo[]> {
  const sheets = await ImportService.ListSheets(path);
  return sheets ?? [];
}
/** 预览表格前 50 行：.xlsx/.xlsm 走 Excel（sheet 为空用第一个），其余走 CSV。 */
export const previewImport = (path: string, sheet: string, hasHeader: boolean): Promise<ImportPreview> =>
  ImportService.PreviewImport(path, sheet, hasHeader);
/** 按列映射导入（单个事务，失败整体回滚）。 */
export const importTable = (connId: string, req: ImportRequest): Promise<ImportResult> =>
  ImportService.Import(connId, req);

// ---------- Recent ----------
export async function listRecent(): Promise<RecentEntry[]> {
  const list = await RecentService.List();
  return list ?? [];
}
export const removeRecent = (path: string): Promise<void> => RecentService.Remove(path);
export const clearRecent = (): Promise<void> => RecentService.Clear();

// ---------- Settings ----------
export const getSettings = (): Promise<Settings> => SettingsService.Get();
export const updateSettings = (next: Settings): Promise<Settings> => SettingsService.Update(next);

// ---------- Transfer（跨连接传输，V3.0） ----------
export const previewTransfer = (req: TransferRequest): Promise<TransferPreview> =>
  TransferService.PreviewTransfer(req);
export const applyTransfer = (req: TransferRequest): Promise<TransferResult> =>
  TransferService.ApplyTransfer(req);
