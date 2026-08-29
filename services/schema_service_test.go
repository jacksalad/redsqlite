package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/repository"
)

// newTestSchemaService 在临时库中构造带外键/索引/视图/触发器的 SchemaService，
// 返回服务与连接 ID（V2.1 多连接）。
func newTestSchemaService(t *testing.T) (*SchemaService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.db")
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
	CREATE VIEW v_users AS SELECT id, name FROM users;
	CREATE TRIGGER trg_order_log AFTER INSERT ON orders BEGIN INSERT INTO logs(msg) VALUES ('new'); END;
	INSERT INTO users VALUES (1, '张三', 90), (2, '李四', 80);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	return NewSchemaService(store), conn
}

func TestPreviewRenameTableWarnings(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	p, err := svc.PreviewTableOp(conn, domain.TableOp{Kind: "renameTable", Table: "users", NewName: "members"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("不应阻断: %s", p.Blocked)
	}
	if !strings.Contains(p.SQL, `ALTER TABLE "users" RENAME TO "members"`) {
		t.Fatalf("SQL 不符: %s", p.SQL)
	}
	joined := strings.Join(p.Warnings, "\n")
	if !strings.Contains(joined, "外键") || !strings.Contains(joined, "orders.user_id") {
		t.Fatalf("缺少外键提示: %v", p.Warnings)
	}
	if !strings.Contains(joined, "v_users") {
		t.Fatalf("缺少视图引用提示: %v", p.Warnings)
	}

	// 新名冲突
	p, err = svc.PreviewTableOp(conn, domain.TableOp{Kind: "renameTable", Table: "users", NewName: "orders"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !strings.Contains(p.Blocked, "同名") {
		t.Fatalf("应阻断同名对象: %s", p.Blocked)
	}
}

func TestApplyAddColumn(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	// NOT NULL 无默认值 → 阻断
	_, err := svc.ApplyTableOp(conn, domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
		Name: "age", Type: "INTEGER", NotNull: true,
	}})
	if err == nil || !strings.Contains(err.Error(), "默认值") {
		t.Fatalf("应阻断 NOT NULL 无默认值: %v", err)
	}

	// 正常加列
	sqlText, err := svc.ApplyTableOp(conn, domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
		Name: "age", Type: "INTEGER", Default: "0",
	}})
	if err != nil {
		t.Fatalf("加列失败: %v", err)
	}
	if !strings.Contains(sqlText, `"age" INTEGER DEFAULT 0`) {
		t.Fatalf("SQL 不符: %s", sqlText)
	}
	cols, _ := svc.GetTableColumns(conn, "users")
	found := false
	for _, c := range cols {
		if c.Name == "age" {
			found = true
		}
	}
	if !found {
		t.Fatal("加列后应能查到新列")
	}

	// 重名阻断
	_, err = svc.ApplyTableOp(conn, domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
		Name: "age", Type: "INTEGER",
	}})
	if err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatalf("应阻断重名列: %v", err)
	}
}

func TestDropColumnBlocked(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	cases := []struct {
		column    string
		wantBlock string
	}{
		{"id", "主键"},
		{"name", "索引"},
		{"user_id", "外键"},
		{"missing", "不存在"},
	}
	for _, tc := range cases {
		p, err := svc.PreviewTableOp(conn, domain.TableOp{Kind: "dropColumn", Table: "users", Column: tc.column})
		if tc.column == "user_id" {
			// user_id 在 orders 表；对 users 表用 score 验证正常路径
			continue
		}
		if err != nil {
			t.Fatalf("预览 %s 失败: %v", tc.column, err)
		}
		if !strings.Contains(p.Blocked, tc.wantBlock) {
			t.Fatalf("列 %s 应阻断[%s]，实际: %q", tc.column, tc.wantBlock, p.Blocked)
		}
	}

	// orders.user_id 是本表外键列 → 阻断
	p, err := svc.PreviewTableOp(conn, domain.TableOp{Kind: "dropColumn", Table: "orders", Column: "user_id"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !strings.Contains(p.Blocked, "外键") {
		t.Fatalf("外键列应阻断: %q", p.Blocked)
	}

	// score 无引用 → 可删，且有视图引用 name 的场景单独验证
	p, err = svc.PreviewTableOp(conn, domain.TableOp{Kind: "dropColumn", Table: "users", Column: "score"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("score 不应阻断: %s", p.Blocked)
	}
	if _, err := svc.ApplyTableOp(conn, domain.TableOp{Kind: "dropColumn", Table: "users", Column: "score"}); err != nil {
		t.Fatalf("删除 score 失败: %v", err)
	}
}

func TestApplyRenameColumn(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	_, err := svc.ApplyTableOp(conn, domain.TableOp{Kind: "renameColumn", Table: "users", Column: "score", NewName: "points"})
	if err != nil {
		t.Fatalf("改列名失败: %v", err)
	}
	cols, _ := svc.GetTableColumns(conn, "users")
	found := false
	for _, c := range cols {
		if c.Name == "points" {
			found = true
		}
	}
	if !found {
		t.Fatal("改列名后应查到新列名")
	}

	// 重名阻断
	_, err = svc.ApplyTableOp(conn, domain.TableOp{Kind: "renameColumn", Table: "users", Column: "points", NewName: "name"})
	if err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatalf("应阻断重名: %v", err)
	}
}

func TestPreviewDropTableWarnings(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	p, err := svc.PreviewTableOp(conn, domain.TableOp{Kind: "dropTable", Table: "users"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("不应阻断: %s", p.Blocked)
	}
	joined := strings.Join(p.Warnings, "\n")
	if !strings.Contains(joined, "2 行") {
		t.Fatalf("应有行数提示: %v", p.Warnings)
	}
	if !strings.Contains(joined, "外键") {
		t.Fatalf("应有外键提示: %v", p.Warnings)
	}
}

func TestCreateTable(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	spec := domain.CreateTableSpec{
		Table: "products",
		Columns: []domain.ColumnSpec{
			{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
			{Name: "title", Type: "TEXT", NotNull: true},
			{Name: "price", Type: "REAL", Default: "0"},
		},
	}
	sqlText, err := svc.ApplyCreateTable(conn, spec)
	if err != nil {
		t.Fatalf("新建表失败: %v", err)
	}
	if !strings.Contains(sqlText, "PRIMARY KEY AUTOINCREMENT") {
		t.Fatalf("SQL 不符: %s", sqlText)
	}
	if _, err := svc.ApplyCreateTable(conn, spec); err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatalf("重复建表应阻断: %v", err)
	}

	// 无主键表 → 提示但不阻断
	p, err := svc.PreviewCreateTable(conn, domain.CreateTableSpec{
		Table:   "no_pk",
		Columns: []domain.ColumnSpec{{Name: "v", Type: "TEXT"}},
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" || len(p.Warnings) != 1 {
		t.Fatalf("无主键应仅提示: %+v", p)
	}
}

func TestTriggerAndIndexOperations(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	schema, err := svc.GetSchema(conn)
	if err != nil {
		t.Fatalf("GetSchema 失败: %v", err)
	}
	if len(schema.Triggers) == 0 || schema.Triggers[0].Name != "trg_order_log" {
		t.Fatalf("应返回触发器: %+v", schema.Triggers)
	}
	if schema.Triggers[0].Table != "orders" {
		t.Fatalf("触发器所属表不符: %+v", schema.Triggers[0])
	}

	if err := svc.DropTrigger(conn, "trg_order_log"); err != nil {
		t.Fatalf("删除触发器失败: %v", err)
	}
	if err := svc.DropTrigger(conn, "trg_order_log"); err == nil {
		t.Fatal("重复删除应报错")
	}

	// 手动索引可删；约束自动索引不可删
	if err := svc.DropIndex(conn, "idx_users_name"); err != nil {
		t.Fatalf("删除手动索引失败: %v", err)
	}
	db, _ := svc.store.DB(conn)
	if _, err := db.Exec(`CREATE TABLE tags (id INTEGER PRIMARY KEY, code TEXT UNIQUE)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	var autoIdx string
	if err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='tags' AND sql IS NULL`,
	).Scan(&autoIdx); err != nil {
		t.Fatalf("查询自动索引失败: %v", err)
	}
	if err := svc.DropIndex(conn, autoIdx); err == nil || !strings.Contains(err.Error(), "自动创建") {
		t.Fatalf("自动索引应不可单独删除: %v", err)
	}
}

func TestCreateIndex(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	// 自动命名 + 成功创建
	preview, err := svc.PreviewCreateIndex(conn, domain.CreateIndexSpec{
		Table: "users", Columns: []string{"score"},
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.Blocked != "" || preview.Name != "idx_users_score" {
		t.Fatalf("自动命名不符: %+v", preview)
	}
	name, err := svc.ApplyCreateIndex(conn, domain.CreateIndexSpec{Table: "users", Columns: []string{"score"}})
	if err != nil || name != "idx_users_score" {
		t.Fatalf("创建失败: %v %q", err, name)
	}
	schema, _ := svc.GetSchema(conn)
	found := false
	for _, idx := range schema.Indexes {
		if idx.Name == "idx_users_score" {
			found = true
		}
	}
	if !found {
		t.Fatal("索引应出现在结构中")
	}

	// 重复创建 → 自动后缀命名
	name2, err := svc.ApplyCreateIndex(conn, domain.CreateIndexSpec{Table: "users", Columns: []string{"score"}})
	if err != nil || name2 != "idx_users_score_2" {
		t.Fatalf("重名应自动加后缀: %v %q", err, name2)
	}

	// 显式重名 → 阻断
	p, err := svc.PreviewCreateIndex(conn, domain.CreateIndexSpec{Table: "users", Name: "idx_users_score", Columns: []string{"name"}})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !strings.Contains(p.Blocked, "同名") {
		t.Fatalf("显式重名应阻断: %q", p.Blocked)
	}

	// 列不存在 → 阻断
	p, _ = svc.PreviewCreateIndex(conn, domain.CreateIndexSpec{Table: "users", Columns: []string{"missing"}})
	if !strings.Contains(p.Blocked, "不存在") {
		t.Fatalf("缺列应阻断: %q", p.Blocked)
	}

	// 相同列已有索引 → 警告
	p, _ = svc.PreviewCreateIndex(conn, domain.CreateIndexSpec{Table: "users", Columns: []string{"name"}})
	if len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "idx_users_name") {
		t.Fatalf("相同列索引应警告: %+v", p.Warnings)
	}
}

func TestTableForeignKeysAndCompletion(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	fks, err := svc.GetTableForeignKeys(conn, "orders")
	if err != nil {
		t.Fatalf("查询外键失败: %v", err)
	}
	if len(fks) != 1 || fks[0].Column != "user_id" || fks[0].RefTable != "users" || fks[0].RefColumn != "id" {
		t.Fatalf("外键信息不符: %+v", fks)
	}
	if fks[0].OnDelete != "NO ACTION" || fks[0].OnUpdate != "NO ACTION" {
		t.Fatalf("默认动作应为 NO ACTION: %+v", fks[0])
	}

	completion, err := svc.GetCompletionSchema(conn)
	if err != nil {
		t.Fatalf("补全 schema 失败: %v", err)
	}
	if cols, ok := completion["users"]; !ok || len(cols) != 3 || cols[0] != "id" {
		t.Fatalf("补全 schema 不符: %+v", completion)
	}
	if _, ok := completion["v_users"]; !ok {
		t.Fatalf("视图应包含在补全 schema 中: %+v", completion)
	}
}
