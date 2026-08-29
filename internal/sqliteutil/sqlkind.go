// Package sqliteutil 提供 SQL 文本分析：去除注释、识别语句类型、
// 多语句检测与危险操作检测（文档 6.5 / 13.2）。
package sqliteutil

import (
	"errors"
	"strings"
)

// SQLKind 语句类型。
type SQLKind int

const (
	KindInvalid SQLKind = iota // 空语句或无法识别
	KindQuery                  // SELECT / WITH / VALUES / PRAGMA / EXPLAIN
	KindExec                   // INSERT / UPDATE / DELETE / DDL / 其他写入
)

// ErrMultipleStatements V1 单语句执行入口仍拒绝多语句；
// 多语句由 ExecuteBatch 走专门的事务路径（V1.2）。
var ErrMultipleStatements = errors.New("一次只能执行一条 SQL 语句")

// ErrEmptySQL 空 SQL。
var ErrEmptySQL = errors.New("SQL 不能为空")

// stripComments 去除 -- 行注释与 /* */ 块注释，保留字符串字面量与
// 引号标识符内部的内容。返回剩余文本（注释替换为空格）。
func stripComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			// 原样保留字符串/标识符，处理转义（'' 与 ""）
			quote := c
			b.WriteByte(c)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						b.WriteByte(sql[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			}
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// Normalize 返回去除注释、首尾空白与结尾分号的 SQL。
func Normalize(sql string) string {
	s := stripComments(sql)
	s = strings.TrimSpace(s)
	for strings.HasSuffix(s, ";") {
		s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	}
	return s
}

// FirstKeyword 返回规范化后语句的首个关键字（大写）。
func FirstKeyword(sql string) string {
	s := Normalize(sql)
	if s == "" {
		return ""
	}
	// 去掉开头的括号（例如 ((SELECT 1))）
	s = strings.TrimLeft(s, "( \t\r\n")
	var b strings.Builder
	for _, r := range s {
		if unicodeIsIdent(r) {
			b.WriteRune(unicodeToUpper(r))
		} else {
			break
		}
	}
	return b.String()
}

// Classify 根据首关键字判断语句类型（文档 6.5：不能只看前缀，
// 需先去注释与空白；WITH 前缀按查询处理）。
func Classify(sql string) SQLKind {
	kw := FirstKeyword(sql)
	switch kw {
	case "SELECT", "WITH", "VALUES", "PRAGMA", "EXPLAIN":
		return KindQuery
	case "":
		return KindInvalid
	default:
		return KindExec
	}
}

// CountStatements 统计非空语句数量，分号必须位于字符串/注释之外。
func CountStatements(sql string) int {
	s := stripComments(sql)
	count := 0
	hasContent := false
	inStmt := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStmt {
			if c == ';' {
				continue // 连续分号
			}
			if !isSpace(c) {
				inStmt = true
				hasContent = true
			}
			continue
		}
		// 语句内：字符串与引号标识符内部的分号不算结束
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			for i < len(s) {
				if s[i] == quote {
					if i+1 < len(s) && s[i+1] == quote {
						i += 2
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if c == ';' {
			count++
			inStmt = false
		}
	}
	if inStmt && hasContent {
		count++
	}
	return count
}

// ValidateSingle 校验 SQL 非空且为单条语句。
func ValidateSingle(sql string) (string, error) {
	n := Normalize(sql)
	if n == "" {
		return "", ErrEmptySQL
	}
	if CountStatements(sql) > 1 {
		return "", ErrMultipleStatements
	}
	return n, nil
}

// SplitStatements 按分号拆分为多条语句（V1.2 多语句执行）。
// 分号必须位于字符串与注释之外；注释会被去除，返回的语句均已
// 去除首尾空白与结尾分号，空语句不会出现。空输入返回空切片。
func SplitStatements(sql string) []string {
	s := stripComments(sql)
	var out []string
	start := -1
	inStmt := false
	flush := func(end int) {
		if inStmt {
			stmt := strings.TrimSpace(s[start:end])
			stmt = strings.TrimSuffix(stmt, ";")
			stmt = strings.TrimSpace(stmt)
			if stmt != "" {
				out = append(out, stmt)
			}
			inStmt = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStmt {
			if c == ';' || isSpace(c) {
				continue
			}
			inStmt = true
			start = i
		}
		// 语句内：字符串与引号标识符内部的分号不算结束
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			for i < len(s) {
				if s[i] == quote {
					if i+1 < len(s) && s[i+1] == quote {
						i += 2
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if c == ';' {
			flush(i + 1)
		}
	}
	flush(len(s))
	return out
}

// IsTransactionControl 判断语句是否为显式事务控制
// （BEGIN/COMMIT/ROLLBACK/SAVEPOINT/RELEASE/END）。
// 多语句批量执行由服务层统一包事务，混入这些语句会破坏回滚语义。
func IsTransactionControl(sql string) bool {
	switch FirstKeyword(sql) {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "END":
		return true
	}
	return false
}

// DangerousReport 简单关键字检测的结果（文档 13.2：
// 仅作提示用途，不是完整 SQL 解析器）。
type DangerousReport struct {
	Dangerous bool
	Reason    string
}

// AnalyzeDangerous 检测 DROP / TRUNCATE(不支持) / VACUUM / ALTER / ATTACH /
// DELETE 以及无 WHERE 的 UPDATE / INSERT OR REPLACE。
func AnalyzeDangerous(sql string) DangerousReport {
	s := Normalize(sql)
	if s == "" {
		return DangerousReport{}
	}
	kw := FirstKeyword(s)
	switch kw {
	case "DROP":
		return DangerousReport{true, "DROP 语句将删除数据库对象，且无法自动恢复"}
	case "VACUUM":
		return DangerousReport{true, "VACUUM 将重建数据库文件，大库可能耗时较长"}
	case "ALTER":
		return DangerousReport{true, "ALTER 语句将修改表结构"}
	case "ATTACH", "DETACH":
		return DangerousReport{true, "ATTACH/DETACH 将改变数据库连接布局"}
	case "DELETE":
		if !hasTopLevelWhere(s) {
			return DangerousReport{true, "DELETE 没有 WHERE 条件，将删除整表数据"}
		}
		return DangerousReport{true, "DELETE 将删除数据行"}
	case "UPDATE":
		if !hasTopLevelWhere(s) {
			return DangerousReport{true, "UPDATE 没有 WHERE 条件，将更新整表数据"}
		}
	case "INSERT":
		if strings.HasPrefix(s, "INSERT OR REPLACE") || strings.HasPrefix(s, "REPLACE") {
			return DangerousReport{true, "REPLACE 语句可能覆盖已有数据行"}
		}
	}
	return DangerousReport{}
}

// hasTopLevelWhere 检测语句顶层是否出现 WHERE 关键字：跳过字符串字面量、
// 引号标识符与括号（子查询）内部。UPDATE/DELETE 只有顶层 WHERE 才限定
// 作用行——把子查询或字符串里的 WHERE 误算进来会漏掉"整表操作"提示。
func hasTopLevelWhere(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			quote := c
			i++
			for i < len(s) {
				if s[i] == quote {
					if i+1 < len(s) && s[i+1] == quote {
						i += 2
						continue
					}
					break
				}
				i++
			}
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case (c == 'w' || c == 'W') && depth == 0 && matchKeywordAt(s, i, "where"):
			return true
		}
	}
	return false
}

// matchKeywordAt 判断 s[i:] 是否以关键字 kw 开头（忽略大小写），
// 且前后都不是标识符字符（避免命中 whereX / xwhere 这类标识符）。
func matchKeywordAt(s string, i int, kw string) bool {
	if i > 0 && isIdentByte(s[i-1]) {
		return false
	}
	end := i + len(kw)
	if end > len(s) {
		return false
	}
	for j := 0; j < len(kw); j++ {
		if s[i+j]|0x20 != kw[j] {
			return false
		}
	}
	return end >= len(s) || !isIdentByte(s[end])
}

// isIdentByte 判断是否为标识符字节（UTF-8 多字节尾部保守视为标识符字符）。
func isIdentByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') || c > 127
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v'
}

func unicodeIsIdent(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') || r > 127
}

func unicodeToUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}
