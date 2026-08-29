package domain

// TableInfo 数据库对象的基本信息。
type TableInfo struct {
	Name string `json:"name"`
}

// ViewInfo 视图信息。
type ViewInfo struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

// IndexInfo 索引信息。
type IndexInfo struct {
	Name    string   `json:"name"`
	Table   string   `json:"table"`
	SQL     string   `json:"sql"`
	Unique  bool     `json:"unique"`
	Origin  string   `json:"origin"`
	Columns []string `json:"columns,omitempty"`
}

// ColumnInfo 表字段信息，对应 PRAGMA table_info。
type ColumnInfo struct {
	CID          int    `json:"cid"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	NotNull      bool   `json:"notNull"`
	DefaultValue any    `json:"defaultValue"`
	PrimaryKey   bool   `json:"primaryKey"`
}

// TriggerInfo 触发器信息，对应 sqlite_master type='trigger'。
type TriggerInfo struct {
	Name  string `json:"name"`
	Table string `json:"table"`
	SQL   string `json:"sql"`
}

// ForeignKeyInfo 表的外键定义（V2.3，对应 PRAGMA foreign_key_list；
// 多列外键共享同一 ID，按 ID 分组展示）。
type ForeignKeyInfo struct {
	ID        int    `json:"id"`
	Seq       int    `json:"seq"`
	Column    string `json:"column"`
	RefTable  string `json:"refTable"`
	RefColumn string `json:"refColumn"` // 空 = 引用父表主键
	OnDelete  string `json:"onDelete"`
	OnUpdate  string `json:"onUpdate"`
}

// SchemaInfo 数据库结构：表、视图、索引、触发器。
type SchemaInfo struct {
	Tables   []TableInfo   `json:"tables"`
	Views    []ViewInfo    `json:"views"`
	Indexes  []IndexInfo   `json:"indexes"`
	Triggers []TriggerInfo `json:"triggers"`
}

// TableStructure 单个表的完整结构描述。
type TableStructure struct {
	Name       string       `json:"name"`
	Columns    []ColumnInfo `json:"columns"`
	DDL        string       `json:"ddl"`
	Indexes    []IndexInfo  `json:"indexes"`
	RowCount   int64        `json:"rowCount"`
	IsView     bool         `json:"isView"`
	Editable   bool         `json:"editable"`
	KeyColumns []string     `json:"keyColumns"`
}
