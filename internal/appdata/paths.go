package appdata

import (
	"path/filepath"
	"strings"
	"time"
)

func nowISO() string {
	return time.Now().Format(time.RFC3339)
}

// samePath 尽量按规范化路径比较（大小写不敏感以兼容 Windows）。
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	na, err1 := filepath.Abs(a)
	nb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return strings.EqualFold(a, b)
	}
	return strings.EqualFold(filepath.Clean(na), filepath.Clean(nb))
}
