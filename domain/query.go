package domain

// QueryResult 通用查询结果，同时承载查询行与写入影响。
type QueryResult struct {
	Columns      []string `json:"columns"`
	ColumnTypes  []string `json:"columnTypes"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rowsAffected"`
	LastInsertID int64    `json:"lastInsertId"`
	ElapsedMs    int64    `json:"elapsedMs"`
	IsQuery      bool     `json:"isQuery"`
	Truncated    bool     `json:"truncated"`
}

// StatementPreview 单条语句的预检信息（多语句确认对话框数据源，V1.2）。
type StatementPreview struct {
	SQL       string `json:"sql"`
	Kind      string `json:"kind"` // "query" | "exec"
	Dangerous bool   `json:"dangerous"`
	Reason    string `json:"reason"`
}

// BatchItem 多语句执行中单条语句的结果。
type BatchItem struct {
	SQL          string       `json:"sql"`
	IsQuery      bool         `json:"isQuery"`
	Result       *QueryResult `json:"result,omitempty"` // 查询类语句的结果（受 maxRows 限制）
	RowsAffected int64        `json:"rowsAffected"`
	Error        string       `json:"error,omitempty"` // 非空表示该条失败
}

// BatchResult 多语句执行总结果：全部语句包在一个事务中，
// 任一条失败则整体回滚（FailedIndex >= 0 时 RolledBack 为 true）。
type BatchResult struct {
	Items          []BatchItem `json:"items"`
	FailedIndex    int         `json:"failedIndex"` // -1 表示全部成功
	RolledBack     bool        `json:"rolledBack"`
	TotalElapsedMs int64       `json:"totalElapsedMs"`
}

// FilterOp 筛选操作符（V3.0 列筛选构建器）。取值由后端白名单校验，
// 未知操作符直接报错，防止拼注入。
type FilterOp string

const (
	FilterEq      FilterOp = "eq"      // =
	FilterNe      FilterOp = "ne"      // <>
	FilterGt      FilterOp = "gt"      // >
	FilterGe      FilterOp = "ge"      // >=
	FilterLt      FilterOp = "lt"      // <
	FilterLe      FilterOp = "le"      // <=
	FilterLike    FilterOp = "like"    // LIKE 包含
	FilterRange   FilterOp = "range"   // 区间 [value, value2]
	FilterIsNull  FilterOp = "is-null" // IS NULL
	FilterNotNull FilterOp = "not-null"
)

// FilterCondition 结构化筛选条件（V3.0）。值以字符串传输由后端参数绑定，
// 数值列经 SQLite 列亲和性自动转换（也规避了 JSON 大整数精度问题）。
// range 允许单边（另一边留空即无该侧边界）。
type FilterCondition struct {
	Column string   `json:"column"`
	Op     FilterOp `json:"op"`
	Value  string   `json:"value"`
	Value2 string   `json:"value2"` // 仅 range 使用
}

// TableDataRequest 表数据浏览请求。
// Cursor 非空时按游标分页（默认顺序，深翻页性能与表规模无关）；
// 排序/视图/复合主键等场景后端自动回退 Page 偏移分页。
type TableDataRequest struct {
	Table     string `json:"table"`
	Page      int    `json:"page"` // 偏移分页页码（1 起，仅回退模式使用）
	PageSize  int    `json:"pageSize"`
	OrderBy   string `json:"orderBy"` // 列名或 "__rowid__"，空表示默认顺序
	OrderDesc bool   `json:"orderDesc"`
	Filter    string `json:"filter"` // 快速文本过滤（跨列 LIKE），空表示不过滤
	// Conditions 结构化筛选（V3.0 高级过滤）：多条件 AND，可与 Filter 并存。
	Conditions []FilterCondition `json:"conditions"`
	Cursor     string            `json:"cursor"` // 游标值：上次结果 nextCursor 原样回传；空表示第一页
}

// TableData 表数据分页结果。
// 行数总数不再随页返回（大表 COUNT(*) 代价高），由前端异步调用 CountTableRows。
type TableData struct {
	Columns     []string `json:"columns"`
	ColumnTypes []string `json:"columnTypes"`
	Rows        [][]any  `json:"rows"`
	Page        int      `json:"page"` // 偏移分页时为当前页码；游标分页时为 1
	PageSize    int      `json:"pageSize"`
	ElapsedMs   int64    `json:"elapsedMs"`
	Editable    bool     `json:"editable"`
	KeyColumns  []string `json:"keyColumns"`

	CursorMode bool   `json:"cursorMode"` // 本页是否为游标分页
	NextCursor string `json:"nextCursor"` // 下一页游标（本页最后一行的游标值）
	PrevCursor string `json:"prevCursor"` // 本页首行游标值（刷新当前页时原样回传）
	HasNext    bool   `json:"hasNext"`
	HasPrev    bool   `json:"hasPrev"`
}
