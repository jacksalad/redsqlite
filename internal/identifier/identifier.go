// Package identifier 提供 SQLite 标识符的安全引用，
// 防止表名/列名拼接引起的 SQL 注入（文档 13.1）。
package identifier

import (
	"errors"
	"strings"
	"unicode"
)

// ErrEmpty 标识符为空。
var ErrEmpty = errors.New("标识符不能为空")

// ErrInvalidCharacter 标识符包含非法字符。
var ErrInvalidCharacter = errors.New("标识符包含非法字符")

// Quote 返回带双引号且转义了内部双引号的标识符。
// 例：users -> "users"；a"b -> "a""b"。
func Quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Validate 校验标识符仅包含安全字符（字母、数字、下划线、中文、空格、$、#、-、点）。
// 拒绝控制字符、引号、分号等可能破坏 SQL 结构的字符。
// 校验通过后再 Quote，双引号被直接拒绝而不是转义，进一步收紧面。
func Validate(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmpty
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		case r == '_' || r == '$' || r == '#' || r == '-' || r == '.' || r == ' ':
		default:
			return ErrInvalidCharacter
		}
	}
	if strings.ContainsAny(name, "`\"'();{}[]\t\r\n\x00") {
		return ErrInvalidCharacter
	}
	return nil
}

// ValidateAndQuote 校验并安全引用标识符。
func ValidateAndQuote(name string) (string, error) {
	if err := Validate(name); err != nil {
		return "", err
	}
	return Quote(name), nil
}

// RowidName 无主键表使用 rowid 定位行时，结果集中暴露的列名。
const RowidName = "__rowid__"
