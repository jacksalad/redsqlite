package services

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/repository"
)

// newRedesignStore 构造带索引/触发器/外键的测试库，返回服务、连接 ID 与底层 DB。
func newRedesignStore(t *testing.T) (*SchemaService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "redesign.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	store := repository.NewStore()
	info, err := store.Open(path, false, false)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	conn := info.ConnID
	t.Cleanup(func() { _ = store.Close() })

	db, _ := store.DB(conn)
	schema := `
	CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL);
	CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), amount REAL);
	CREATE TABLE logs (msg TEXT);
	CREATE INDEX idx_users_name ON users(name);
	CREATE TRIGGER trg_order_log AFTER INSERT ON orders BEGIN INSERT INTO logs(msg) VALUES ('new'); END;
	INSERT INTO users VALUES (1, '张三', 90), (2, '李四', 80);
	INSERT INTO orders VALUES (10, 1, 5.5), (11, 2, 3.0);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	return NewSchemaService(store), conn
}

func TestRedesignBasicTransform(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	rd := domain.TableRedesign{
		Table: "users",
		Columns: []domain.RedesignColumn{
			{Name: "name", Type: "TEXT", NotNull: true},
			{Name: "score", NewName: "points", Type: "REAL"},
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
		},
	}
	preview, err := svc.PreviewTableRedesign(conn, rd)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.Blocked != "" {
		t.Fatalf("不应阻断: %s", preview.Blocked)
	}
	if len(preview.SQL) < 5 {
		t.Fatalf("语句数不足（应含建表/拷贝/删表/改名/重建索引）: %d", len(preview.SQL))
	}

	if err := svc.ApplyTableRedesign(conn, rd); err != nil {
		t.Fatalf("重建失败: %v", err)
	}

	// 列顺序与改名生效
	cols, err := svc.GetTableColumns(conn, "users")
	if err != nil {
		t.Fatalf("查询列失败: %v", err)
	}
	if len(cols) != 3 || cols[0].Name != "name" || cols[1].Name != "points" || cols[2].Name != "id" {
		t.Fatalf("列顺序/改名不符: %+v", cols)
	}
	// 数据保全
	var name string
	var score float64
	if err := db.QueryRow(`SELECT name, points FROM users WHERE id = 1`).Scan(&name, &score); err != nil || name != "张三" || score != 90 {
		t.Fatalf("数据未保全: %v %q %v", err, name, score)
	}
	// 索引重建
	var idxCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_users_name'`).Scan(&idxCount); err != nil || idxCount != 1 {
		t.Fatalf("索引未重建: %v %d", err, idxCount)
	}
	// orders 的外键仍指向 users，触发器仍工作
	if _, err := db.Exec(`INSERT INTO orders VALUES (12, 1, 1.0)`); err != nil {
		t.Fatalf("重建后插入失败: %v", err)
	}
	var triggerMsgs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs WHERE msg='new'`).Scan(&triggerMsgs); err != nil || triggerMsgs == 0 {
		t.Fatalf("触发器未随重建恢复: %v %d", err, triggerMsgs)
	}
}

func TestRedesignAddColumnWithFK(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	rd := domain.TableRedesign{
		Table: "logs",
		Columns: []domain.RedesignColumn{
			{Name: "msg", Type: "TEXT"},
			{Name: "", NewName: "user_id", Type: "INTEGER", NotNull: true, Default: "1"},
		},
		AddForeignKeys: []domain.ForeignKeySpec{
			{Column: "user_id", RefTable: "users", RefColumn: "id"},
		},
	}
	if err := svc.ApplyTableRedesign(conn, rd); err != nil {
		t.Fatalf("重建失败: %v", err)
	}

	fks, err := svc.GetTableForeignKeys(conn, "logs")
	if err != nil || len(fks) != 1 || fks[0].Column != "user_id" {
		t.Fatalf("新增外键未生效: %v %+v", err, fks)
	}
	// 既有行按默认值填充
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs WHERE user_id = 1`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("新增列默认值未回填: %v %d", err, n)
	}
	// 外键强制生效（DSN 默认 foreign_keys=1）
	if _, err := db.Exec(`INSERT INTO logs (msg, user_id) VALUES ('x', 999)`); err == nil {
		t.Fatal("违反外键的插入应失败")
	}
}

func TestRedesignDropColumnAndRenameTable(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	rd := domain.TableRedesign{
		Table: "users",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "name", Type: "TEXT", NotNull: true},
			{Name: "score", Drop: true},
		},
	}
	// users 被 orders 外键引用：删除 score 不影响
	if err := svc.ApplyTableRedesign(conn, rd); err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("删列后数据不符: %v %d", err, n)
	}
	var cols []string
	rows, _ := db.Query(`SELECT name FROM pragma_table_info('users') ORDER BY cid`)
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols = append(cols, c)
	}
	rows.Close()
	if len(cols) != 2 {
		t.Fatalf("应只剩 2 列: %v", cols)
	}

	// 被外键引用的表重命名 → 阻断
	p, err := svc.PreviewTableRedesign(conn, domain.TableRedesign{
		Table:    "users",
		NewTable: "members",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "name", Type: "TEXT", NotNull: true},
		},
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !strings.Contains(p.Blocked, "外键") {
		t.Fatalf("被外键引用的表重命名应阻断: %q", p.Blocked)
	}

	// 无引用表的纯重命名成功（free 无索引/触发器/外键/视图引用）
	if _, err := db.Exec(`CREATE TABLE free (a TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := svc.ApplyTableRedesign(conn, domain.TableRedesign{
		Table:    "free",
		NewTable: "freed",
		Columns:  []domain.RedesignColumn{{Name: "a", Type: "TEXT"}},
	}); err != nil {
		t.Fatalf("纯重命名失败: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM freed`).Scan(&n); err != nil {
		t.Fatalf("重命名后表应可用: %v", err)
	}
}

func TestRedesignBlockedCases(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	// CHECK 约束
	if _, err := db.Exec(`CREATE TABLE checked (a INTEGER CHECK (a > 0))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	p, err := svc.PreviewTableRedesign(conn, domain.TableRedesign{
		Table:   "checked",
		Columns: []domain.RedesignColumn{{Name: "a", Type: "INTEGER"}},
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !strings.Contains(p.Blocked, "CHECK") {
		t.Fatalf("CHECK 表应阻断: %q", p.Blocked)
	}

	// 生成列
	if _, err := db.Exec(`CREATE TABLE gen (a INTEGER, b AS (a * 2))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	p, _ = svc.PreviewTableRedesign(conn, domain.TableRedesign{
		Table:   "gen",
		Columns: []domain.RedesignColumn{{Name: "a", Type: "INTEGER"}, {Name: "b", Type: ""}},
	})
	if !strings.Contains(p.Blocked, "生成列") {
		t.Fatalf("生成列表应阻断: %q", p.Blocked)
	}

	// 触发器引用删除列
	if _, err := db.Exec(`CREATE TRIGGER trg_amt AFTER UPDATE OF amount ON orders BEGIN SELECT 1; END;`); err != nil {
		t.Fatalf("建触发器失败: %v", err)
	}
	p, _ = svc.PreviewTableRedesign(conn, domain.TableRedesign{
		Table: "orders",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "user_id", Type: "INTEGER"},
			{Name: "amount", Drop: true},
		},
	})
	if !strings.Contains(p.Blocked, "触发器") {
		t.Fatalf("触发器引用删除列应阻断: %q", p.Blocked)
	}

	// 清单不全
	p, _ = svc.PreviewTableRedesign(conn, domain.TableRedesign{
		Table:   "users",
		Columns: []domain.RedesignColumn{{Name: "id", Type: "INTEGER", PrimaryKey: true}},
	})
	if !strings.Contains(p.Blocked, "覆盖全部") {
		t.Fatalf("清单不全应阻断: %q", p.Blocked)
	}
}

func TestRedesignRollbackOnNotNullViolation(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	if _, err := db.Exec(`INSERT INTO logs VALUES (NULL)`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	before := countVia(t, db, `SELECT COUNT(*) FROM logs`)

	err := svc.ApplyTableRedesign(conn, domain.TableRedesign{
		Table: "logs",
		Columns: []domain.RedesignColumn{
			{Name: "msg", Type: "TEXT", NotNull: true},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("NOT NULL 违例应失败并回滚: %v", err)
	}
	after := countVia(t, db, `SELECT COUNT(*) FROM logs`)
	if before != after {
		t.Fatalf("回滚后数据应保持: %d -> %d", before, after)
	}
	// 原表结构未变（仍可插入 NULL）
	if _, err := db.Exec(`INSERT INTO logs VALUES (NULL)`); err != nil {
		t.Fatalf("回滚后原表应保持原结构: %v", err)
	}
}

func TestRedesignRollbackOnForeignKeyCheck(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)

	before := countVia(t, db, `SELECT COUNT(*) FROM logs`)
	err := svc.ApplyTableRedesign(conn, domain.TableRedesign{
		Table: "logs",
		Columns: []domain.RedesignColumn{
			{Name: "msg", Type: "TEXT"},
			{Name: "", NewName: "user_id", Type: "INTEGER", NotNull: true, Default: "999"}, // users 无 id=999
		},
		AddForeignKeys: []domain.ForeignKeySpec{
			{Column: "user_id", RefTable: "users", RefColumn: "id"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "外键检查") {
		t.Fatalf("外键违例应失败并回滚: %v", err)
	}
	if countVia(t, db, `SELECT COUNT(*) FROM logs`) != before {
		t.Fatal("回滚后数据应保持")
	}
	// 外键开关恢复为开
	repo := repository.NewSchemaRepository(svc.store, conn)
	if !repo.ForeignKeysEnabled() {
		t.Fatal("外键开关应恢复为开启")
	}
}

func countVia(t *testing.T, db *sql.DB, sqlText string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(sqlText).Scan(&n); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	return n
}

// TestRedesignCompositeForeignKey V3.0：复合外键的表重建。
// 不涉改名/删列时按 (id,seq) 聚合重建表级 FOREIGN KEY；
// 删除/改名复合外键列仍阻断并给中文原因。
func TestRedesignCompositeForeignKey(t *testing.T) {
	svc, conn := newRedesignStore(t)
	db, _ := svc.store.DB(conn)
	schema := `
	CREATE TABLE pairs (a TEXT NOT NULL, b TEXT NOT NULL, PRIMARY KEY (a, b));
	CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		pa TEXT NOT NULL,
		pb TEXT NOT NULL,
		FOREIGN KEY (pa, pb) REFERENCES pairs (a, b) ON DELETE CASCADE
	);
	INSERT INTO pairs VALUES ('x', '1'), ('y', '2');
	INSERT INTO items VALUES (1, 'x', '1'), (2, 'y', '2');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	// ① 不动外键列：重建成功且复合外键按列序还原
	rd := domain.TableRedesign{
		Table: "items",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "pa", Type: "TEXT", NotNull: true},
			{Name: "pb", Type: "TEXT", NotNull: true},
		},
	}
	preview, err := svc.PreviewTableRedesign(conn, rd)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.Blocked != "" {
		t.Fatalf("复合外键重建不应阻断: %s", preview.Blocked)
	}
	joined := strings.Join(preview.SQL, "\n")
	if !strings.Contains(joined, `FOREIGN KEY ("pa", "pb") REFERENCES "pairs" ("a", "b") ON DELETE CASCADE`) {
		t.Fatalf("复合外键 DDL 不符:\n%s", joined)
	}
	if err := svc.ApplyTableRedesign(conn, rd); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("数据丢失: %d %v", n, err)
	}
	// 外键仍生效（foreign_keys=ON 下插入违规行应失败）
	if _, err := db.Exec(`INSERT INTO items VALUES (3, 'no', 'such')`); err == nil {
		t.Fatal("复合外键约束应仍然生效")
	}

	// ② 删除复合外键列 → 阻断
	dropRd := domain.TableRedesign{
		Table: "items",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "pa", Type: "TEXT", NotNull: true},
			{Name: "pb", Type: "TEXT", NotNull: true, Drop: true},
		},
	}
	p, err := svc.PreviewTableRedesign(conn, dropRd)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked == "" || !strings.Contains(p.Blocked, "复合外键") || !strings.Contains(p.Blocked, "pb") {
		t.Fatalf("删复合外键列应阻断: %q", p.Blocked)
	}

	// ③ 改名复合外键列 → 阻断
	renameRd := domain.TableRedesign{
		Table: "items",
		Columns: []domain.RedesignColumn{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "pa", NewName: "part_a", Type: "TEXT", NotNull: true},
			{Name: "pb", Type: "TEXT", NotNull: true},
		},
	}
	p, err = svc.PreviewTableRedesign(conn, renameRd)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked == "" || !strings.Contains(p.Blocked, "复合外键") || !strings.Contains(p.Blocked, "pa") {
		t.Fatalf("改复合外键列名应阻断: %q", p.Blocked)
	}
}
