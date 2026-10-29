# AGENTS.md — RedSQLite 开发指南

供 AI 代理（或新成员）快速接手本项目。改代码前请先读「架构不变量」与「已知坑」。

## 项目概述

精简版 Navicat，**仅支持 SQLite** 的轻量桌面客户端。优先 Windows 11（含 ARM64 交叉构建）。
技术栈：**Wails v3 (beta) + React 19 + TypeScript + Vite + Go**，驱动 `modernc.org/sqlite`（纯 Go，无 CGO）。
原则：**每连接单文件、默认安全优先、无主键表默认只读、DDL 由后端生成**。
版本状态：**V3.1 已完成**（V2.0：结构编辑+触发器；V2.1：多数据库 Tab；V2.2：主题/只读打开/ARM64；V2.3：设计器外键与生成列/索引创建/SQL 补全；V2.4：表重建引擎；V2.5：Excel 导入导出/数据库备份/BLOB 增强/复制行；V3.0：大整数精度/视图设计器/列筛选构建器/排序游标分页/复合外键重建/重建进度/跨连接传输；V3.1：打开数据库子窗口——最近列表优先）。

## 环境版本（三者必须保持一致，已固定）

| 组件 | 版本 |
|---|---|
| wails3 CLI / Wails Go 库 / @wailsio/runtime | v3.0.0-beta.12 |
| Go / Node | 1.26.1 / 22.19.0 |
| modernc.org/sqlite | v1.44.3 |
| github.com/xuri/excelize/v2 | v2.11.0 |
| React / Zustand / @uiw/react-codemirror / lucide-react | 19.x / 5.x / 4.x / 1.x |

升级 Wails 时三处要同步升级，否则 bindings 生成/运行时不兼容。

## 常用命令

```bash
wails3 dev                                    # 开发模式（热重载）
wails3 task build                             # 生产打包 → bin/redsqlite.exe
wails3 task build:arm64                       # 交叉编译 Windows ARM64 → bin/redsqlite-arm64.exe
wails3 task package                           # NSIS 安装包 → bin/RedSQLite-amd64-installer.exe
                                              # （需 makensis 在 PATH；NSIS 装在 C:\Program Files (x86)\NSIS）
go test ./domain/... ./internal/... ./repository/... ./services/...   # 后端测试
cd frontend && npx tsc --noEmit               # 前端类型检查
cd frontend && npm run build                  # 仅构建前端

# 版本号：Taskfile.yml 的 APP_VERSION、build/windows/info.json、build/windows/nsis/project.nsi 的
# INFO_PRODUCTVERSION 三处同步改（info.json 用四段式 2.5.0.0，语言键必须是 "0409"；project.nsi 用三段式），
# APP_VERSION 经 -ldflags 注入 main.appVersion。

# 改了 Go 服务的方法签名/新增服务后，重新生成 TS bindings：
wails3 generate bindings -clean=true -ts -i   # 输出到 frontend/bindings（勿手改，勿提交前删除）

# 图标流水线（改 icons/redsqlite.svg 后）：
# 1) Chrome 渲染 512px 透明底 PNG（--default-background-color=00000000 必须带，否则白底！）
# 2) 覆盖 build/appicon.png
# 3) cd build && wails3 generate icons -input appicon.png -windowsfilename windows/icon.ico
#    （darwin icns 报错可忽略，Windows 部分会写出）
# 4) wails3 task build 重新打包
```

提交规范：作者用 `-c user.name="RedJACK" -c user.email="redjack@local"`，中文提交信息。

## 目录结构与调用链

```text
main.go               窗口 + 10 个服务注册（勿在服务外再建全局状态）
domain/               领域模型（JSON 标签即前端字段名；tableop.go = 结构操作模型）
internal/
  identifier/         标识符白名单校验 + 引号转义（防注入，所有表名/列名必须过这里）
  sqliteutil/         SQL 规范化/分类/多语句拆分(SplitStatements)/危险检测
  schemadsl/          DDL 生成器（ALTER/CREATE TABLE，V2.0：SQL 唯一来源在后端）
  cell/               单元格值 <-> JSON 编解码（BLOB/截断/bigint 标记，V3.0 含大整数）
  exporter/           CSV/JSON/XLSX 流式导出
  tabular/            CSV/XLSX 共用导入模型与事务导入引擎（V2.5）
  csvimport/          CSV 解析（BOM/分隔符识别，引擎在 tabular）
  xlsximport/         Excel 解析（excelize，工作表/预览/导入）
  logging/            slog 落盘到 %APPDATA%\RedSQLite\logs（5MB 启动轮转）
  appdata/            设置与最近打开（%APPDATA%\RedSQLite\*.json）
repository/           唯一允许触碰 *sql.DB 的层；Store 是多连接注册表（V2.1），
                      仓储按 connID 绑定构造（NewXxxRepository(store, connID)）；
                      transfer_repository.go = 跨连接传输引擎（V3.0）
services/             Database/Schema/Data/Query/Export/Import/File/Recent/Settings/Transfer，
                      暴露给 Wails；连接型方法一律以 connID 为首参；
                      redesign.go = 表重建引擎（V2.4，V3.0 分块进度）；
                      view_designer.go = 视图设计器（V3.0）；
                      transfer_service.go = 跨连接传输（V3.0）；
                      file_service.go = BLOB 查看器的文件读写与对话框（V2.5）
frontend/src/
  api/                bindings 薄封装（组件禁止直接 import bindings；连接型 API 以 connId 开头）
  stores/             connections(多连接+最近)/tabs(按连接归属)/settings/toast/ui（Zustand）
  components/         DataGrid（核心网格）/SchemaTree/ConnectionTabs(数据库 Tab 条)/
                      TableOpFlow(结构操作弹窗流程)/CreateTableModal/CreateIndexModal/
                      RedesignModal(表重设计)/DdlModal/ContextMenu/ImportWizard(CSV+Excel)/
                      BlobViewerModal(BLOB 查看器)/ViewDesignerModal(视图设计器，V3.0)/
                      TransferWizard(跨连接传输向导，V3.0)/TabsBar/Modal 等
  pages/              Welcome/TableData(列筛选构建器，V3.0)/Structure/Query
  utils/              sqlFormat.ts / theme.ts(主题解析+useResolvedTheme) /
                      blobkind.ts(图片魔数识别) / history.ts / constants.ts(事件名等共享常量)
icons/redsqlite.svg   图标矢量源
```

数据流：React 组件 → src/api（带 connId）→ frontend/bindings（生成）→ Go services（connID 首参）→ repository（按连接构造）→ database/sql → SQLite。

## 架构不变量

1. **分层**：服务不直接持有/使用 `*sql.DB`，一律通过 `repository.Store` 的方法；**唯一例外**是导入/导出引擎（`internal/exporter`、`internal/tabular` 及 csvimport/xlsximport，V2.5）：服务经 `Store.DB(connID)` 取出连接交给引擎执行，SQL 全部在引擎内生成，服务层仍不得手写 SQL（见不变量 12）。仓储不缓存跨请求状态，每次调用由服务层按 connID 构造。
2. **多连接**（V2.1）：`Store` 是连接注册表（`map[connID]*Conn`），每连接独立 `*sql.DB` 且 `SetMaxOpenConns(1)`、独立取消 context；**同一路径必须复用同一连接**（`byPath` 索引，否则两个连接互相锁死 SQLITE_BUSY）。连接级 PRAGMA（busy_timeout/foreign_keys/可选 WAL）通过 DSN `_pragma` 参数设置，不是 Exec。只读打开 = 文件不可写 **或** 用户显式勾选（`Open(path, enableWAL, forceReadOnly)`）。
3. **SQL 安全**：值一律参数绑定；表名/列名一律 `identifier.ValidateAndQuote`（PRAGMA 无法参数绑定，只能拼接，所以校验必须先行）。**结构编辑 DDL 只能由 `internal/schemadsl` 生成**：默认值只接受字面量白名单（数字/'文本'/x'十六进制'/NULL），表达式默认值拒绝分号与注释（防语句拆分注入）；前端只展示 SQL，不得自行拼接。
4. **行定位**：主键 → 无主键 rowid 表回退 `rowid`（结果集注入 `__rowid__` 列，见 `identifier.RowidName` 与前端 `utils/constants.ts`）→ 视图/WITHOUT ROWID 只读。主键列与 `__rowid__` 禁止 UI 编辑。
5. **语句边界**：单条走 `QueryService.Execute`（`sqliteutil.ValidateSingle`，多语句拒绝）；多条走 `ExecuteBatch`（`SplitStatements` 拆分 → 单事务逐条执行 → 任一失败整体回滚 → 逐条返回结果）。批量内禁止显式事务控制（BEGIN/COMMIT 等，`IsTransactionControl` 检测）；VACUUM 等无法在事务内运行的语句只能单条执行。含 BEGIN...END 体的 CREATE TRIGGER 必须单独执行（拆分器不识别触发器体）。
6. **单元格编码**：BLOB/超长文本/超 JS 安全整数范围（±2^53-1）的 int64 经 `internal/cell` 转标记对象（`{__kind:"blob"...}` / `{__kind:"bigint", str:"十进制字符串"}`），前端 `types/index.ts` 的 `isBlobMarker/isTruncatedMarker/isBigintMarker` 对应判别；改一处必须同步另一处。前端编辑超范围整数字符串时原样回传（禁止 `parseInt` 丢精度），由 SQLite 列亲和性转换。
7. **写操作安全**：多行删除/多语句/表格导入（CSV/Excel 同引擎）/复制行都走事务；危险 SQL 检测**只在后端**（`QueryService.Preview` → `sqliteutil.AnalyzeDangerous`，逐条返回），前端 QueryPage 用 Preview 结果做确认弹窗——不要再新建前端副本（V1.1 前的 `sqlDanger.ts` 已删除）。结构编辑同理：前端 `useTableOpFlow`/`CreateTableModal`/`RedesignModal` 只调后端 Preview/Apply，阻断原因（主键列/索引覆盖/外键引用/同名冲突/CHECK/生成列等）与提示都由后端返回。**表重建（V2.4）必须走 `PreviewTableRedesign/ApplyTableRedesign`**：单事务 + 提交前 `PRAGMA foreign_key_check` 全库校验；`PRAGMA foreign_keys` 只能在事务外切换（事务内是 no-op），单连接保证无并发窗口；触发器须"先删后建"（RENAME 阶段 SQLite 重解析全库触发器，悬空引用直接报错，**包括其他表上引用本表的触发器**）。
8. **分页**：默认顺序走游标分页（rowid 或单列主键作游标，`WHERE key > cursor LIMIT n+1`）；**排序浏览（V3.0）也走游标**：排序列+游标列复合排序，`(a, b) > (?, ?)` row-value 比较（DESC 同步翻转方向），复合游标以 JSON 数组传输；**排序列可空且实际存在 NULL 时必须回退 LIMIT/OFFSET**（row-value 比较对 NULL 不成立会丢页，用 `LIMIT 1` 探测）；视图/复合主键仍回退 OFFSET。结构化筛选（V3.0 `domain.FilterCondition`）的 WHERE 拼接只在后端（列白名单 + 值参数绑定），`GetTableData` 与 `CountTableRows` 接同一套构造。`GetTableData` **不做** `COUNT(*)`，行数由前端异步调 `CountTableRows`（大表不阻塞翻页）；结构页 `GetTableStructure` 仍是按需同步计数。
9. **前端状态分 store**：连接（含最近列表）/标签页/设置/Toast 各自独立，别合并成大对象。Tab 必须记录 `connId`；表/查询 Tab 只显示当前激活连接的（TabsBar 过滤），连接关闭联动清理其全部 Tab。
10. **bindings 模型可空**：生成的 TS 类型里 slice 均为 `T[] | null`、行可能为 null，前端使用前必须过滤收窄（参考 TableDataPage 的 rows useMemo、QueryPage 的 items 收窄）。
11. **主题**（V2.2）：颜色一律走 `global.css` 的 CSS 变量（浅色 = `:root[data-theme="light"]` 覆盖），新增样式禁止硬编码颜色（`#fff` 反白文字除外）；CodeMirror 等命令式 API 用 `utils/theme.ts` 的 `useResolvedTheme()` 跟随 `data-theme` 属性变化。
12. **二进制内容传输**（V2.5）：前端 WebView 无本地文件权限，BLOB 完整内容与文件读写一律走 Base64（`domain.BinaryContent`），上限 32MB；列表里的 BLOB 标记只带 64KB 预览，查看/另存必须调 `GetCellBlob` 取完整数据。表格导入双后端（CSV/XLSX）共用 `internal/tabular` 的事务引擎，服务层按扩展名路由，别在两处各写一份插入逻辑。
13. **跨连接传输**（V3.0）：只允许在**两个不同连接**之间（`TransferService` 直接拒绝同连接——单连接下目标事务会占住唯一连接，读源数据死锁；同库复制引导用户走 SQL 页）。源/目标各是独立 `*sql.DB`、事务各自开，绝无跨库事务；引擎在 `repository/transfer_repository.go`（读一批→关闭→写一批，`[]byte` 立即拷贝）；DDL 按原文重建（正则只改写表名与 ON 子句）；目标侧 foreign_keys 事务外切换；rowid/自增计数不复制（显式主键列值保留）。
14. **进度事件**（V3.0）：长任务（表重建/传输）经 Wails 事件回报进度——Go `application.Get().Event.Emit(常量名, map)`，前端 `Events.On`（`@wailsio/runtime`），事件名常量在 Go 服务与 `utils/constants.ts` 两处对应；data 必须带连接 ID，前端按连接过滤防多库串扰；**单测环境 `application.Get()` 为 nil，必须判空**否则 panic。

## 已知坑（本项目实测）

- `go build ./...` 会把 `build/ios` 模板包拉进来而失败；构建请指定包列表或直接 `wails3 task build`。
- **`PRAGMA` 列数/列序坑**：`index_list` 返回 5 列（seq,name,unique,origin,partial，origin 是文本）；`foreign_key_list` 返回 **8 列**且 **on_update 在 on_delete 前**（id,seq,table,from,to,on_update,on_delete,match）；`table_xinfo` 是 **pk 在 hidden 前**（cid,name,type,notnull,dflt_value,pk,hidden）——Scan 必须按实际列序全收，少收直接报错 `expected N destination arguments`。
- `GetObjectSQL` 查询 sqlite_master 时过滤了 `sqlite_%` 前缀，查自动索引（sqlite_autoindex_*）会得到"不存在"；`DropIndex` 用前缀预判解决。
- `database/sql` 扫描任意值时 TEXT→string、BLOB→[]byte（modernc），行内值必须立即 `cell.Encode` 拷贝，否则 []byte 在下次 Next 后失效。
- SQLite 的 `ALTER TABLE ADD COLUMN` 限制（schemadsl 已前置校验）：不能加 PK/UNIQUE 约束列、默认值不能是表达式/CURRENT_TIMESTAMP、NOT NULL 必须有非 NULL 默认值。`DROP COLUMN` 对主键/被索引/被外键引用的列失败——服务层预检给出中文原因，别让 SQLite 原始报错直出。
- `RENAME TABLE` 在 foreign_keys=ON（本项目 DSN 默认）下会自动改写引用它的外键/视图/触发器定义，预览弹窗必须提示用户。
- `ALTER TABLE ... RENAME` / `DROP TABLE` 会**重解析全库触发器**：任何触发器（含其他表上的）引用了正被删除/改名的表都会报 "error in trigger X: no such table" —— 表重建必须把本表触发器与引用本表的触发器都先删后建（见 services/redesign.go）。
- `PRAGMA foreign_key_check` 可以在事务内运行，表重建用它做提交前校验；`PRAGMA foreign_keys` 在事务内是 no-op，只能事务外切换。
- AUTOINCREMENT 只允许精确 `INTEGER PRIMARY KEY`（`INT` 都不行）；复合主键要渲染成表级 `PRIMARY KEY (...)`。
- lucide-react 1.x 没有 `FileBinary` 图标（用 `Binary`）；新增图标先确认导出名。
- Wails beta.12：`SaveFileDialog` 没有 `SetTitle`（用 `SetMessage`）；对话框经 `application.Get().Dialog.OpenFile()/SaveFile()`。
- Chrome headless 截图默认白底；图标类渲染必须加 `--default-background-color=00000000`，且透明度要用像素级 Alpha 检查验证（PowerShell System.Drawing）。
- GUI 自动化验证（computer-use）时：WebView2 的 AX 树混有其他 Chromium 窗口的噪声元素，但 Web 内容元素可命中（用元素目标点击）；全屏像素帧常报 stale，坐标点击基本不可用；CodeMirror 不接受 AXSetValue，要先 `Tab` 键聚焦再 `type`；原生文件对话框是可靠路径（对话框 AX 树完整）。
- 生成的 bindings 目录 `frontend/bindings` 依赖 vite 插件 `wails("./bindings")`；dev/build 会自动再生成。
- `build/windows/info.json`：语言键必须是 `"0409"`（模板默认 `"0000"` 会导致 Windows 完全读不到版本资源）；`fixed` 里 `file_version` 与 `product_version` 都要写四段式（缺 `product_version` 时 ProductVersionRaw 为 0 且属性页字符串为空）。
- NSIS（makensis）装在 `C:\Program Files (x86)\NSIS`，winget 安装后当前 shell 的 PATH 不会刷新，跑 `wails3 task package` 前需手动把该目录加进 PATH。
- **NSIS 安装器元数据**：安装器标题/安装目录/快捷方式/卸载项来自 `wails_tools.nsh` 的 `INFO_PRODUCTNAME` 等定义，模板默认值是 "My Product"/"My Company"（`build/config.yml` 的 `info` 段也是模板默认）——真实值在 `project.nsi` 里 `!include "wails_tools.nsh"` 之前显式 `!define` 覆盖（V3.1 修复"Product Setup"问题）；安装目录用 `$PROGRAMFILES64\${INFO_PRODUCTNAME}`（不含公司名层）。**`project.nsi` 必须带 UTF-8 BOM**：makensis 对无 BOM 文件按本地 ANSI 代码页解析，中文注释/© 字符会报 "Bad text encoding" 直接失败。
- Git Bash 的 `$'...'` 无法传递 NUL 字节（字符串在 NUL 处截断），用它 grep 二进制里的 UTF-16 字符串会得到假结果；验证二进制内容用 PowerShell 的 `[Text.Encoding]::Unicode`。
- **excelize 实测坑**（V2.5）：自己写文件时 `GetSheetDimension` 不刷新（Sheets 返回的行数只是信息性参考，真实 Excel 文件才准）；`GetCellType` 对数值单元格返回 `CellTypeUnset`（OOXML 数值无 t 属性，别拿它断言数字）；StreamWriter 写的字符串是 `t="inlineStr"` 不是 sharedString。导入按原始值读：日期是 Excel 序列数（与 CSV 一致按 TEXT 绑定，数值亲和列自动转换）。
- modernc 驱动把**空长度 BLOB** 扫描成 nil []byte，与 NULL 无法区分——取整列 BLOB 时要附带 `(col IS NULL)` 辅助列判别（见 `GetCellBlob`）。
- XLSX 单表上限 1,048,576 行：导出超限给明确错误提示改用 CSV/JSON；XLSX 导入跳过全空行（Excel 尾部常见样式空行）、短行补空单元格（各行列宽不齐是常态，与 CSV 的严格列数检查不同）。
- **单连接死锁（V3.0 实测）**：`SetMaxOpenConns(1)` 下，事务（BeginTx 后未 Commit）持有唯一连接期间，同库**任何**其他查询都会永久阻塞——测试里 `db.Query` 的 rows 不 Close、跨连接传输在源=目标时边开事务边读源，都因此挂死（go test 表现为无输出卡住）。测试中的 rows 必须及时 Close；引擎一律"读一批→关闭→写一批"。
- **SQLite CREATE TRIGGER 不校验 body 引用的表/列**（创建成功，DML 触发时才报 no such table）——删除/修改视图与触发器的引用检查只能做文本扫描（`ContainsIdentifier`）给警告，无法依赖 SQLite 阻断。
- **row-value 比较 `(a,b) > (?,?)` 对 NULL 不成立**（结果 NULL，行被丢弃）——排序浏览的复合游标分页对"可空且实际存在 NULL"的排序列必须回退 OFFSET（`SELECT 1 ... WHERE col IS NULL LIMIT 1` 探测，见 `sortColumnHasNulls`）。
- **Wails 事件（V3.0 实测）**：Go 侧 `application.Get().Event.Emit(name, data)`、前端 `@wailsio/runtime` 的 `Events.On(name, cb)`（回调里 `ev.data`）；单测环境 `application.Get()` 返回 nil，发事件前必须判空。字符串比较的亲和性转换在 row-value 内逐对生效，字符串参数绑定到数值列会自动转数字。
- **bindings 生成器把 Go string 枚举生成 TS enum**（如 `FilterOp` → `FilterOp.FilterEq = "eq"`）：TS 里给枚举类型字段赋值必须用枚举成员，字符串字面量不兼容（编译报错）。
- 工具沙箱下用 PowerShell `Start-Process` 启动的 exe 可能不写真实 `%APPDATA%`（写入被重定向），冒烟验证日志横幅时直接前台运行 exe（`./bin/redsqlite.exe &`）更可靠。

## 测试要求

- 改 Go 后端：跑全部 `go test`（repository 集成测试会建真实临时 SQLite 文件，覆盖分页/编辑/删除/查询/只读判定/多连接并存与同路径复用；services 覆盖结构编辑预览的阻断与提示）。
- 改前端：`npx tsc --noEmit` 必须零错误（strict + noUnusedLocals）。
- 发布/交付前：`wails3 task build` 成功 + 手动冒烟（打开库 → 浏览表 → 结构编辑一次 → 执行 SQL → 开第二个库验证 Tab 切换 → 切浅色主题目检对比度）。

## 路线图状态（V3 已完成，2026-08）

已完成迭代：

- **V1.1 可分发 + 大表性能**：NSIS 安装包（`wails3 task package`）、版本号单一来源（Taskfile `APP_VERSION`）、slog 落盘日志 + 启动版本横幅、行数统计异步化（翻页不再 `COUNT(*)`）、默认顺序 rowid/主键游标分页（排序仍 OFFSET，见不变量 8）。
- **V1.2 编辑体验与 SQL 增强**：多语句执行（事务 + 逐条结果 + 失败整体回滚 + 逐条危险确认）、CSV 导入向导（选文件 → 预览 50 行 → 列映射 → 事务插入，失败回滚）、查询中止反馈（计时器、区分中止/超时、执行中编辑器只读）、SQL 格式化（`utils/sqlFormat.ts`）。
- **V2.0 表结构可视化编辑**：`internal/schemadsl` 生成 ALTER/CREATE TABLE（加列/改列名/删列/改表名/删表 + 新建表设计器），SQL 预览弹窗（阻断原因/警告），预检主键/索引覆盖/外键引用/同名冲突；触发器分组显示 + 触发器/索引 DDL 查看与删除；结构页右键菜单；Tab 随表重命名/删除联动。
- **V2.1 多数据库 Tab**：Store 连接注册表（每连接单 *sql.DB + 独立取消；同路径复用防自锁）、全部服务 connID 化、前端 connectionsStore + 数据库连接 Tab 条、关闭连接联动清理 Tab、DatabaseInfo 增加 connId/reused。
- **V2.2 主题与分发**：主题切换（跟随系统/浅色/深色，CodeMirror 联动）、以只读方式打开（显式勾选）、`wails3 task build:arm64` Windows ARM64 交叉构建。
- **V2.3 设计器补全**：新建表支持外键（表级 FOREIGN KEY + 动作白名单）与生成列（VIRTUAL/STORED）；索引可视化创建（自动命名/同列重复警告，结构页与对象树入口）；结构页外键区块；SQL 编辑器接入真实表/列名补全（GetCompletionSchema，随结构刷新重载）；NSIS 安装包在 V2 代码下重新打包验证。
- **V2.4 表重建引擎**：`services/redesign.go` 实现"建新表→拷数据→删旧改名→重建索引/触发器"的单事务重建（提交前全库 foreign_key_check，失败整体回滚）；支持改类型/NOT NULL/默认值/列序/主键/表名/删列/新增列回填默认值/新增外键；保守阻断（CHECK/COLLATE/生成列/复合外键/部分与表达式索引涉改动/被引用表重命名）；RedesignModal 列网格 + 上下移排序 + 多语句 SQL 预览。
- **V2.5 数据链路增强**：Excel(.xlsx) 导入（`internal/tabular` 共享事务引擎 + `internal/xlsximport`，向导选工作表/预览/列映射）；Excel 导出（excelize 流式写入保留类型，超行上限明确报错）；数据库在线备份（VACUUM INTO，目标不可覆盖，只读连接可用）；BLOB 单元格增强（完整内容在线查看、图片魔数预览、另存为/从文件替换，FileService 经 Base64 传输）；表数据页复制行（单列 INTEGER 主键自动取新自增值，约束冲突整体回滚）。
- **V3.0 跨库传输 / 视图设计器 / 筛选构建器 / 债务清理**：>2^53 大整数 bigint 标记编码（cell 包 + 前端类型/编辑/游标全链路）；视图设计器（Preview/ApplyCreateView、AlterView=DROP+CREATE 单事务、DropView，ViewDesignerModal + SchemaTree 视图区右键 + Tab 联动）；列筛选构建器（`domain.FilterCondition` 操作符白名单，后端 WHERE 构造，与游标/排序/快速过滤组合）；排序浏览的复合游标分页（排序列+游标列 row-value 比较，NULL 排序列回退 OFFSET）；表重建支持复合外键（(id,seq) 聚合原文重建，涉删改名列阻断）；大表重建按 rowid 分块拷贝 + `redesign:progress` 进度事件；跨连接传输 TransferService（预检冲突/依赖提示，目标单事务分批写入 + `transfer:progress`，同连接拒绝防死锁，TransferWizard 向导 + 表右键入口）。

下一步候选（按优先级，勿跳级实现非目标项）：

- P3：EXPLAIN 查询计划展示、运行 SQL 文件 / 转储 SQL 文件（V3.0 未纳入）、排序游标分页对 NULL 列的支持（IS NULL 感知的 seek 谓词）
- 测试补强：前端组件测试、损坏数据库/锁定数据库场景、干净 Win11 虚拟机安装包验收（V1.1 遗留的手工项）、ARM64 真机验收
- 已知限制：含 BEGIN...END 的 CREATE TRIGGER 不能在多语句中执行；排序浏览对含 NULL 的排序列/视图/复合主键仍走 OFFSET；同一路径以不同模式二次打开时沿用首次模式；表重建不支持含 CHECK/COLLATE/生成列的表、复合外键涉删改名列时阻断；跨连接传输不保留 rowid/自增计数、不支持覆盖目标同名表；Excel 导入日期按序列数处理（不做格式化解析）、.xls 旧格式不支持；BLOB 在线查看/文件传输上限 32MB
- 分发待办：干净虚拟机安装验证、可选代码签名（`wails3 task sign:installer`，需证书）

## 参考

- 原始开发文档（V1 规划，定义了目标/非目标/阶段划分，用户会话中提供）
- Wails v3 文档：https://v3.wails.io/（beta API 变化快，以当前安装版本源码 `pkg/application` 为准）
