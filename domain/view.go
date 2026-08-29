package domain

// ViewSpec 视图定义（V3.0 视图设计器）。Body 为 SELECT 语句文本
// （不含 CREATE VIEW 包装，由后端拼接与校验）。
type ViewSpec struct {
	Name string `json:"name"`
	Body string `json:"body"`
	// OldName 修改视图时的原视图名（编辑模式回填，新建时为空）。
	// 与 Name 不同表示重命名（DROP 旧名 + CREATE 新名，单事务），
	// 引用旧名的视图/触发器会悬空（运行时才报错），预览时给出警告。
	OldName string `json:"oldName,omitempty"`
}

// ViewOpPreview 视图操作预览：待执行 SQL（DROP+CREATE 组合时为多条）、
// 阻断原因（非空即禁止）与提示。
type ViewOpPreview struct {
	SQL      []string `json:"sql"`
	Warnings []string `json:"warnings,omitempty"`
	Blocked  string   `json:"blocked,omitempty"`
}
