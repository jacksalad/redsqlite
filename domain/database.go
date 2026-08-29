package domain

// DatabaseInfo 描述一个已打开的 SQLite 数据库连接。
type DatabaseInfo struct {
	// ConnID 连接标识（V2.1 多连接）；同一路径复用连接时 Reused 为 true。
	ConnID       string `json:"connId"`
	Reused       bool   `json:"reused"`
	Path         string `json:"path"`
	FileName     string `json:"fileName"`
	Size         int64  `json:"size"`
	SQLiteVer    string `json:"sqliteVersion"`
	ReadOnly     bool   `json:"readOnly"`
	JournalMode  string `json:"journalMode"`
	EncodingMode string `json:"encoding"`
	PageSize     int64  `json:"pageSize"`
}

// BackupResult VACUUM INTO 备份结果（V2.5）。
type BackupResult struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ElapsedMs int64  `json:"elapsedMs"`
}
