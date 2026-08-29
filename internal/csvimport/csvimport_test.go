package csvimport

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/redjack/redsqlite/internal/tabular"
)

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 CSV 失败: %v", err)
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

func TestParse(t *testing.T) {
	path := writeCSV(t, "\uFEFFid,name,score\n1,张三,90.5\n2,\"李,四\",80\n3,王五,\n")

	p, err := Parse(path, true)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(p.Columns) != 3 || p.Columns[0] != "id" || p.Columns[1] != "name" {
		t.Errorf("表头解析错误: %v", p.Columns)
	}
	if len(p.Rows) != 3 || p.Rows[1][1] != "李,四" {
		t.Errorf("数据解析错误: %v", p.Rows)
	}
	if p.Delim != "," {
		t.Errorf("分隔符识别错误: %q", p.Delim)
	}
	if p.Truncated {
		t.Error("不应标记截断")
	}

	// 无表头模式
	p, err = Parse(path, false)
	if err != nil {
		t.Fatalf("Parse(hasHeader=false) 失败: %v", err)
	}
	if p.Columns[0] != "c1" || len(p.Rows) != 4 {
		t.Errorf("无表头解析错误: %v %v", p.Columns, p.Rows)
	}

	// 分号分隔
	p, err = Parse(writeCSV(t, "a;b;c\n1;2;3\n"), true)
	if err != nil || p.Delim != ";" || len(p.Rows) != 1 {
		t.Errorf("分号分隔解析错误: %+v err=%v", p, err)
	}

	// 制表符分隔
	p, err = Parse(writeCSV(t, "a\tb\n1\t2\n"), true)
	if err != nil || p.Delim != "\t" {
		t.Errorf("制表符解析错误: %+v err=%v", p, err)
	}

	// 空文件
	if _, err := Parse(writeCSV(t, ""), true); err == nil {
		t.Error("空文件应报错")
	}
}

func TestParseTruncated(t *testing.T) {
	var content string
	content += "n\n"
	for i := 0; i < previewRows+5; i++ {
		content += "1\n"
	}
	p, err := Parse(writeCSV(t, content), true)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(p.Rows) != previewRows || !p.Truncated {
		t.Errorf("预览截断标记错误: rows=%d truncated=%v", len(p.Rows), p.Truncated)
	}
}

func TestImport(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	path := writeCSV(t, "\uFEFFid,name,score\n1,张三,90.5\n2,李四,\n3,王五,70\n")

	res, err := Import(ctx, db, tabular.ImportRequest{
		Path: path, Table: "users", HasHeader: true,
		Mapping:     []string{"id", "name", "score"},
		EmptyAsNull: true,
	})
	if err != nil {
		t.Fatalf("Import 失败: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("导入行数错误: %d", res.Inserted)
	}

	// INTEGER 亲和列应把 "1" 存成整数；空单元格按 NULL
	var name string
	var score any
	var nullCount int
	if err := db.QueryRow(`SELECT name, score FROM users WHERE id = 1`).Scan(&name, &score); err != nil || name != "张三" {
		t.Errorf("导入数据错误: %v %v", name, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE score IS NULL`).Scan(&nullCount); err != nil || nullCount != 1 {
		t.Errorf("空值应写为 NULL: %d %v", nullCount, err)
	}

	// 跳过列 + 换序映射
	path2 := writeCSV(t, "score,name\n66.6,赵六\n")
	res, err = Import(ctx, db, tabular.ImportRequest{
		Path: path2, Table: "users", HasHeader: true,
		Mapping: []string{"", "name"}, // 只导入 name
	})
	if err != nil || res.Inserted != 1 {
		t.Fatalf("列映射导入失败: %+v %v", res, err)
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE name='赵六'`).Scan(&cnt); err != nil || cnt != 1 {
		t.Errorf("映射列导入数据错误: %d %v", cnt, err)
	}

	// 失败回滚：约束冲突时整体回滚
	before := countUsers(t, db)
	_, err = Import(ctx, db, tabular.ImportRequest{
		Path: writeCSV(t, "id,name\n1,重复主键\n99,会被回滚\n"), Table: "users", HasHeader: true,
		Mapping: []string{"id", "name"},
	})
	if err == nil {
		t.Error("主键冲突应报错")
	}
	if got := countUsers(t, db); got != before {
		t.Errorf("失败后应整体回滚: before=%d after=%d", before, got)
	}

	// 非法表名
	if _, err := Import(ctx, db, tabular.ImportRequest{Path: path, Table: `bad"name`, HasHeader: true, Mapping: []string{"id"}}); err == nil {
		t.Error("非法表名应报错")
	}
	// 空映射
	if _, err := Import(ctx, db, tabular.ImportRequest{Path: path, Table: "users", HasHeader: true, Mapping: []string{"", ""}}); err == nil {
		t.Error("空映射应报错")
	}
	// 目标列不存在
	if _, err := Import(ctx, db, tabular.ImportRequest{Path: path, Table: "users", HasHeader: true, Mapping: []string{"nope"}}); err == nil {
		t.Error("目标列不存在应报错")
	}
}

func countUsers(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	return n
}
