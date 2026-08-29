package domain

// Settings 应用设置。
type Settings struct {
	PageSize       int    `json:"pageSize"`       // 表数据默认每页行数
	QueryTimeoutMs int    `json:"queryTimeoutMs"` // SQL 执行超时（毫秒）
	EnableWAL      bool   `json:"enableWAL"`      // 打开数据库时启用 WAL 模式
	MaxQueryRows   int    `json:"maxQueryRows"`   // 单次查询最大返回行数
	Theme          string `json:"theme"`          // 界面主题：system / light / dark
}

// 主题合法值。
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// ValidTheme 校验主题值，非法值回退 system。
func ValidTheme(theme string) string {
	switch theme {
	case ThemeSystem, ThemeLight, ThemeDark:
		return theme
	default:
		return ThemeSystem
	}
}

// DefaultSettings 默认设置，遵循文档 13.3 的安全限制。
func DefaultSettings() Settings {
	return Settings{
		PageSize:       100,
		QueryTimeoutMs: 30000,
		EnableWAL:      false,
		MaxQueryRows:   10000,
		Theme:          ThemeSystem,
	}
}

// RecentEntry 最近打开的数据库记录。
type RecentEntry struct {
	Path     string `json:"path"`
	FileName string `json:"fileName"`
	OpenedAt string `json:"openedAt"`
}
