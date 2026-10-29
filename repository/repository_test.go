package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/cell"
	"github.com/redjack/redsqlite/internal/identifier"
)

// newTestStore 在临时目录创建带测试数据的 SQLite 文件并打开，
// 返回 Store 与连接 ID（V2.1 多连接）。
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	store := NewStore()
	info, err := store.Open(path, false, false)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	conn := info.ConnID
	t.Cleanup(func() { store.Close() })

	db, _ := store.DB(conn)
	schema := `
	CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL);
	CREATE TABLE logs (msg TEXT); -- 无主键
	CREATE VIEW v_users AS SELECT id, name FROM users;
	CREATE INDEX idx_users_name ON users(name);
	INSERT INTO users VALUES (1, '张三', 90.5), (2, '李四', 80), (3, '王五', 70);
	INSERT INTO logs VALUES ('a'), ('b'), ('c'), ('d'), ('e');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	return store, conn
}

func TestStoreOpenInvalid(t *testing.T) {
	store := NewStore()

	if _, err := store.Open(filepath.Join(t.TempDir(), "missing.db"), false, false); err == nil {
		t.Fatal("打开不存在的文件应报错")
	}

	// 非 SQLite 文件
	bad := filepath.Join(t.TempDir(), "bad.db")
	os.WriteFile(bad, []byte("this is not sqlite at all........."), 0o644)
	if _, err := store.Open(bad, false, false); err == nil {
		t.Fatal("打开非 SQLite 文件应报错")
	}

	// 目录
	if _, err := store.Open(t.TempDir(), false, false); err == nil {
		t.Fatal("打开目录应报错")
	}
}

func TestStoreInfo(t *testing.T) {
	store, conn := newTestStore(t)
	info, err := store.Info(conn)
	if err != nil {
		t.Fatalf("Info 失败: %v", err)
	}
	if info.FileName != "test.db" || info.ReadOnly {
		t.Errorf("Info 字段错误: %+v", info)
	}
	if info.SQLiteVer == "" || info.JournalMode == "" {
		t.Errorf("缺少 SQLite 元信息: %+v", info)
	}
}

func TestGetSchema(t *testing.T) {
	store, conn := newTestStore(t)
	schema := NewSchemaRepository(store, conn)

	info, err := schema.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema 失败: %v", err)
	}
	if len(info.Tables) != 2 || len(info.Views) != 1 || len(info.Indexes) != 1 {
		t.Errorf("结构数量错误: %+v", info)
	}
	if info.Tables[0].Name != "logs" || info.Tables[1].Name != "users" {
		t.Errorf("表顺序错误: %+v", info.Tables)
	}
	if info.Indexes[0].Name != "idx_users_name" || info.Indexes[0].Table != "users" {
		t.Errorf("索引信息错误: %+v", info.Indexes)
	}
}

func TestGetTableColumns(t *testing.T) {
	store, conn := newTestStore(t)
	schema := NewSchemaRepository(store, conn)

	cols, err := schema.GetTableColumns("users")
	if err != nil {
		t.Fatalf("GetTableColumns 失败: %v", err)
	}
	if len(cols) != 3 || cols[0].Name != "id" || !cols[0].PrimaryKey {
		t.Errorf("字段信息错误: %+v", cols)
	}
	if !cols[1].NotNull || cols[2].NotNull {
		t.Errorf("NOT NULL 解析错误: %+v", cols)
	}

	if _, err := schema.GetTableColumns(`bad"name`); err == nil {
		t.Error("非法表名应报错")
	}
}

func TestKeyColumns(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)

	keys, editable, err := data.KeyColumns("users")
	if err != nil || !editable {
		t.Fatalf("users 应可编辑: %v %v", editable, err)
	}
	if len(keys) != 1 || keys[0] != "id" {
		t.Errorf("users 主键列错误: %v", keys)
	}

	// 无主键表用 rowid
	keys, editable, err = data.KeyColumns("logs")
	if err != nil || !editable || keys[0] != identifier.RowidName {
		t.Fatalf("logs 应回退到 rowid: %v %v %v", keys, editable, err)
	}

	// 视图只读
	_, editable, err = data.KeyColumns("v_users")
	if err != nil || editable {
		t.Fatalf("视图应只读: %v %v", editable, err)
	}
}

func TestGetTableData(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)

	// 第 1 页，每页 2 行（logs 有 5 行；默认顺序走游标分页）
	result, err := data.GetTableData(context.Background(), domain.TableDataRequest{
		Table: "logs", Page: 1, PageSize: 2,
	})
	if err != nil {
		t.Fatalf("GetTableData 失败: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("分页错误: rows=%d", len(result.Rows))
	}
	if !result.CursorMode || !result.HasNext || result.HasPrev {
		t.Errorf("游标分页状态错误: %+v", result)
	}
	// 无主键表应注入 __rowid__
	if result.Columns[0] != identifier.RowidName {
		t.Errorf("缺少 rowid 列: %v", result.Columns)
	}
	if !result.Editable {
		t.Error("logs 表应可编辑（rowid 回退）")
	}

	// 游标翻页：第 2、3 页走完 5 行
	result2, err := data.GetTableData(context.Background(), domain.TableDataRequest{
		Table: "logs", PageSize: 2, Cursor: result.NextCursor,
	})
	if err != nil {
		t.Fatalf("游标第 2 页失败: %v", err)
	}
	if len(result2.Rows) != 2 || !result2.HasPrev {
		t.Errorf("游标第 2 页错误: rows=%d hasPrev=%v", len(result2.Rows), result2.HasPrev)
	}
	if got := result2.Rows[0][1]; got != "c" {
		t.Errorf("游标第 2 页首行错误: %v", got)
	}
	result3, err := data.GetTableData(context.Background(), domain.TableDataRequest{
		Table: "logs", PageSize: 2, Cursor: result2.NextCursor,
	})
	if err != nil {
		t.Fatalf("游标第 3 页失败: %v", err)
	}
	if len(result3.Rows) != 1 || result3.HasNext {
		t.Errorf("游标第 3 页错误: rows=%d hasNext=%v", len(result3.Rows), result3.HasNext)
	}

	// 行数统计独立异步获取（不再随分页返回）
	total, err := data.CountFiltered(context.Background(), "logs", "", nil)
	if err != nil || total != 5 {
		t.Errorf("CountFiltered 错误: total=%d err=%v", total, err)
	}
	total, err = data.CountFiltered(context.Background(), "logs", "a", nil)
	if err != nil || total != 1 {
		t.Errorf("CountFiltered 过滤错误: total=%d err=%v", total, err)
	}

	// 排序 + 过滤（V3.0：score 无 NULL，排序走复合游标分页）
	result, err = data.GetTableData(context.Background(), domain.TableDataRequest{
		Table: "users", Page: 1, PageSize: 10, OrderBy: "score", OrderDesc: true, Filter: "李",
	})
	if err != nil {
		t.Fatalf("过滤查询失败: %v", err)
	}
	if len(result.Rows) != 1 || !result.CursorMode {
		t.Errorf("过滤结果错误: %+v", result)
	}
	total, err = data.CountFiltered(context.Background(), "users", "李", nil)
	if err != nil || total != 1 {
		t.Errorf("users 过滤统计错误: total=%d err=%v", total, err)
	}

	// 非法排序列
	if _, err := data.GetTableData(context.Background(), domain.TableDataRequest{
		Table: "users", OrderBy: "id) OR 1=1",
	}); err == nil {
		t.Error("非法排序列应报错")
	}
}

func TestGetTableDataCursorModes(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)
	ctx := context.Background()

	// 单列主键表（users）：游标为主键列
	first, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "users", PageSize: 2})
	if err != nil {
		t.Fatalf("读取 users 失败: %v", err)
	}
	if !first.CursorMode || first.NextCursor != "2" {
		t.Errorf("主键游标错误: mode=%v next=%q", first.CursorMode, first.NextCursor)
	}
	second, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "users", PageSize: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("游标翻页失败: %v", err)
	}
	if len(second.Rows) != 1 || second.HasNext {
		t.Errorf("users 第 2 页错误: rows=%d hasNext=%v", len(second.Rows), second.HasNext)
	}

	// 游标 + 过滤：从过滤后的首页继续
	// logs 数据为 a,b,c,d,e，过滤 "b|c" 不可表达，改用逐字过滤验证
	filtered, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "logs", PageSize: 2, Filter: "b"})
	if err != nil {
		t.Fatalf("过滤游标失败: %v", err)
	}
	if !filtered.CursorMode || len(filtered.Rows) != 1 || filtered.HasNext {
		t.Errorf("过滤游标页错误: %+v", filtered)
	}

	// 视图：无游标，回退 OFFSET
	view, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "v_users", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("读取视图失败: %v", err)
	}
	if view.CursorMode || view.Page != 1 {
		t.Errorf("视图应回退 OFFSET: %+v", view)
	}

	// 按主键列排序 = 游标列本身（V3.0）：仍走游标分页
	sorted, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "users", PageSize: 2, OrderBy: "id",
	})
	if err != nil {
		t.Fatalf("排序读取失败: %v", err)
	}
	if !sorted.CursorMode || len(sorted.Rows) != 2 || !sorted.HasNext {
		t.Errorf("主键排序应游标分页: mode=%v rows=%d", sorted.CursorMode, len(sorted.Rows))
	}
}

// TestSortedCursorPagination V3.0：排序浏览的复合游标分页
// （排序列 + 游标列 row-value 比较），含 DESC 翻转与 NULL 回退。
func TestSortedCursorPagination(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)
	db, _ := store.DB(conn)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE scores (id INTEGER PRIMARY KEY, val INTEGER NOT NULL)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// val 有重复值，验证游标列 tiebreaker
	if _, err := db.Exec(`INSERT INTO scores VALUES (1, 90), (2, 70), (3, 90), (4, 70), (5, 80)`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// ASC 逐页翻：70,70 | 80,90 | 90
	page1, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "scores", PageSize: 2, OrderBy: "val"})
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !page1.CursorMode || len(page1.Rows) != 2 {
		t.Fatalf("排序游标页错误: %+v", page1)
	}
	if page1.Rows[0][1] != int64(70) || page1.Rows[1][1] != int64(70) {
		t.Fatalf("第 1 页排序错误: %v", page1.Rows)
	}
	if page1.NextCursor == "" {
		t.Fatal("缺少复合游标")
	}
	page2, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "scores", PageSize: 2, OrderBy: "val", Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("翻页失败: %v", err)
	}
	if len(page2.Rows) != 2 || page2.Rows[0][1] != int64(80) || page2.Rows[1][1] != int64(90) {
		t.Fatalf("第 2 页排序错误: %v", page2.Rows)
	}
	page3, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "scores", PageSize: 2, OrderBy: "val", Cursor: page2.NextCursor,
	})
	if err != nil {
		t.Fatalf("翻页失败: %v", err)
	}
	if len(page3.Rows) != 1 || page3.HasNext {
		t.Fatalf("第 3 页错误: %d 行 hasNext=%v", len(page3.Rows), page3.HasNext)
	}

	// DESC：90,90 | 80,70 | 70（比较方向翻转）
	d1, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "scores", PageSize: 2, OrderBy: "val", OrderDesc: true})
	if err != nil || len(d1.Rows) != 2 || d1.Rows[0][1] != int64(90) {
		t.Fatalf("DESC 第 1 页错误: %v %v", d1.Rows, err)
	}
	d2, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "scores", PageSize: 2, OrderBy: "val", OrderDesc: true, Cursor: d1.NextCursor,
	})
	if err != nil || len(d2.Rows) != 2 || d2.Rows[0][1] != int64(80) || d2.Rows[1][1] != int64(70) {
		t.Fatalf("DESC 第 2 页错误: %v %v", d2.Rows, err)
	}
	d3, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "scores", PageSize: 2, OrderBy: "val", OrderDesc: true, Cursor: d2.NextCursor,
	})
	if err != nil || len(d3.Rows) != 1 || d3.HasNext {
		t.Fatalf("DESC 第 3 页错误: %d %v", len(d3.Rows), err)
	}

	// 排序列含 NULL → 回退 OFFSET（row-value 比较对 NULL 不成立）
	if _, err := db.Exec(`ALTER TABLE scores ADD COLUMN note TEXT`); err != nil {
		t.Fatalf("加列失败: %v", err)
	}
	if _, err := db.Exec(`UPDATE scores SET note = 'x' WHERE id IN (1, 3)`); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	nullablePage, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "scores", PageSize: 2, OrderBy: "note"})
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if nullablePage.CursorMode || nullablePage.Page != 1 {
		t.Fatalf("含 NULL 排序列应回退 OFFSET: mode=%v page=%d", nullablePage.CursorMode, nullablePage.Page)
	}

	// 复合主键表：无游标列，排序也回退 OFFSET
	if _, err := db.Exec(`CREATE TABLE cp (a TEXT, b TEXT, v INT, PRIMARY KEY (a, b))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	cpPage, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "cp", PageSize: 2, OrderBy: "v"})
	if err != nil || cpPage.CursorMode {
		t.Fatalf("复合主键排序应回退 OFFSET: %v", err)
	}
}

func TestEditRows(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)
	ctx := context.Background()

	// 插入
	if _, err := data.InsertRow(ctx, "users", map[string]any{
		"name": "新用户", "score": float64(66),
	}); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// 更新（主键定位）
	n, err := data.UpdateCell(ctx, "users", map[string]any{"id": float64(2)}, "score", 88.5)
	if err != nil || n != 1 {
		t.Fatalf("更新失败: %v %d", err, n)
	}

	// 更新不存在的行
	if _, err := data.UpdateCell(ctx, "users", map[string]any{"id": float64(99)}, "score", 1); err == nil {
		t.Error("更新不存在的行应报错")
	}

	// 无主键表用 rowid 定位更新
	n, err = data.UpdateCell(ctx, "logs", map[string]any{identifier.RowidName: float64(1)}, "msg", "updated")
	if err != nil || n != 1 {
		t.Fatalf("rowid 更新失败: %v %d", err, n)
	}

	// 删除多行（事务）
	n, err = data.DeleteRows(ctx, "logs", []map[string]any{
		{identifier.RowidName: float64(2)},
		{identifier.RowidName: float64(3)},
	})
	if err != nil || n != 2 {
		t.Fatalf("删除失败: %v %d", err, n)
	}

	// 校验最终数据
	result, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "users", Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(result.Rows) != 4 {
		t.Errorf("users 应有 4 行: %d", len(result.Rows))
	}
	found := false
	for _, row := range result.Rows {
		if fmt.Sprintf("%v", row[1]) == "新用户" {
			found = true
		}
	}
	if !found {
		t.Error("未找到插入的行")
	}
}

func TestQueryRepository(t *testing.T) {
	store, conn := newTestStore(t)
	query := NewQueryRepository(store, conn)
	ctx := context.Background()

	res, err := query.Execute(ctx, "SELECT id, name FROM users ORDER BY id", 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !res.IsQuery || len(res.Rows) != 2 || !res.Truncated {
		t.Errorf("行数限制未生效: rows=%d truncated=%v", len(res.Rows), res.Truncated)
	}

	res, err = query.Execute(ctx, "DELETE FROM logs WHERE msg = 'a'", 0)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if res.IsQuery || res.RowsAffected != 1 {
		t.Errorf("影响行数错误: %+v", res)
	}

	if _, err := query.Execute(ctx, "SELECT 1; SELECT 2", 0); err == nil {
		t.Error("多语句应被拒绝")
	}

	if _, err := query.Execute(ctx, "SELECT * FROM not_exist", 0); err == nil {
		t.Error("查询不存在的表应报错")
	}
}

func TestExecuteBatch(t *testing.T) {
	store, conn := newTestStore(t)
	query := NewQueryRepository(store, conn)
	ctx := context.Background()

	// 全部成功：两条写入 + 一条查询
	res, err := query.ExecuteBatch(ctx, []string{
		"INSERT INTO users (name, score) VALUES ('batch1', 60)",
		"UPDATE users SET score = 61 WHERE name = 'batch1'",
		"SELECT id FROM users WHERE name = 'batch1'",
	}, 100)
	if err != nil {
		t.Fatalf("ExecuteBatch 失败: %v", err)
	}
	if res.FailedIndex != -1 || res.RolledBack {
		t.Fatalf("应全部成功: %+v", res)
	}
	if len(res.Items) != 3 || res.Items[0].RowsAffected != 1 || res.Items[1].RowsAffected != 1 {
		t.Errorf("条目结果错误: %+v", res.Items)
	}
	if !res.Items[2].IsQuery || res.Items[2].Result == nil || len(res.Items[2].Result.Rows) != 1 {
		t.Errorf("查询条目错误: %+v", res.Items[2])
	}

	// 中途失败：整体回滚
	before := countRows(t, store, conn, "logs")
	res, err = query.ExecuteBatch(ctx, []string{
		"INSERT INTO logs (msg) VALUES ('will-rollback')",
		"DELETE FROM logs WHERE not_exist_col = 1", // 失败
		"INSERT INTO logs (msg) VALUES ('never')",
	}, 0)
	if err != nil {
		t.Fatalf("ExecuteBatch 应返回结构化结果而非 error: %v", err)
	}
	if res.FailedIndex != 1 || !res.RolledBack {
		t.Errorf("失败索引/回滚标记错误: %+v", res)
	}
	if len(res.Items) != 2 || res.Items[1].Error == "" {
		t.Errorf("已完成条目应包含错误信息: %+v", res.Items)
	}
	if got := countRows(t, store, conn, "logs"); got != before {
		t.Errorf("事务应整体回滚: before=%d after=%d", before, got)
	}

	// 拒绝显式事务控制
	if _, err := query.ExecuteBatch(ctx, []string{"BEGIN", "SELECT 1"}, 0); err == nil {
		t.Error("显式事务控制应被拒绝")
	}

	// 空输入
	if _, err := query.ExecuteBatch(ctx, nil, 0); err == nil {
		t.Error("空语句列表应报错")
	}
}

func countRows(t *testing.T, store *Store, conn string, table string) int64 {
	t.Helper()
	db, err := store.DB(conn)
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	return n
}

func TestStoreMultiConnection(t *testing.T) {
	store, conn := newTestStore(t)

	// 打开第二个库：两个连接并存
	path2 := filepath.Join(t.TempDir(), "second.db")
	if err := os.WriteFile(path2, nil, 0o644); err != nil {
		t.Fatalf("创建第二个文件失败: %v", err)
	}
	info2, err := store.Open(path2, false, false)
	if err != nil {
		t.Fatalf("打开第二个库失败: %v", err)
	}
	if store.Count() != 2 || info2.ConnID == conn {
		t.Fatalf("应有 2 个独立连接: count=%d", store.Count())
	}
	db2, err := store.DB(info2.ConnID)
	if err != nil {
		t.Fatalf("第二个连接不可用: %v", err)
	}
	if _, err := db2.Exec(`CREATE TABLE t (a)`); err != nil {
		t.Fatalf("第二个库建表失败: %v", err)
	}
	if _, err := store.DB(conn); err != nil {
		t.Fatal("原连接应仍可用")
	}

	// 同一路径重复打开：复用连接
	reused, err := store.Open(path2, false, false)
	if err != nil {
		t.Fatalf("重复打开失败: %v", err)
	}
	if !reused.Reused || reused.ConnID != info2.ConnID || store.Count() != 2 {
		t.Fatalf("同路径应复用连接: reused=%v connID=%q count=%d", reused.Reused, reused.ConnID, store.Count())
	}

	// 关闭一个连接不影响另一个
	if err := store.CloseConn(info2.ConnID); err != nil {
		t.Fatalf("关闭连接失败: %v", err)
	}
	if store.Count() != 1 {
		t.Fatalf("应剩 1 个连接: %d", store.Count())
	}
	if _, err := store.DB(info2.ConnID); err == nil {
		t.Fatal("已关闭连接应不可用")
	}
	// 复用后再次打开原路径应建立新连接（旧的已关）
	reopened, err := store.Open(path2, false, false)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	if reopened.Reused || reopened.ConnID == conn {
		t.Fatalf("应建立新连接: %+v", reopened)
	}
}

// 不同大小写的同一路径必须复用同一连接（Windows 文件系统大小写不敏感；
// 若各建连接，两个连接会互相竞争文件锁导致 SQLITE_BUSY）。
func TestStorePathKeyReuse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("大小写折叠仅对 Windows 文件系统有意义")
	}
	store, conn := newTestStore(t)

	path := store.Path(conn)
	variant := strings.ToUpper(path)
	if variant == path {
		variant = strings.ToLower(path)
	}
	info, err := store.Open(variant, false, false)
	if err != nil {
		t.Fatalf("以不同大小写打开同一路径失败: %v", err)
	}
	if !info.Reused || info.ConnID != conn || store.Count() != 1 {
		t.Fatalf("不同大小写的同一路径应复用连接: reused=%v connID=%q count=%d", info.Reused, info.ConnID, store.Count())
	}
}

func TestStoreBackup(t *testing.T) {
	store, conn := newTestStore(t)
	ctx, err := store.Context(conn)
	if err != nil {
		t.Fatalf("获取上下文失败: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "backup.db")
	result, err := store.Backup(ctx, conn, dest)
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	if result.Size == 0 || result.ElapsedMs < 0 {
		t.Errorf("备份结果异常: %+v", result)
	}

	// 回读备份：结构与数据完整
	bak, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("打开备份失败: %v", err)
	}
	defer bak.Close()
	var n int
	if err := bak.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 3 {
		t.Errorf("备份数据不完整: n=%d err=%v", n, err)
	}
	var indexes int
	if err := bak.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_users_name'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Errorf("备份应包含索引: %d %v", indexes, err)
	}

	// 目标已存在时拒绝覆盖
	if _, err := store.Backup(ctx, conn, dest); err == nil {
		t.Error("目标文件已存在时应报错")
	}

	// 空路径
	if _, err := store.Backup(ctx, conn, "  "); err == nil {
		t.Error("空路径应报错")
	}
}

func TestGetCellBlob(t *testing.T) {
	store, conn := newTestStore(t)
	db, _ := store.DB(conn)
	if _, err := db.Exec(`CREATE TABLE files (id INTEGER PRIMARY KEY, data BLOB)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO files VALUES (1, x'DEADBEEF'), (2, NULL), (3, x'')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	repo := NewDataRepository(store, conn)
	ctx := context.Background()

	// 完整内容
	content, err := repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(1)}, "data")
	if err != nil {
		t.Fatalf("GetCellBlob 失败: %v", err)
	}
	if content.Size != 4 || content.B64 != "3q2+7w==" {
		t.Errorf("内容错误: %+v", content)
	}

	// NULL 报错
	if _, err := repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(2)}, "data"); err == nil {
		t.Error("NULL 单元格应报错")
	}

	// 空 BLOB 合法
	content, err = repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(3)}, "data")
	if err != nil || content.Size != 0 || content.B64 != "" {
		t.Errorf("空 BLOB 应返回空内容: %+v err=%v", content, err)
	}

	// 不存在的行
	if _, err := repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(99)}, "data"); err == nil {
		t.Error("不存在的行应报错")
	}
	// 非法列名
	if _, err := repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(1)}, `bad"col`); err == nil {
		t.Error("非法列名应报错")
	}

	// 超过 32MB 上限：先查 length() 即拒绝，不把内容物化进内存
	if _, err := db.Exec(`INSERT INTO files VALUES (4, ?)`, make([]byte, maxBlobTransferBytes+1)); err != nil {
		t.Fatalf("插入超大 BLOB 失败: %v", err)
	}
	if _, err := repo.GetCellBlob(ctx, "files", map[string]any{"id": float64(4)}, "data"); err == nil ||
		!strings.Contains(err.Error(), "32 MB") {
		t.Errorf("超大 BLOB 应在长度检查处拒绝: %v", err)
	}
}

func TestDuplicateRows(t *testing.T) {
	store, conn := newTestStore(t)
	db, _ := store.DB(conn)
	repo := NewDataRepository(store, conn)
	ctx := context.Background()

	// users：单列 INTEGER 主键 → 复制取新自增 id
	n, err := repo.DuplicateRows(ctx, "users", []map[string]any{{"id": float64(1)}})
	if err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("复制行数 = %d, want 1", n)
	}
	var cnt int
	var maxID int64
	if err := db.QueryRow(`SELECT COUNT(*), MAX(id) FROM users`).Scan(&cnt, &maxID); err != nil || cnt != 4 {
		t.Fatalf("复制后行数错误: %d %v", cnt, err)
	}
	if maxID != 4 {
		t.Errorf("新行应取新自增 id: max=%d", maxID)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM users WHERE id = 4`).Scan(&name); err != nil || name != "张三" {
		t.Errorf("复制内容错误: %q %v", name, err)
	}

	// logs：无主键 rowid 表 → 整行复制
	var before int
	db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&before)
	if _, err := repo.DuplicateRows(ctx, "logs", []map[string]any{{
		"__rowid__": func() any {
			var rid int64
			db.QueryRow(`SELECT rowid FROM logs LIMIT 1`).Scan(&rid)
			return float64(rid)
		}(),
	}}); err != nil {
		t.Fatalf("rowid 表复制失败: %v", err)
	}
	var after int
	db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&after)
	if after != before+1 {
		t.Errorf("rowid 表复制后行数错误: before=%d after=%d", before, after)
	}

	// TEXT 主键冲突 → 报错并整体回滚
	if _, err := db.Exec(`CREATE TABLE codes (code TEXT PRIMARY KEY, val INT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO codes VALUES ('A', 1), ('B', 2)`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	if _, err := repo.DuplicateRows(ctx, "codes", []map[string]any{{"code": "A"}}); err == nil {
		t.Error("TEXT 主键复制应冲突报错")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM codes`).Scan(&cnt); err != nil || cnt != 2 {
		t.Errorf("失败后应整体回滚: %d %v", cnt, err)
	}

	// 空选择 / 非法表名
	if _, err := repo.DuplicateRows(ctx, "users", nil); err == nil {
		t.Error("空选择应报错")
	}
	if _, err := repo.DuplicateRows(ctx, `bad"name`, []map[string]any{{"id": float64(1)}}); err == nil {
		t.Error("非法表名应报错")
	}
}

// TestBigintPrecision V3.0：超出 JS 安全整数范围的 int64 应以 bigint 标记
// 传输，游标分页与大整数主键定位（UpdateCell）不受影响。
func TestBigintPrecision(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)
	db, _ := store.DB(conn)
	ctx := context.Background()

	if _, err := db.Exec(`CREATE TABLE big (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// 2^53+1 与 int64 最大值：JSON Number 均无法精确表示
	if _, err := db.Exec(`INSERT INTO big VALUES (9007199254740993, 'a'), (9223372036854775807, 'b')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	page, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "big", PageSize: 1})
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("行数错误: %d", len(page.Rows))
	}
	cellVal := page.Rows[0][0]
	m, ok := cellVal.(cell.Marker)
	if !ok || m.Kind != cell.KindBigint {
		t.Fatalf("期望 bigint 标记, 得到 %#v", cellVal)
	}
	if m.Str != "9007199254740993" {
		t.Errorf("Str = %q", m.Str)
	}
	if !page.CursorMode || page.NextCursor != "9007199254740993" || !page.HasNext {
		t.Errorf("bigint 游标错误: mode=%v next=%q hasNext=%v", page.CursorMode, page.NextCursor, page.HasNext)
	}

	// bigint 主键游标翻页
	second, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "big", PageSize: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("游标翻页失败: %v", err)
	}
	if len(second.Rows) != 1 || second.HasNext {
		t.Fatalf("第 2 页错误: rows=%d hasNext=%v", len(second.Rows), second.HasNext)
	}
	m2, ok := second.Rows[0][0].(cell.Marker)
	if !ok || m2.Str != "9223372036854775807" {
		t.Fatalf("第 2 页主键错误: %#v", second.Rows[0][0])
	}

	// bigint 标记作为行定位 key 回传，Decode 还原后 UPDATE 命中
	if _, err := data.UpdateCell(ctx, "big",
		map[string]any{"id": map[string]any{"__kind": cell.KindBigint, "str": "9223372036854775807"}},
		"note", "edited"); err != nil {
		t.Fatalf("bigint 主键更新失败: %v", err)
	}
	var note string
	if err := db.QueryRow(`SELECT note FROM big WHERE id = 9223372036854775807`).Scan(&note); err != nil || note != "edited" {
		t.Errorf("更新未生效: %q %v", note, err)
	}

	// 编辑值本身是超范围整数字符串：必须原样绑定存储，不得经数值转换丢精度
	if _, err := data.UpdateCell(ctx, "big",
		map[string]any{"id": map[string]any{"__kind": cell.KindBigint, "str": "9223372036854775807"}},
		"note", "9007199254740993x"); err != nil {
		t.Fatalf("超范围整数字符串编辑失败: %v", err)
	}
	var note2 string
	if err := db.QueryRow(`SELECT note FROM big WHERE id = 9223372036854775807`).Scan(&note2); err != nil || note2 != "9007199254740993x" {
		t.Errorf("超范围整数字符串应原样存储: %q %v", note2, err)
	}
}

// TestFilterConditions V3.0 列筛选构建器：操作符白名单、列存在性校验、
// 与游标分页/快速过滤组合。
func TestFilterConditions(t *testing.T) {
	store, conn := newTestStore(t)
	data := NewDataRepository(store, conn)
	ctx := context.Background()

	fetch := func(conds []domain.FilterCondition) domain.TableData {
		t.Helper()
		out, err := data.GetTableData(ctx, domain.TableDataRequest{
			Table: "users", PageSize: 100, Conditions: conds,
		})
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		return out
	}

	// eq：数值列字符串值经亲和性转换
	out := fetch([]domain.FilterCondition{{Column: "id", Op: domain.FilterEq, Value: "2"}})
	if len(out.Rows) != 1 || out.Rows[0][1] != "李四" {
		t.Fatalf("eq 结果错误: %v", out.Rows)
	}
	// ne / gt / le
	if n := len(fetch([]domain.FilterCondition{{Column: "id", Op: domain.FilterNe, Value: "2"}}).Rows); n != 2 {
		t.Fatalf("ne 结果数错误: %d", n)
	}
	if n := len(fetch([]domain.FilterCondition{{Column: "score", Op: domain.FilterGt, Value: "75"}}).Rows); n != 2 {
		t.Fatalf("gt 结果数错误: %d", n)
	}
	if n := len(fetch([]domain.FilterCondition{{Column: "score", Op: domain.FilterLe, Value: "70"}}).Rows); n != 1 {
		t.Fatalf("le 结果数错误: %d", n)
	}
	// like 含通配符转义（无匹配）
	if n := len(fetch([]domain.FilterCondition{{Column: "name", Op: domain.FilterLike, Value: "%"}}).Rows); n != 0 {
		t.Fatalf("like 通配符应被转义: %d", n)
	}
	if n := len(fetch([]domain.FilterCondition{{Column: "name", Op: domain.FilterLike, Value: "张"}}).Rows); n != 1 {
		t.Fatalf("like 结果数错误: %d", n)
	}
	// range 双边 / 单边
	if n := len(fetch([]domain.FilterCondition{{Column: "score", Op: domain.FilterRange, Value: "75", Value2: "85"}}).Rows); n != 1 {
		t.Fatalf("range 结果数错误: %d", n)
	}
	if n := len(fetch([]domain.FilterCondition{{Column: "score", Op: domain.FilterRange, Value: "80"}}).Rows); n != 2 {
		t.Fatalf("range 单边结果数错误: %d", n)
	}
	// is-null / not-null（logs.msg 全非空）
	if n := len(fetch([]domain.FilterCondition{{Column: "name", Op: domain.FilterNotNull}}).Rows); n != 3 {
		t.Fatalf("not-null 结果数错误: %d", n)
	}
	// 多条件 AND
	if n := len(fetch([]domain.FilterCondition{
		{Column: "score", Op: domain.FilterGe, Value: "70"},
		{Column: "name", Op: domain.FilterLike, Value: "五"},
	}).Rows); n != 1 {
		t.Fatalf("AND 组合结果数错误: %d", n)
	}
	// 与快速过滤并存
	out, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "users", PageSize: 100, Filter: "四",
		Conditions: []domain.FilterCondition{{Column: "score", Op: domain.FilterLt, Value: "100"}},
	})
	if err != nil || len(out.Rows) != 1 {
		t.Fatalf("组合过滤错误: %v %d", err, len(out.Rows))
	}
	// 与游标分页组合：条件筛选下逐页翻
	page1, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "users", PageSize: 2,
		Conditions: []domain.FilterCondition{{Column: "id", Op: domain.FilterGe, Value: "1"}},
	})
	if err != nil || !page1.CursorMode || len(page1.Rows) != 2 || !page1.HasNext {
		t.Fatalf("筛选游标页错误: %+v %v", page1, err)
	}
	page2, err := data.GetTableData(ctx, domain.TableDataRequest{
		Table: "users", PageSize: 2, Cursor: page1.NextCursor,
		Conditions: []domain.FilterCondition{{Column: "id", Op: domain.FilterGe, Value: "1"}},
	})
	if err != nil || len(page2.Rows) != 1 || page2.HasNext {
		t.Fatalf("筛选游标第 2 页错误: %v", err)
	}

	// 校验失败场景：未知列 / 未知操作符 / range 无边界
	for _, conds := range [][]domain.FilterCondition{
		{{Column: "nope", Op: domain.FilterEq, Value: "1"}},
		{{Column: "id", Op: "evil", Value: "1"}},
		{{Column: "score", Op: domain.FilterRange}},
	} {
		if _, err := data.GetTableData(ctx, domain.TableDataRequest{Table: "users", Conditions: conds}); err == nil {
			t.Errorf("应报错: %#v", conds)
		}
	}
	// CountFiltered 同一套条件构造
	total, err := data.CountFiltered(ctx, "users", "", []domain.FilterCondition{{Column: "score", Op: domain.FilterGt, Value: "75"}})
	if err != nil || total != 2 {
		t.Fatalf("CountFiltered 条件统计错误: %d %v", total, err)
	}
}

// TestExecRedesignPlanChunked V3.0：分块拷贝的行数正确性与进度回调单调性。
func TestExecRedesignPlanChunked(t *testing.T) {
	store, conn := newTestStore(t)
	db, _ := store.DB(conn)
	ctx := context.Background()
	repo := NewSchemaRepository(store, conn)

	if _, err := db.Exec(`CREATE TABLE big_src (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// 2500 行（rowid 有空洞，验证 rowid 范围分块不依赖连续性）
	var b strings.Builder
	b.WriteString(`INSERT INTO big_src VALUES `)
	for i := 0; i < 2500; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "(%d, 'v%d')", i*3, i)
	}
	if _, err := db.Exec(b.String()); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	defer db.Exec(`DROP TABLE IF EXISTS __rs_tmp_big`)

	plan := RedesignPlan{
		Pre: []string{`CREATE TABLE "__rs_tmp_big" ("id" INTEGER PRIMARY KEY, "v" TEXT)`},
		Copy: &RedesignCopyStep{
			Target:     `"__rs_tmp_big"`,
			TargetCols: []string{`"id"`, `"v"`},
			Source:     `"big_src"`,
			SourceCols: []string{`"id"`, `"v"`},
			ByRowid:    true,
			Total:      2500,
		},
	}
	var callbacks []int64
	if err := repo.ExecRedesignPlan(ctx, plan, 1000, func(copied int64) {
		callbacks = append(callbacks, copied)
	}); err != nil {
		t.Fatalf("分块重建失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "__rs_tmp_big"`).Scan(&n); err != nil || n != 2500 {
		t.Fatalf("分块拷贝行数错误: %d %v", n, err)
	}
	if len(callbacks) != 3 || callbacks[0] != 1000 || callbacks[1] != 2000 || callbacks[2] != 2500 {
		t.Fatalf("进度回调错误: %v", callbacks)
	}

	// WITHOUT ROWID 表：整条拷贝，一次回调
	if _, err := db.Exec(`CREATE TABLE wo_src (k TEXT PRIMARY KEY, v INT) WITHOUT ROWID`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wo_src VALUES ('a', 1), ('b', 2)`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	defer db.Exec(`DROP TABLE IF EXISTS __rs_tmp_wo`)
	plan2 := RedesignPlan{
		Pre: []string{`CREATE TABLE "__rs_tmp_wo" ("k" TEXT PRIMARY KEY, "v" INTEGER) WITHOUT ROWID`},
		Copy: &RedesignCopyStep{
			Target:     `"__rs_tmp_wo"`,
			TargetCols: []string{`"k"`, `"v"`},
			Source:     `"wo_src"`,
			SourceCols: []string{`"k"`, `"v"`},
			ByRowid:    false,
			Total:      2,
		},
	}
	callbacks = nil
	if err := repo.ExecRedesignPlan(ctx, plan2, 1000, func(copied int64) {
		callbacks = append(callbacks, copied)
	}); err != nil {
		t.Fatalf("WITHOUT ROWID 重建失败: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM "__rs_tmp_wo"`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("WITHOUT ROWID 行数错误: %d %v", n, err)
	}
	if len(callbacks) != 1 || callbacks[0] != 2 {
		t.Fatalf("WITHOUT ROWID 进度回调错误: %v", callbacks)
	}
}
