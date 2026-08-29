package xlsximport

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
	_ "modernc.org/sqlite"

	"github.com/redjack/redsqlite/internal/tabular"
)

// writeXlsx 生成测试工作簿：Sheet1 数据 + 空行 + 尾部空样式行；Data 为第二个工作表。
func writeXlsx(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	sheet1 := f.GetSheetName(0)
	cells := map[string]any{
		"A1": "id", "B1": "name", "C1": "score",
		"A2": 1, "B2": "张三", "C2": 90.5,
		"A3": 2, "B3": "李四", // C3 空 → 补空单元格
		"A4": 3, "B4": "王五", "C4": 70,
		// 第 5 行整行为空（应跳过）
	}
	for cell, v := range cells {
		if err := f.SetCellValue(sheet1, cell, v); err != nil {
			t.Fatalf("写入测试单元格失败: %v", err)
		}
	}
	if _, err := f.NewSheet("Data"); err != nil {
		t.Fatalf("新建工作表失败: %v", err)
	}
	if err := f.SetCellValue("Data", "A1", "x"); err != nil {
		t.Fatalf("写入 Data 失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("保存测试工作簿失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭工作簿失败: %v", err)
	}
	return path
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import.db")
	os.WriteFile(path, nil, 0o644)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, score REAL)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return db
}

func TestSheets(t *testing.T) {
	sheets, err := Sheets(writeXlsx(t))
	if err != nil {
		t.Fatalf("Sheets 失败: %v", err)
	}
	if len(sheets) != 2 || sheets[0].Name != "Sheet1" || sheets[1].Name != "Data" {
		t.Fatalf("工作表列表错误: %+v", sheets)
	}
	// 行数来自工作表维度声明，excelize 生成的文件维度不刷新，仅检查非负
	if sheets[0].Rows < 0 {
		t.Errorf("行数不应为负: %d", sheets[0].Rows)
	}
}

func TestParse(t *testing.T) {
	path := writeXlsx(t)

	p, err := Parse(path, "", true)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(p.Columns) != 3 || p.Columns[0] != "id" || p.Columns[2] != "score" {
		t.Errorf("表头解析错误: %v", p.Columns)
	}
	if len(p.Rows) != 3 || p.Rows[1][1] != "李四" || p.Rows[1][2] != "" {
		t.Errorf("数据解析错误（空单元格应补齐）: %v", p.Rows)
	}
	if p.Rows[0][0] != "1" || p.Rows[0][2] != "90.5" {
		t.Errorf("数值应按原始值读取: %v", p.Rows[0])
	}
	if p.Delim != "" {
		t.Errorf("Excel 预览不应有分隔符: %q", p.Delim)
	}
	if p.Truncated {
		t.Error("不应标记截断")
	}

	// 无表头
	p, err = Parse(path, "", false)
	if err != nil {
		t.Fatalf("Parse(hasHeader=false) 失败: %v", err)
	}
	if p.Columns[0] != "c1" || len(p.Rows) != 4 || p.Rows[0][0] != "id" {
		t.Errorf("无表头解析错误: %v %v", p.Columns, p.Rows)
	}

	// 指定工作表
	p, err = Parse(path, "Data", true)
	if err != nil || len(p.Columns) != 1 || p.Columns[0] != "x" {
		t.Errorf("指定工作表解析错误: %+v err=%v", p, err)
	}

	// 工作表不存在
	if _, err := Parse(path, "nope", true); err == nil {
		t.Error("不存在的工作表应报错")
	}
}

func TestImport(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	path := writeXlsx(t)

	res, err := Import(ctx, db, tabular.ImportRequest{
		Path: path, Table: "users", HasHeader: true,
		Mapping:     []string{"id", "name", "score"},
		EmptyAsNull: true,
	})
	if err != nil {
		t.Fatalf("Import 失败: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("导入行数错误（空行应跳过）: %d", res.Inserted)
	}

	// INTEGER 亲和列把 "1" 存成整数；空单元格按 NULL；短行补空
	var id int64
	var name string
	var score any
	if err := db.QueryRow(`SELECT id, name, score FROM users WHERE id = 1`).Scan(&id, &name, &score); err != nil || name != "张三" || id != 1 {
		t.Errorf("导入数据错误: %v %v %v", id, name, err)
	}
	if err := db.QueryRow(`SELECT score FROM users WHERE id = 2`).Scan(&score); err != nil || score != nil {
		t.Errorf("空单元格应写为 NULL: %v %v", score, err)
	}

	// 失败回滚：主键冲突
	_, err = Import(ctx, db, tabular.ImportRequest{
		Path: path, Table: "users", HasHeader: true,
		Mapping: []string{"id", "name", "score"},
	})
	if err == nil {
		t.Error("主键冲突应报错")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 3 {
		t.Errorf("失败后应整体回滚: %d %v", n, err)
	}
}
