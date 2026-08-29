// Package xlsximport 把 Excel(.xlsx) 工作表导入 SQLite 表（V2.5）。
// 基于 excelize（纯 Go）。单元格按原始值读取：数值为数字字面量字符串
// （INTEGER/REAL 亲和列由 SQLite 自动转换）；日期为 Excel 序列数，
// 与 CSV 一致统一按 TEXT 绑定。全空行（Excel 常见的尾部样式行）跳过。
package xlsximport

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/redjack/redsqlite/internal/tabular"
)

// previewRows 预览行数上限。
const previewRows = tabular.PreviewRows

// SheetInfo 工作表信息。
type SheetInfo struct {
	Name string `json:"name"`
	Rows int    `json:"rows"` // 已用行数（按工作表维度声明，可能略有偏差）
}

// Sheets 列出工作簿的全部工作表。
func Sheets(path string) ([]SheetInfo, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取 Excel 文件: %w", err)
	}
	defer f.Close()

	names := f.GetSheetList()
	out := make([]SheetInfo, 0, len(names))
	for _, name := range names {
		info := SheetInfo{Name: name}
		if dim, err := f.GetSheetDimension(name); err == nil {
			_, rowCount, err := excelize.CellNameToCoordinates(dimEnd(dim))
			if err == nil {
				info.Rows = rowCount
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// Parse 预览指定工作表前 50 行。hasHeader 为 true 时首行作为列名；
// sheet 为空时使用第一个工作表。
func Parse(path string, sheet string, hasHeader bool) (tabular.Preview, error) {
	f, sheet, err := openSheet(path, sheet)
	if err != nil {
		return tabular.Preview{}, err
	}
	defer f.Close()

	rows, err := f.Rows(sheet)
	if err != nil {
		return tabular.Preview{}, fmt.Errorf("无法读取工作表 %q: %w", sheet, err)
	}
	defer rows.Close()

	width := dimensionCols(f, sheet)
	next := rowIterator(rows, &width)

	preview := tabular.Preview{}
	first, err := next()
	if err == io.EOF {
		return tabular.Preview{}, fmt.Errorf("工作表 %q 为空", sheet)
	}
	if err != nil {
		return tabular.Preview{}, err
	}
	if hasHeader {
		preview.Columns = tabular.DedupeColumns(first)
	} else {
		for i := range first {
			preview.Columns = append(preview.Columns, fmt.Sprintf("c%d", i+1))
		}
		preview.Rows = append(preview.Rows, first)
	}

	for len(preview.Rows) < previewRows {
		rec, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return tabular.Preview{}, err
		}
		preview.Rows = append(preview.Rows, rec)
	}
	if _, err := next(); err == nil {
		preview.Truncated = true
	}
	return preview, nil
}

// Import 在单个事务中把工作表数据插入目标表（引擎在 tabular 包）。
// 行不足处补空单元格；sheet 为空时使用第一个工作表。
func Import(ctx context.Context, db *sql.DB, req tabular.ImportRequest) (tabular.Result, error) {
	f, sheet, err := openSheet(req.Path, req.Sheet)
	if err != nil {
		return tabular.Result{}, err
	}
	defer f.Close()

	rows, err := f.Rows(sheet)
	if err != nil {
		return tabular.Result{}, fmt.Errorf("无法读取工作表 %q: %w", sheet, err)
	}
	defer rows.Close()

	width := len(req.Mapping) // 行不足处补到映射宽度，与预览列数一致
	next := rowIterator(rows, &width)

	if req.HasHeader {
		if _, err := next(); err != nil {
			return tabular.Result{}, fmt.Errorf("跳过表头失败: %w", err)
		}
	}
	return tabular.Import(ctx, db, req, next)
}

// openSheet 打开工作簿并解析目标工作表名（空则取第一个）。
func openSheet(path string, sheet string) (*excelize.File, string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("无法读取 Excel 文件: %w", err)
	}
	if sheet != "" {
		for _, name := range f.GetSheetList() {
			if name == sheet {
				return f, sheet, nil
			}
		}
		f.Close()
		return nil, "", fmt.Errorf("工作表 %q 不存在", sheet)
	}
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		f.Close()
		return nil, "", fmt.Errorf("工作簿没有工作表")
	}
	return f, sheets[0], nil
}

// rowIterator 返回逐行读取函数：跳过全空行，行不足 width 时补空单元格。
// width 为指针：解析中可能因行比维度声明更宽而增长。
func rowIterator(rows *excelize.Rows, width *int) func() ([]string, error) {
	return func() ([]string, error) {
		for {
			if !rows.Next() {
				if err := rows.Error(); err != nil {
					return nil, fmt.Errorf("读取工作表数据失败: %w", err)
				}
				return nil, io.EOF
			}
			cells, err := rows.Columns()
			if err != nil {
				return nil, fmt.Errorf("读取工作表数据失败: %w", err)
			}
			if len(cells) > *width {
				*width = len(cells)
			}
			padded := cells
			if len(padded) < *width {
				padded = append(append([]string{}, padded...), make([]string, *width-len(padded))...)
			}
			empty := true
			for _, c := range padded {
				if c != "" {
					empty = false
					break
				}
			}
			if empty {
				continue // Excel 尾部常见的空样式行
			}
			return padded, nil
		}
	}
}

// dimensionCols 按工作表维度声明返回已用列数（无法解析时为 0）。
func dimensionCols(f *excelize.File, sheet string) int {
	dim, err := f.GetSheetDimension(sheet)
	if err != nil {
		return 0
	}
	col, _, err := excelize.CellNameToCoordinates(dimEnd(dim))
	if err != nil {
		return 0
	}
	return col
}

// dimEnd 取维度范围的右下角单元格，如 "A1:C50" → "C50"；无冒号时原样返回。
func dimEnd(dim string) string {
	if i := strings.LastIndexByte(dim, ':'); i >= 0 {
		return dim[i+1:]
	}
	return dim
}
