# RedSQLite

精简版 Navicat —— 仅支持 SQLite 的轻量桌面客户端。基于 **Wails v3 + React 19 + TypeScript**，优先面向 Windows 11（支持 ARM64 交叉构建）。

## 功能（V3.1）

- 打开本地 `.db` / `.sqlite` / `.sqlite3` 文件（原生文件对话框 + 最近打开列表），**支持以只读方式打开**（显式勾选）
- **多数据库 Tab（V2.1）**：同时打开多个数据库文件，顶部数据库 Tab 条切换，同一路径自动复用连接；每连接独立的表/查询 Tab；**「+」先弹打开数据库子窗口（V3.1）——最近列表优先、浏览文件兜底、只读开关、已打开标记**
- 左侧结构树：表 / 视图 / 索引 / 触发器；表右键菜单（浏览/设计/传输/重命名/删除）、新建表；视图区新建/编辑/删除（V3.0）
- **视图设计器（V3.0）**：CodeMirror 编辑 SELECT 主体 + SQL 预览确认；新建 / 编辑（DROP+CREATE 单事务原子替换）/ 删除；重命名视图、删除前扫描引用它的视图/触发器给出失效警告（SQLite 运行时才报错，提前提示）
- **表结构可视化编辑（V2.0）**：改列名、加列、删列、改表名、删表、新建表设计器——所有操作先生成 SQL 预览（含阻断原因与影响提示）再执行；触发器/索引 DDL 查看与删除
- **设计器补全（V2.3）**：新建表支持外键（ON DELETE/UPDATE 动作）与生成列（VIRTUAL/STORED）；索引可视化创建（自动命名、同列重复警告）；结构页外键区块；SQL 编辑器自动补全真实表/列名
- **表重建引擎（V2.4，V3.0 增强）**：重新设计已有表——改列类型/NOT NULL/默认值、调整列序、更换主键、删列、新增列（默认值回填）、新增外键、表改名；单事务执行（建新表→拷数据→删旧改名→重建索引/触发器），提交前全库外键校验，任一步失败整体回滚；**多列复合外键按原文重建（V3.0）**；**大数据量按 rowid 分块拷贝并实时回报进度（V3.0）**
- 表数据浏览：分页（默认 100 行/页）、排序、文本过滤、NULL/BLOB/超长文本安全展示；游标分页（V1.1 默认顺序，**V3.0 扩展到排序浏览**：排序列+游标列 row-value 复合比较，深翻页不减速），行数统计异步进行
- **列筛选构建器（V3.0）**：按列/操作符组合结构化条件（= ≠ > ≥ < ≤ 包含/范围/为空/非空，多条件 AND），WHERE 由后端构造（列白名单 + 参数绑定），与排序/快速过滤可叠加
- **大整数精度（V3.0）**：超过 JS 安全整数范围（±2^53-1）的 INTEGER 以字符串精确传输与显示，编辑回传不丢精度
- 表数据编辑：单元格编辑（双击）、新增行、删除行（事务 + 二次确认）；无主键表自动回退 `rowid` 定位，视图与无定位字段对象默认只读
- SQL 查询页：CodeMirror 编辑器（SQL 高亮/补全，随主题切换）、Ctrl+Enter 执行、多语句执行（分号分隔，事务包裹、逐条结果、任一失败整体回滚）、SQL 格式化、查询中止（实时计时反馈）、查询历史、结果导出
- **跨连接数据传输（V3.0）**：在两个已打开连接之间复制表（仅结构 / 结构+数据，索引触发器默认连带）；预检目标同名冲突/外键依赖/行数预估；目标单事务分批写入，失败整体回滚；进度实时回报
- **主题切换（V2.2）**：跟随系统 / 浅色 / 深色
- 导出：CSV（UTF-8 + BOM，Excel 无乱码）/ JSON / **Excel .xlsx（V2.5，保留数值/布尔类型，流式写出）**，整表或查询结果
- 导入：CSV / **Excel .xlsx 向导（V2.5，Excel 先选工作表）**：选文件 → 预览前 50 行 → 列映射 → 事务插入，失败整体回滚；CSV 自动识别分隔符与 BOM，Excel 跳过空行/补齐短行
- **数据库备份（V2.5）**：`VACUUM INTO` 在线备份生成紧凑独立副本（工具栏一键备份，只读连接可用，目标文件不可覆盖）
- **BLOB 增强（V2.5）**：单元格 BLOB 点击打开查看器——完整内容在线读取、常见图片格式（PNG/JPEG/GIF/BMP/WebP/ICO）直接预览、另存为文件、从文件替换
- **复制行（V2.5）**：表数据页复制选中行；单列 INTEGER 主键自动取新自增值，约束冲突整体回滚
- 安全：标识符白名单校验 + 引号转义、参数绑定、结构编辑 DDL 由后端统一生成（默认值字面量白名单）、危险 SQL 逐条确认（后端统一检测）、查询超时与最大行数限制
- 设置：每页行数、查询超时、最大返回行数、WAL 开关（可选，默认关闭）、界面主题
- 诊断：错误日志落盘到 `%APPDATA%\RedSQLite\logs\redsqlite.log`，启动打印版本信息

## 开发环境（固定版本）

| 组件 | 版本 |
|---|---|
| Windows | 11 (10.0.26200) amd64 |
| Go | 1.26.1 |
| Node.js | v22.19.0 (npm 11.14.1) |
| Wails CLI (`wails3`) | v3.0.0-beta.12 |
| Wails Go 库 | v3.0.0-beta.12 |
| `@wailsio/runtime` | 3.0.0-beta.12 |
| React | 19.2.8 |
| TypeScript | 5.x + Vite 8 |
| `modernc.org/sqlite` | v1.44.3（纯 Go，无 CGO） |
| Zustand / CodeMirror / lucide-react | 5.x / 6.x (@uiw 4.x) / 1.x |

## 快速开始

```bash
# 安装 Wails CLI（与库版本保持一致）
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.12

# 开发模式（热重载）
wails3 dev

# 生产构建（产物：bin/redsqlite.exe）
wails3 task build

# Windows ARM64 交叉构建（产物：bin/redsqlite-arm64.exe）
wails3 task build:arm64

# NSIS 安装包（产物：bin/RedSQLite-amd64-installer.exe，需要安装 NSIS/makensis）
wails3 task package

# 运行测试
go test ./domain/... ./internal/... ./repository/... ./services/...
cd frontend && npx tsc --noEmit
```

> `wails3 task build` 会依次执行：前端 `npm install` + `vite build` → 生成 TS bindings → 打入 Windows 图标/清单/版本信息 → `go build`。
> 版本号在 `Taskfile.yml` 的 `APP_VERSION` 与 `build/windows/info.json` 两处维护（保持一致），经 `-ldflags` 注入二进制，启动日志可见。

## 项目结构

```text
├── main.go                 Wails 应用入口（窗口 + 服务注册）
├── domain/                 领域模型（数据库/结构/查询/设置/表结构操作）
├── internal/
│   ├── identifier/         标识符校验与安全引用（防注入）
│   ├── sqliteutil/         SQL 规范化、分类、多语句拆分、危险检测
│   ├── schemadsl/          DDL 生成器（ALTER/CREATE TABLE，V2.0）
│   ├── cell/               单元格值 JSON 编解码（BLOB/截断标记）
│   ├── exporter/           CSV / JSON / XLSX 流式导出
│   ├── tabular/            CSV/XLSX 共用导入模型与事务引擎（V2.5）
│   ├── csvimport/          CSV 解析（BOM/分隔符识别）
│   ├── xlsximport/         Excel 解析（excelize：工作表/预览/导入）
│   ├── logging/            slog 日志落盘（%APPDATA%\RedSQLite\logs）
│   └── appdata/            设置与最近打开（%APPDATA%\RedSQLite）
├── repository/             SQLite 数据访问（Store 多连接注册表，V2.1；含跨连接传输引擎，V3.0）
├── services/               10 个 Wails 服务（Database/Schema/Data/Query/Export/Import/File/Recent/Settings/Transfer）
└── frontend/               React + TS（Vite）
    ├── bindings/           wails3 生成的 TS 绑定（勿手改）
    └── src/
        ├── api/            绑定薄封装（组件不直接依赖 bindings）
        ├── stores/         Zustand（连接/标签页/设置/提示）
        ├── components/     DataGrid、结构树、连接 Tab 条、结构操作弹窗、视图设计器、
        │                   传输向导、导入向导、BLOB 查看器、Tab、模态、Toast 等
        ├── pages/          欢迎页、表数据、表结构、SQL 查询
        └── styles/         全局样式（深色 + 浅色主题变量）
```

## 设计要点

- **多连接（V2.1）**：`Store` 连接注册表，每连接独立 `*sql.DB` 且 `SetMaxOpenConns(1)`、独立取消 context；同一路径复用同一连接（避免自锁）；连接级 PRAGMA（`busy_timeout=5000`、`foreign_keys=ON`，WAL 可选）通过 DSN `_pragma` 参数保证生效
- **行定位**：优先主键；无主键的 rowid 表回退 `rowid`（结果集中注入 `__rowid__` 列）；视图 / WITHOUT ROWID 无定位列 → 只读
- **游标分页（V1.1，V3.0 扩展）**：默认顺序按 rowid/单列主键游标翻页（`WHERE key > cursor`）；排序浏览按 排序列+游标列 复合排序，用 row-value 比较 `(a, b) > (?, ?)` 定位（DESC 同步翻转方向）；排序列含 NULL 或视图/复合主键场景回退 LIMIT/OFFSET。行数统计异步获取，翻页不等 `COUNT(*)`
- **多语句执行（V1.2）**：分号拆分（字符串/注释内的分号不算）→ 单事务逐条执行 → 逐条展示结果；任一条失败整体回滚。危险语句逐条列出原因并二次确认（检测在后端，单一规则来源）
- **结构编辑（V2.0-V2.4）**：DDL 由后端 `schemadsl` 生成（默认值字面量白名单、表达式拒绝分号/注释），前端只预览确认；删列前预检主键/索引/外键引用并给中文原因；重命名表自动改写外键与视图/触发器定义（预览时提示）。表重建（V2.4）单事务执行：事务外切换外键开关（单连接无竞态）、触发器先删后建（规避 RENAME 重解析悬空引用）、提交前 `PRAGMA foreign_key_check` 全库校验
- **只读打开**：文件不可写时自动以 `mode=ro` 打开，或用户在欢迎页显式勾选「以只读方式打开」；状态栏标记「只读」
- **编辑安全**：值全部参数绑定；表名/列名白名单校验后加引号；多行删除/多语句/表格导入/复制行全部走事务；主键/rowid 列禁止 UI 编辑
- **数据链路（V2.5）**：CSV/Excel 导入共用同一事务引擎（`internal/tabular`，按扩展名路由）；XLSX 导出用 excelize StreamWriter 流式写入（超 1,048,576 行明确报错）；BLOB 完整内容与文件读写经 Base64 走绑定传输（32MB 上限），WebView 无本地文件权限
- **单元格编码（V3.0）**：BLOB/超长文本/超安全范围大整数经 `internal/cell` 转标记对象传输（`{__kind:"bigint", str:"十进制字符串"}`），前端类型判别一一对应；编辑超范围整数字符串原样回传由 SQLite 亲和性转换
- **跨连接传输（V3.0）**：两个连接各是独立 `*sql.DB`、事务各自开，绝无跨库事务；源侧按 rowid 范围分批读（WITHOUT ROWID 走 OFFSET），目标单事务建表→拷数据→建索引/触发器，失败整体回滚；DDL 按原文重建（正则改写表名与 ON 子句）；目标侧 foreign_keys 事务外切换；同连接传输直接拒绝（单连接下目标事务会占死唯一连接）
- **进度事件（V3.0）**：表重建/跨连接传输经 Wails 事件（`redesign:progress` / `transfer:progress`）回报 `{phase, copied, total}`，前端订阅显示进度条；事件按连接 ID 过滤防多库串扰
- **大结果保护**：单次查询默认最多 10000 行（可设置），超出标记截断；单元格文本最多展示 1MB；BLOB 以 Base64 预览
- **日志（V1.1）**：`log/slog` 写入 `%APPDATA%\RedSQLite\logs\redsqlite.log`（5MB 启动轮转，保留 3 份），启动打印版本/运行环境，出错自动记录

## 已知限制（按规划留待后续版本）

- NSIS 安装包仅 amd64（ARM64 可用 `wails3 task build:arm64` 出绿色 exe，安装包待做）；安装包尚未在干净 Win11 虚拟机验收
- 多语句不支持含 `BEGIN...END` 体的 `CREATE TRIGGER`（拆分按分号，请单独执行）；`VACUUM` 等事务外语句也只能单条执行
- 排序浏览的游标分页在排序列含 NULL、视图、复合主键场景回退 LIMIT/OFFSET（row-value 比较对 NULL 不成立）
- 表重建不支持含 CHECK/COLLATE 约束、生成列的表（会明确阻断而非静默丢失定义）；复合外键涉及删列/改名列时同样阻断；部分索引/表达式索引涉及列改名或删除时阻断
- 跨连接传输不复制 rowid/自增计数（目标侧行自动分配，显式主键值仍保留）；不支持覆盖目标同名表；索引/触发器名冲突时整体回滚
- 同一路径以不同模式二次打开时沿用首次打开的模式（写模式优先）
- Excel 导入按原始值处理（日期为 Excel 序列数，不做格式化解析）；不支持 .xls 旧二进制格式；BLOB 在线查看与文件传输上限 32MB（图片预览超 10MB 只显示信息）
