package domain

// TransferRequest 跨连接表传输请求（V3.0）。源/目标是两个已打开连接
// （各为独立 *sql.DB，绝无跨库事务；同一路径时为同一连接，引擎按
// "读一批→写一批" 执行避免占用唯一连接死锁）。
type TransferRequest struct {
	SourceConnID string `json:"sourceConnId"`
	SourceTable  string `json:"sourceTable"`
	TargetConnID string `json:"targetConnId"`
	TargetTable  string `json:"targetTable"`
	// IncludeData false = 仅结构；true = 结构 + 数据。
	IncludeData bool `json:"includeData"`
	// 索引/触发器默认连带（按 DDL 原文重建，表名改写为目标表名）。
	IncludeIndexes  bool `json:"includeIndexes"`
	IncludeTriggers bool `json:"includeTriggers"`
}

// TransferPreview 传输预检：源 DDL 原文、行数预估、目标冲突与提示。
type TransferPreview struct {
	SourceDDL   string   `json:"sourceDdl"`   // 源表 CREATE TABLE 原文
	IndexDDLs   []string `json:"indexDdls"`   // 手动索引 DDL 原文（自动索引随表约束）
	TriggerDDLs []string `json:"triggerDdls"` // 本表触发器 DDL 原文
	ColumnCount int      `json:"columnCount"`
	RowCount    int64    `json:"rowCount"`    // 预计行数（数据量预估）
	TargetTable string   `json:"targetTable"` // 解析后的目标表名（去空白）
	Blocked     string   `json:"blocked,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// TransferResult 传输执行结果。
type TransferResult struct {
	RowsCopied int64 `json:"rowsCopied"`
	ElapsedMs  int64 `json:"elapsedMs"`
}
