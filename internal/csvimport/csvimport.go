// Package csvimport 把 CSV 文件导入 SQLite 表（V1.2）。
// 规则：UTF-8（自动跳过 BOM）；分隔符自动识别（逗号/分号/制表符）；
// V2.5 起通用类型与事务引擎位于 internal/tabular，本包只负责 CSV 解析。
package csvimport

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"github.com/redjack/redsqlite/internal/tabular"
)

// previewRows 预览行数上限（与 tabular.PreviewRows 一致）。
const previewRows = tabular.PreviewRows

// Parse 打开 CSV 文件并返回预览。hasHeader 为 true 时首行作为列名。
func Parse(path string, hasHeader bool) (tabular.Preview, error) {
	f, err := os.Open(path)
	if err != nil {
		return tabular.Preview{}, fmt.Errorf("无法读取 CSV 文件: %w", err)
	}
	defer f.Close()

	reader, delim, err := newReader(f)
	if err != nil {
		return tabular.Preview{}, err
	}

	preview := tabular.Preview{Delim: delim}
	if hasHeader {
		header, err := reader.Read()
		if err == io.EOF {
			return tabular.Preview{}, fmt.Errorf("CSV 文件为空")
		}
		if err != nil {
			return tabular.Preview{}, fmt.Errorf("读取表头失败: %w", err)
		}
		preview.Columns = tabular.DedupeColumns(header)
	} else {
		// 先读一行判断列数，再放回预览行
		first, err := reader.Read()
		if err == io.EOF {
			return tabular.Preview{}, fmt.Errorf("CSV 文件为空")
		}
		if err != nil {
			return tabular.Preview{}, fmt.Errorf("读取数据失败: %w", err)
		}
		for i := range first {
			preview.Columns = append(preview.Columns, fmt.Sprintf("c%d", i+1))
		}
		preview.Rows = append(preview.Rows, first)
	}

	for len(preview.Rows) < previewRows {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return tabular.Preview{}, fmt.Errorf("读取数据失败（第 %d 行附近）: %w", len(preview.Rows)+2, err)
		}
		preview.Rows = append(preview.Rows, rec)
	}
	// 文件是否还有剩余行
	if _, err := reader.Read(); err == nil {
		preview.Truncated = true
	}
	return preview, nil
}

// Import 在单个事务中把 CSV 数据插入目标表（V2.5：引擎在 tabular 包）。
func Import(ctx context.Context, db *sql.DB, req tabular.ImportRequest) (tabular.Result, error) {
	f, err := os.Open(req.Path)
	if err != nil {
		return tabular.Result{}, fmt.Errorf("无法读取 CSV 文件: %w", err)
	}
	defer f.Close()

	reader, _, err := newReader(f)
	if err != nil {
		return tabular.Result{}, err
	}
	if req.HasHeader {
		if _, err := reader.Read(); err != nil {
			return tabular.Result{}, fmt.Errorf("跳过表头失败: %w", err)
		}
	}

	return tabular.Import(ctx, db, req, func() ([]string, error) {
		rec, err := reader.Read()
		if err == io.EOF {
			return nil, io.EOF
		}
		return rec, err
	})
}

// newReader 构造 csv.Reader：跳过 UTF-8 BOM，按首行识别分隔符。
func newReader(f *os.File) (*csv.Reader, string, error) {
	buffered := bufio.NewReader(f)
	// 跳过 UTF-8 BOM（Excel 导出的 CSV 带有）
	if b, err := buffered.Peek(3); err == nil && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = buffered.Discard(3)
	}

	delim, err := detectDelimiter(buffered)
	if err != nil {
		return nil, "", err
	}

	reader := csv.NewReader(buffered)
	reader.Comma = delim
	reader.LazyQuotes = true
	return reader, string(delim), nil
}

// detectDelimiter 读取首行，按出现次数最多的候选分隔符判定。
func detectDelimiter(r *bufio.Reader) (rune, error) {
	line, err := r.Peek(4096)
	if err != nil && len(line) == 0 {
		return 0, fmt.Errorf("读取 CSV 首行失败: %w", err)
	}
	// 截断到换行
	if idx := indexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	candidates := []byte{',', ';', '\t'}
	best := byte(',')
	bestCount := -1
	for _, c := range candidates {
		n := countByte(line, c)
		if n > bestCount {
			best = c
			bestCount = n
		}
	}
	return rune(best), nil
}

func countByte(b []byte, target byte) int {
	n := 0
	for _, c := range b {
		if c == target {
			n++
		}
	}
	return n
}

func indexByte(b []byte, target byte) int {
	for i, c := range b {
		if c == target {
			return i
		}
	}
	return -1
}
