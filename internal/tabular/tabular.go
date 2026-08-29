// Package tabular 定义 CSV / XLSX 共用的表格导入模型与事务导入引擎（V2.5）。
// 引擎与文件格式解耦：调用方通过 next 迭代器提供数据行（已跳过表头），
// 返回 io.EOF 表示结束。导入在单个事务中执行，任一行失败整体回滚。
package tabular

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/redjack/redsqlite/internal/identifier"
)

// Preview 表格预览信息（前若干行）。
type Preview struct {
	Columns   []string   `json:"columns"`   // 列名（hasHeader=false 时为 c1..cN）
	Rows      [][]string `json:"rows"`      // 预览数据行（不含表头），最多 50 行
	Truncated bool       `json:"truncated"` // 文件还有更多行
	Delim     string     `json:"delim"`     // CSV 识别出的分隔符；XLSX 为空
}

// ImportRequest 导入请求。Mapping 与来源列顺序对应，
// 元素为目标表列名或空字符串（空 = 跳过该来源列）。
type ImportRequest struct {
	Path        string   `json:"path"`
	Sheet       string   `json:"sheet"` // XLSX 工作表名；CSV 忽略
	Table       string   `json:"table"`
	HasHeader   bool     `json:"hasHeader"`
	Mapping     []string `json:"mapping"`
	EmptyAsNull bool     `json:"emptyAsNull"` // 空单元格按 NULL 写入；否则写入空字符串
}

// Result 导入结果。
type Result struct {
	Inserted  int64 `json:"inserted"`
	ElapsedMs int64 `json:"elapsedMs"`
}

// PreviewRows 预览行数上限。
const PreviewRows = 50

// Import 在单个事务中把 next 提供的数据行插入目标表。
// 值全部按 TEXT 绑定：INTEGER/REAL 亲和列会由 SQLite 自动转换数字字面量。
// next 返回 io.EOF 表示数据结束。
func Import(ctx context.Context, db *sql.DB, req ImportRequest, next func() ([]string, error)) (Result, error) {
	start := time.Now()
	if next == nil {
		return Result{}, fmt.Errorf("数据源为空")
	}
	if len(req.Mapping) == 0 {
		return Result{}, fmt.Errorf("没有可导入的列映射")
	}

	// 校验目标列，构造 INSERT 语句
	var colParts, placeholders []string
	var srcIndexes []int
	for i, target := range req.Mapping {
		if strings.TrimSpace(target) == "" {
			continue // 跳过该来源列
		}
		quoted, err := identifier.ValidateAndQuote(target)
		if err != nil {
			return Result{}, fmt.Errorf("第 %d 个映射的目标列名非法: %w", i+1, err)
		}
		colParts = append(colParts, quoted)
		placeholders = append(placeholders, "?")
		srcIndexes = append(srcIndexes, i)
	}
	if len(colParts) == 0 {
		return Result{}, fmt.Errorf("没有可导入的列映射")
	}
	quotedTable, err := identifier.ValidateAndQuote(req.Table)
	if err != nil {
		return Result{}, fmt.Errorf("表名非法: %w", err)
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotedTable, strings.Join(colParts, ", "), strings.Join(placeholders, ", "))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return Result{}, fmt.Errorf("准备插入语句失败（目标列不存在？）: %w", err)
	}
	defer stmt.Close()

	var count int64
	line := 1 // 已处理行号（用于报错定位，从 1 起：首条数据行）
	for {
		rec, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, fmt.Errorf("读取第 %d 行失败: %w", line, err)
		}
		args := make([]any, len(srcIndexes))
		for k, srcIdx := range srcIndexes {
			if srcIdx >= len(rec) {
				return Result{}, fmt.Errorf("第 %d 行缺少第 %d 列（各行列数不一致）", line, srcIdx+1)
			}
			value := rec[srcIdx]
			if req.EmptyAsNull && value == "" {
				args[k] = nil
			} else {
				args[k] = value
			}
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return Result{}, fmt.Errorf("插入第 %d 行失败: %w", line, err)
		}
		count++
		line++
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("提交导入事务失败: %w", err)
	}
	return Result{Inserted: count, ElapsedMs: time.Since(start).Milliseconds()}, nil
}

// DedupeColumns 保证列名唯一（空列名补 c1..cN，重名列加 _2/_3 后缀）。
func DedupeColumns(cols []string) []string {
	seen := map[string]int{}
	out := make([]string, len(cols))
	for i, c := range cols {
		name := strings.TrimSpace(c)
		if name == "" {
			name = fmt.Sprintf("c%d", i+1)
		}
		if n, ok := seen[name]; ok {
			seen[name] = n + 1
			// 生成的去重名可能撞上后续真实列名（如 a,a,a_2），
			// 每个最终输出名都必须登记 seen 并循环直至唯一
			cand := fmt.Sprintf("%s_%d", name, n+1)
			for k := n + 2; ; k++ {
				if _, exists := seen[cand]; !exists {
					break
				}
				cand = fmt.Sprintf("%s_%d", name, k)
			}
			seen[cand] = 1
			name = cand
		} else {
			seen[name] = 1
		}
		out[i] = name
	}
	return out
}
