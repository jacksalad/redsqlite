package domain

// TableOpKind 表结构操作类型（V2.0 可视化结构编辑）。
type TableOpKind string

const (
	OpAddColumn    TableOpKind = "addColumn"
	OpRenameColumn TableOpKind = "renameColumn"
	OpDropColumn   TableOpKind = "dropColumn"
	OpRenameTable  TableOpKind = "renameTable"
	OpDropTable    TableOpKind = "dropTable"
)

// ColumnSpec 新增列/新建表的列定义（前端表单输入，SQL 由后端生成）。
type ColumnSpec struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	NotNull             bool   `json:"notNull"`
	PrimaryKey          bool   `json:"primaryKey"`
	AutoIncrement       bool   `json:"autoIncrement"`
	Unique              bool   `json:"unique"`
	Default             string `json:"default"`
	DefaultIsExpression bool   `json:"defaultIsExpression"`
	// 生成列（V2.3，仅新建表）：Generated 非空时按表达式生成列渲染，
	// 此时不允许默认值/主键/自增/唯一。
	Generated       string `json:"generated"`
	GeneratedStored bool   `json:"generatedStored"`
}

// TableOp 单个表结构操作。
type TableOp struct {
	Kind       string      `json:"kind"`
	Table      string      `json:"table"`
	Column     string      `json:"column,omitempty"`
	NewName    string      `json:"newName,omitempty"`
	ColumnSpec *ColumnSpec `json:"columnSpec,omitempty"`
}

// TableOpPreview 操作预览：待执行 SQL、阻断原因（非空即禁止）、提示信息。
type TableOpPreview struct {
	SQL      string   `json:"sql"`
	Warnings []string `json:"warnings,omitempty"`
	Blocked  string   `json:"blocked,omitempty"`
}

// ForeignKeySpec 外键定义（V2.3，新建表/重设计）。
type ForeignKeySpec struct {
	Column    string `json:"column"`    // 本表列
	RefTable  string `json:"refTable"`  // 被引用表
	RefColumn string `json:"refColumn"` // 被引用列；空 = 引用父表主键
	OnDelete  string `json:"onDelete"`  // 动作白名单见 schemadsl；空 = 不写子句
	OnUpdate  string `json:"onUpdate"`
	// 复合外键（V3.0 表重建内部使用）：非空时按列序渲染
	// FOREIGN KEY (Columns...) REFERENCES RefTable (RefColumns...)，
	// 此时单列字段 Column/RefColumn 不参与渲染。RefColumns 任一为空
	// 则省略被引用列清单（引用父表主键）。
	Columns    []string `json:"columns,omitempty"`
	RefColumns []string `json:"refColumns,omitempty"`
}

// CreateTableSpec 新建表规格。
type CreateTableSpec struct {
	Table       string           `json:"table"`
	Columns     []ColumnSpec     `json:"columns"`
	ForeignKeys []ForeignKeySpec `json:"foreignKeys,omitempty"`
	// WithoutRowid 渲染 WITHOUT ROWID 后缀（V2.4 表重建内部使用，设计器未暴露）。
	WithoutRowid bool `json:"withoutRowid,omitempty"`
}

// RedesignColumn 表重设计的列变换（V2.4）。Name 指向现有列（空 = 新增列，
// 此时 NewName 必填）；Columns 列表即新列序，须覆盖全部现有列。
type RedesignColumn struct {
	Name                string `json:"name"`
	NewName             string `json:"newName"`
	Type                string `json:"type"`
	NotNull             bool   `json:"notNull"`
	PrimaryKey          bool   `json:"primaryKey"`
	Default             string `json:"default"`
	DefaultIsExpression bool   `json:"defaultIsExpression"`
	Drop                bool   `json:"drop"`
}

// TableRedesign 表重设计规格：改类型/约束/列序/主键/表名/新增外键。
// 通过"建新表 → 拷数据 → 删旧表 → 改名 → 重建索引/触发器"实现（V2.4）。
type TableRedesign struct {
	Table          string           `json:"table"`
	NewTable       string           `json:"newTable"` // 新表名；空 = 保持
	Columns        []RedesignColumn `json:"columns"`
	AddForeignKeys []ForeignKeySpec `json:"addForeignKeys,omitempty"`
}

// RedesignPreview 表重建预览：依序执行的语句、提示与阻断原因。
type RedesignPreview struct {
	SQL      []string `json:"sql"`
	Warnings []string `json:"warnings,omitempty"`
	Blocked  string   `json:"blocked,omitempty"`
}

// CreateIndexSpec 新建索引规格（V2.3）。Name 为空时由服务端自动命名。
type CreateIndexSpec struct {
	Table   string   `json:"table"`
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

// CreateIndexPreview 新建索引预览：解析后的名称（含自动命名）、SQL、阻断与提示。
type CreateIndexPreview struct {
	Name     string   `json:"name"`
	SQL      string   `json:"sql"`
	Warnings []string `json:"warnings,omitempty"`
	Blocked  string   `json:"blocked,omitempty"`
}
