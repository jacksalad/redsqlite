// Package exporter 把查询结果流式导出为 CSV / JSON / XLSX（文档 6.6）。
// 规则：UTF-8；CSV 带 BOM 以便 Excel 正确识别；NULL 在 CSV 中为空、
// 在 JSON 中为 null；BLOB 以 Base64 输出，避免内容损坏；
// XLSX 保留数值/布尔类型（excelize 流式写入，无样式）。
package exporter

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// Querier 导出所需的最小查询接口。
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Result 导出结果。
type Result struct {
	Path string `json:"path"`
	Rows int64  `json:"rows"`
}

// Format 支持的导出格式。
const (
	FormatCSV  = "csv"
	FormatJSON = "json"
	FormatXLSX = "xlsx"
)

// ErrUnknownFormat 未知导出格式。
var ErrUnknownFormat = errors.New("不支持的导出格式（仅支持 csv / json / xlsx）")

// xlsxMaxRows XLSX 单个工作表最大行数（含表头）。
const xlsxMaxRows = 1048576

// Export 把 sqlText 的查询结果流式写入 path。
func Export(ctx context.Context, q Querier, sqlText, format, path string) (Result, error) {
	switch format {
	case FormatCSV, FormatJSON, FormatXLSX:
	default:
		return Result{}, ErrUnknownFormat
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("创建导出目录失败: %w", err)
	}

	rows, err := q.QueryContext(ctx, sqlText)
	if err != nil {
		return Result{}, fmt.Errorf("导出查询失败: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return Result{}, fmt.Errorf("读取导出列失败: %w", err)
	}

	if format == FormatXLSX {
		return writeXLSX(rows, columns, path)
	}

	f, err := os.Create(path)
	if err != nil {
		return Result{}, fmt.Errorf("创建导出文件失败: %w", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	var count int64

	if format == FormatCSV {
		count, err = writeCSV(writer, rows, columns)
	} else {
		count, err = writeJSON(writer, rows, columns)
	}
	if err != nil {
		return Result{}, err
	}
	if err := writer.Flush(); err != nil {
		return Result{}, fmt.Errorf("写入导出文件失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Result{}, fmt.Errorf("落盘导出文件失败: %w", err)
	}

	return Result{Path: path, Rows: count}, nil
}

// writeXLSX 用 excelize 流式写入 XLSX：表头 + 逐行数据，保留类型。
func writeXLSX(rows *sql.Rows, columns []string, path string) (Result, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := f.GetSheetName(0)
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return Result{}, fmt.Errorf("初始化 XLSX 流式写入失败: %w", err)
	}

	header := make([]interface{}, len(columns))
	for i, c := range columns {
		header[i] = excelize.Cell{Value: c}
	}
	if err := sw.SetRow("A1", header); err != nil {
		return Result{}, fmt.Errorf("写入 XLSX 表头失败: %w", err)
	}

	var count int64
	rowNum := 2
	for rows.Next() {
		if rowNum > xlsxMaxRows {
			return Result{}, fmt.Errorf("结果超过 XLSX 单表行数上限（%d 行），请改用 CSV / JSON 导出", xlsxMaxRows-1)
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, fmt.Errorf("读取导出数据失败: %w", err)
		}
		cells := make([]interface{}, len(values))
		for i, v := range values {
			cells[i] = excelize.Cell{Value: xlsxValue(v)}
		}
		cellName, err := excelize.CoordinatesToCellName(1, rowNum)
		if err != nil {
			return Result{}, fmt.Errorf("计算单元格坐标失败: %w", err)
		}
		if err := sw.SetRow(cellName, cells); err != nil {
			return Result{}, fmt.Errorf("写入 XLSX 数据失败: %w", err)
		}
		count++
		rowNum++
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("读取导出数据失败: %w", err)
	}

	if err := sw.Flush(); err != nil {
		return Result{}, fmt.Errorf("写出 XLSX 失败: %w", err)
	}
	if err := f.SaveAs(path); err != nil {
		return Result{}, fmt.Errorf("保存 XLSX 文件失败: %w", err)
	}
	return Result{Path: path, Rows: count}, nil
}

// xlsxValue 把扫描值转为 excelize 支持的单元格值（BLOB 同 CSV/JSON 走 Base64；
// 文本原样写入完整内容，不做 UI 截断）。
func xlsxValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return base64.StdEncoding.EncodeToString(t)
	case string, bool, int64, int, int32, float64, float32:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

func writeCSV(w io.Writer, rows *sql.Rows, columns []string) (int64, error) {
	// UTF-8 BOM：避免 Excel 打开 UTF-8 CSV 乱码
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return 0, err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return 0, fmt.Errorf("写入 CSV 表头失败: %w", err)
	}

	var count int64
	for rows.Next() {
		encoded, err := scanRow(rows, len(columns))
		if err != nil {
			return count, err
		}
		if err := cw.Write(encoded); err != nil {
			return count, fmt.Errorf("写入 CSV 数据失败: %w", err)
		}
		count++
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return count, fmt.Errorf("写入 CSV 失败: %w", err)
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("读取导出数据失败: %w", err)
	}
	return count, nil
}

func writeJSON(w io.Writer, rows *sql.Rows, columns []string) (int64, error) {
	if _, err := w.Write([]byte("[\n")); err != nil {
		return 0, err
	}

	var count int64
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("读取导出数据失败: %w", err)
		}

		if count > 0 {
			if _, err := w.Write([]byte(",\n")); err != nil {
				return count, err
			}
		}
		if _, err := w.Write([]byte("  {")); err != nil {
			return count, err
		}
		for i, v := range values {
			if i > 0 {
				if _, err := w.Write([]byte(", ")); err != nil {
					return count, err
				}
			}
			keyJSON, _ := json.Marshal(columns[i])
			valJSON, err := json.Marshal(jsonValue(v))
			if err != nil {
				return count, fmt.Errorf("序列化导出数据失败: %w", err)
			}
			if _, err := w.Write(append(append(keyJSON, ':'), valJSON...)); err != nil {
				return count, err
			}
		}
		if _, err := w.Write([]byte("}")); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("读取导出数据失败: %w", err)
	}

	if _, err := w.Write([]byte("\n]\n")); err != nil {
		return count, err
	}
	return count, nil
}

func scanRow(rows *sql.Rows, n int) ([]string, error) {
	values := make([]any, n)
	ptrs := make([]any, n)
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, fmt.Errorf("读取导出数据失败: %w", err)
	}
	out := make([]string, n)
	for i, v := range values {
		out[i] = csvValue(v)
	}
	return out, nil
}

func csvValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "" // NULL 输出为空（文档 6.6）
	case []byte:
		return base64.StdEncoding.EncodeToString(t)
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// jsonValue 与 csvValue/xlsxValue 对称：导出文件是交付物，必须写原始完整值，
// 严禁复用 cell.Encode（那是 UI 展示协议，会截断大文本并泄漏 __kind 标记）。
// 数值类型交由 json.Marshal 全精度输出（int64 不会丢精度）。
func jsonValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return base64.StdEncoding.EncodeToString(t)
	case string, bool, int64, int, int32, float64, float32:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}
