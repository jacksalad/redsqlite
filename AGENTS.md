# AGENTS.md — RedSQLite 开发指南

供 AI 代理（或新成员）快速接手。改代码前先读「架构不变量」与「已知坑」。

## 项目概述

精简版 Navicat，仅支持 SQLite 的轻量桌面客户端，优先 Windows 11（含 ARM64 交叉构建）。
技术栈：**Wails v3 (beta) + React 19 + TypeScript + Vite + Go**，驱动 `modernc.org/sqlite`（纯 Go，无 CGO）。
原则：**每连接单文件、默认安全优先、无主键表默认只读、DDL 由后端生成**。
当前版本 1.0.0（版本号三处同步，见「常用命令」）。

## 环境版本（Wails 三件套必须一致）

| 组件 | 版本 |
|---|---|
| wails3 CLI / Wails Go 库 / @wailsio/runtime | v3.0.0-beta.12 |
| Go / Node | 1.25+ / 22+ |
| modernc.org/sqlite | v1.44.3 |
| github.com/xuri/excelize/v2 | v2.11.0 |
| React / Zustand / @uiw/react-codemirror / lucide-react | 19.x / 5.x / 4.x / 1.x |

## 常用命令

```bash
wails3 dev                                    # 开发模式（热重载）
wails3 task build                             # 生产打包 → bin/redsqlite.exe
wails3 task build:arm64                       # ARM64 交叉编译 → bin/redsqlite-arm64.exe
wails3 task package                           # NSIS 安装包（makensis 需在 PATH，装在 C:\Program Files (x86)\NSIS）
go test ./domain/... ./internal/... ./repository/... ./services/...   # 后端测试
cd frontend && npx tsc --noEmit               # 前端类型检查
cd frontend && npm test                       # 前端单测（vitest）
# 版本号：Taskfile.yml APP_VERSION、build/windows/info.json（四段式）、build/windows/nsis/project.nsi（三段式）三处同步改
# 重生成 TS bindings（改了 Go 服务签名后）：wails3 generate bindings -clean=true -ts -i → frontend/bindings（勿手改）
# 图标：改 icons/redsqlite.svg 后用 Chrome 渲染 512px 透明底 PNG（--default-background-color=00000000 必须带）
#   → 覆盖 build/appicon.png → wails3 generate icons → 重新打包
```

提交规范：作者用 `-c user.name="RedJACK" -c user.email="redjack@local"`，中文提交信息。

## 目录结构与调用链

```text
main.go               窗口 + 服务注册（勿在服务外再建全局状态）
domain/               领域模型（JSON 标签即前端字段名）
internal/             identifier(防注入)/sqliteutil(SQL 分析)/schemadsl(DDL 生成)/
                      cell(单元格编码)/exporter/tabular/csvimport/xlsximport/logging/appdata
repository/           唯一触碰 *sql.DB 的层；Store = 多连接注册表；仓储按 connID 构造；
                      transfer_repository.go = 跨连接传输引擎
services/             连接型方法一律以 connID 为首参；redesign.go = 表重建引擎；
                      view_designer.go = 视图设计器；file_service.go = BLOB 文件读写
frontend/src/         api(bindings 薄封装，组件禁止直连 bindings)/stores(Zustand，按连接/标签/设置分 store)/
                      components/pages/utils(rows.ts 行收窄、rowKeys.ts 行定位键、constants.ts 事件名常量)
```

数据流：React 组件 → src/api（带 connId）→ frontend/bindings（生成）→ Go services（connID 首参）→ repository → database/sql。

## 架构不变量

1. **分层**：服务不直接持有 `*sql.DB`，一律经 `repository.Store`；仓储不缓存跨请求状态。
2. **多连接**：每连接独立 `*sql.DB` + `SetMaxOpenConns(1)` + 独立取消 context；**同一路径必须复用同一连接**（`byPath` 索引，否则互相锁死 SQLITE_BUSY）；连接级 PRAGMA 经 DSN `_pragma` 设置，不是 Exec。
3. **SQL 安全**：值一律参数绑定；表名/列名一律 `identifier.ValidateAndQuote`。**结构编辑 DDL 只能由 `internal/schemadsl` 生成**（默认值字面量白名单，防语句拆分注入）；前端只展示 SQL 不得拼接。
4. **行定位**：主键 → rowid 表回退 `rowid`（注入 `__rowid__` 列）→ 视图/WITHOUT ROWID 只读；主键列与 `__rowid__` 禁止 UI 编辑。
5. **语句边界**：单条走 `Execute`（多语句拒绝）；多条走 `ExecuteBatch`（拆分→单事务→失败整体回滚）；批量内禁止 BEGIN/COMMIT；VACUUM 只能单条；含 BEGIN...END 的 CREATE TRIGGER 必须单独执行。
6. **单元格编码**：BLOB/超长文本/超 ±2^53-1 的 int64 经 `internal/cell` 转标记对象，前端 `types/index.ts` 判别函数一一对应；编辑超范围整数字符串原样回传（禁止 parseInt），靠 SQLite 亲和性转换。
7. **写操作安全**：多行删除/多语句/导入/复制行都走事务；危险 SQL 检测**只在后端**（`AnalyzeDangerous`），前端用 Preview 结果做确认；结构编辑同理，阻断原因由后端返回。**表重建必须走 `PreviewTableRedesign/ApplyTableRedesign`**：单事务 + 提交前 `PRAGMA foreign_key_check`；`PRAGMA foreign_keys` 只能事务外切换；触发器先删后建（RENAME 会重解析全库触发器，含其他表上引用本表的）。
8. **分页**：默认顺序走游标分页；排序浏览走排序列+游标列 row-value 复合比较（DESC 翻转方向）；**排序列可空且实际有 NULL 必须回退 OFFSET**（row-value 对 NULL 不成立）；结构化筛选的 WHERE 只在后端构造（列白名单+参数绑定）；行数由前端异步 `CountTableRows`，`GetTableData` 不做 COUNT。
9. **前端分 store**：连接/标签/设置/Toast 各自独立；Tab 必须带 `connId`；跨 store 联动走对方动作（如 `focusConn`），不直写内部状态。
10. **bindings 可空**：生成的类型 slice 为 `T[] | null`、行可能为 null，用前必须收窄（结果网格统一 `utils/rows.ts` 的 `narrowRows`）。
11. **主题**：颜色只走 `global.css` CSS 变量（禁止硬编码；带透明度用 `color-mix`）；CodeMirror 用 `useResolvedTheme()` 跟随。
12. **二进制传输**：BLOB 完整内容与文件读写走 Base64（32MB 上限）；列表 BLOB 标记只带 64KB 预览，完整数据调 `GetCellBlob`；CSV/XLSX 导入共用 `internal/tabular` 引擎，勿写两份。
13. **跨连接传输**：只在两个不同连接间（同连接直接拒绝，会死锁）；源/目标事务各自开，绝无跨库事务；读一批→关闭→写一批；DDL 按原文重建（正则只改表名与 ON 子句）。
14. **进度事件**：表重建/传输经 Wails 事件回报（事件名常量 Go 与 `utils/constants.ts` 两处对应；表重建键 `connId`、传输键 `targetConnId`）；**单测环境 `application.Get()` 为 nil 必须判空**。

## 已知坑（本项目实测）

- `go build ./...` 会拉进 `build/ios` 模板而失败；构建指定包列表或用 `wails3 task build`。
- **PRAGMA 列序坑**：`index_list` 5 列（origin 是文本）；`foreign_key_list` 8 列且 **on_update 在 on_delete 前**；`table_xinfo` 是 pk 在 hidden 前——Scan 按实际列序全收，少收报 `expected N destination arguments`。
- modernc：TEXT→string、BLOB→[]byte，行内值须立即 `cell.Encode` 拷贝；**空 BLOB 扫成 nil 与 NULL 无法区分**（取整列 BLOB 附 `(col IS NULL)` 辅助列，见 `GetCellBlob`）。
- `ALTER TABLE ADD COLUMN`：不能加 PK/UNIQUE 列、默认值不能是表达式、NOT NULL 须有默认值；`DROP COLUMN` 对主键/被索引/被外键引用列失败——服务层预检给中文原因。
- `RENAME/DROP TABLE` 会**重解析全库触发器**，悬空引用（含其他表上的）直接报错——重建须把相关触发器先删后建。
- `PRAGMA foreign_key_check` 可在事务内跑；`PRAGMA foreign_keys` 在事务内是 no-op。
- AUTOINCREMENT 只允许精确 `INTEGER PRIMARY KEY`；复合主键渲染成表级 `PRIMARY KEY (...)`。
- **单连接死锁**：`SetMaxOpenConns(1)` 下事务未提交期间同库任何查询永久阻塞——测试的 rows 必须及时 Close；引擎一律"读一批→关闭→写一批"（go test 表现为无输出卡住）。
- row-value 比较 `(a,b) > (?,?)` 对 NULL 不成立（行被丢）——可空排序列用 `LIMIT 1` 探测 NULL 后回退 OFFSET。
- **Wails 事件**：Go `application.Get().Event.Emit(name, data)` / 前端 `Events.On`（回调 `ev.data`）；单测判空，见不变量 14。
- bindings 生成器把 Go string 枚举生成 TS enum：赋值必须用枚举成员，字符串字面量编译报错。
- Wails beta.12：`SaveFileDialog` 用 `SetMessage` 没有 `SetTitle`；对话框经 `application.Get().Dialog.OpenFile()/SaveFile()`。
- lucide-react 1.x 无 `FileBinary`（用 `Binary`）；新增图标先确认导出名。
- excelize：`GetSheetDimension` 不刷新；数值单元格 `GetCellType` 返回 Unset（别拿它断言数字）；StreamWriter 写的是 inlineStr；导入按原始值读（日期为序列数）。XLSX 单表上限 1,048,576 行。
- `build/windows/info.json`：语言键必须 `"0409"`；`fixed` 的 `file_version` 与 `product_version` 都要写四段式。
- NSIS：winget 安装后 PATH 不刷新，跑 package 前手动加 `C:\Program Files (x86)\NSIS`；安装器元数据在 `project.nsi` 里 `!include "wails_tools.nsh"` 之前 `!define` 覆盖；**`project.nsi` 必须带 UTF-8 BOM** 否则中文注释报 "Bad text encoding"。
- `GetObjectSQL` 过滤了 `sqlite_%` 前缀，自动索引查不到"存在"；`DropIndex` 用前缀预判。
- SQLite CREATE TRIGGER 不校验 body 引用——视图/触发器引用检查只能文本扫描（`ContainsIdentifier`）给警告。

## 测试要求

- 改 Go：跑全部 `go test`（repository 建真实临时 SQLite 文件；services 覆盖结构编辑阻断；internal 覆盖导出保真/列去重/危险检测）。
- 改前端：`npx tsc --noEmit` 零错误；纯逻辑改动跑 `npm test`。
- 发布前：`wails3 task build` 成功 + 手动冒烟（打开库→浏览表→结构编辑一次→执行 SQL→开第二个库验证 Tab 切换→切浅色主题目检）。

## 后续方向

- EXPLAIN 查询计划展示、运行/转储 SQL 文件、排序游标分页对 NULL 列的支持（IS NULL 感知 seek 谓词）
- 重构候选：拆分 `buildRedesign`（约 500 行）与 `GetTableData`（约 240 行）、rowid 分块引擎与 PRAGMA 扫描去重、前端 ForeignKeyEditor/useProgressEvent 公共组件、schema_repository 按职责拆包
- 测试补强：前端组件测试、`query_service` 并发取消注册表、损坏/锁定数据库场景、干净 Win11 虚拟机安装验收、ARM64 真机验收

## 参考

- Wails v3 文档：https://v3.wails.io/（beta API 变化快，以当前安装版本源码 `pkg/application` 为准）
