package exporter

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	_ "modernc.org/sqlite"

	"github.com/redjack/redsqlite/internal/cell"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, score REAL, data BLOB);
	INSERT INTO users VALUES (1, '张三', 90.5, x'DEADBEEF');
	INSERT INTO users VALUES (2, 'a,b"c
d', NULL, NULL);
	INSERT INTO users VALUES (3, NULL, 1.0, x'00');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	return db
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取导出文件失败: %v", err)
	}
	return data
}

func TestExportCSV(t *testing.T) {
	db := newTestDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	res, err := Export(context.Background(), db, "SELECT * FROM users ORDER BY id", FormatCSV, path)
	if err != nil {
		t.Fatalf("导出 CSV 失败: %v", err)
	}
	if res.Rows != 3 {
		t.Fatalf("导出行数 = %d, want 3", res.Rows)
	}

	data := readAll(t, path)
	// BOM
	if data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Error("CSV 缺少 UTF-8 BOM")
	}
	text := string(data[3:])
	wantHeader := "id,name,score,data"
	if !contains(text, wantHeader) {
		t.Errorf("CSV 表头缺失: %q", text)
	}
	// NULL -> 空字段；BLOB -> base64；引号/换行正确处理
	if !contains(text, "3q2+7w==") { // DEADBEEF base64
		t.Errorf("CSV 未包含 BLOB base64: %q", text)
	}
	// NULL 输出空：第 3 行 name 为空
	if !contains(text, ",90.5,") {
		t.Errorf("CSV 未正确输出 NULL 为空: %q", text)
	}
}

func TestExportJSON(t *testing.T) {
	db := newTestDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	res, err := Export(context.Background(), db, "SELECT id, name, data FROM users ORDER BY id", FormatJSON, path)
	if err != nil {
		t.Fatalf("导出 JSON 失败: %v", err)
	}
	if res.Rows != 3 {
		t.Fatalf("导出行数 = %d, want 3", res.Rows)
	}

	var rows []map[string]any
	if err := json.Unmarshal(readAll(t, path), &rows); err != nil {
		t.Fatalf("导出的 JSON 无法解析: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("JSON 行数 = %d, want 3", len(rows))
	}
	if rows[0]["name"] != "张三" {
		t.Errorf("JSON 中文值错误: %v", rows[0]["name"])
	}
	if rows[1]["data"] != nil {
		t.Errorf("JSON NULL 应为 null: %v", rows[1]["data"])
	}
	if rows[2]["data"] != "AA==" {
		t.Errorf("JSON BLOB 应为 base64: %v", rows[2]["data"])
	}
}

func TestExportJSONFullFidelity(t *testing.T) {
	db := newTestDB(t)
	// 超 UI 截断阈值的大文本 + 超 JS 安全整数范围的 int64：
	// 导出文件是交付物，必须原样写出，不得复用 cell.Encode 的截断/标记协议
	long := strings.Repeat("x", cell.MaxTextDisplayBytes+100)
	if _, err := db.Exec("CREATE TABLE big (id INTEGER, note TEXT)"); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec("INSERT INTO big VALUES (?, ?)", 9007199254740993, long); err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}

	path := filepath.Join(t.TempDir(), "big.json")
	if _, err := Export(context.Background(), db, "SELECT * FROM big", FormatJSON, path); err != nil {
		t.Fatalf("导出 JSON 失败: %v", err)
	}

	raw := string(readAll(t, path))
	if contains(raw, "__kind") {
		t.Error("JSON 导出泄漏了 UI 内部标记（__kind）")
	}
	if !contains(raw, "9007199254740993") {
		t.Error("JSON 导出丢失大整数精度")
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("导出的 JSON 无法解析: %v", err)
	}
	if note, _ := rows[0]["note"].(string); note != long {
		t.Errorf("大文本被截断: 长度 = %d, want %d", len(note), len(long))
	}
}

func TestExportUnknownFormat(t *testing.T) {
	db := newTestDB(t)
	_, err := Export(context.Background(), db, "SELECT 1", "xml", filepath.Join(t.TempDir(), "x"))
	if err == nil {
		t.Fatal("未知格式应报错")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestExportXLSX(t *testing.T) {
	db := newTestDB(t)
	path := filepath.Join(t.TempDir(), "out.xlsx")

	res, err := Export(context.Background(), db, "SELECT * FROM users ORDER BY id", FormatXLSX, path)
	if err != nil {
		t.Fatalf("导出 XLSX 失败: %v", err)
	}
	if res.Rows != 3 {
		t.Fatalf("导出行数 = %d, want 3", res.Rows)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("回读 XLSX 失败: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	all, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("读取工作表失败: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("总行数 = %d, want 4（含表头）", len(all))
	}
	wantHeader := []string{"id", "name", "score", "data"}
	for i, h := range wantHeader {
		if all[0][i] != h {
			t.Errorf("表头[%d] = %q, want %q", i, all[0][i], h)
		}
	}
	// 行 1：数值 / 文本 / NULL（空）/ BLOB（Base64）
	wantRow := []string{"1", "张三", "90.5", "3q2+7w=="}
	for i, v := range wantRow {
		if all[1][i] != v {
			t.Errorf("数据[1][%d] = %q, want %q", i, all[1][i], v)
		}
	}
	if all[3][1] != "" {
		t.Errorf("NULL 应为空单元格: %q", all[3][1])
	}

	// 类型保留：OOXML 数值单元格没有 t 属性（GetCellType 返回 Unset），
	// 文本走 inlineStr（StreamWriter 写法）
	if ct, _ := f.GetCellType(sheet, "A2"); ct != excelize.CellTypeUnset {
		t.Errorf("A2 应为数字单元格: %v", ct)
	}
	if ct, _ := f.GetCellType(sheet, "C2"); ct != excelize.CellTypeUnset {
		t.Errorf("C2 应为数字单元格: %v", ct)
	}
	if ct, _ := f.GetCellType(sheet, "B2"); ct != excelize.CellTypeInlineString {
		t.Errorf("B2 应为字符串单元格: %v", ct)
	}
}
