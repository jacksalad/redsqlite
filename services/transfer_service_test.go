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

// newTransferFixture 建两个独立临时库（源/目标），返回传输服务与两个连接 ID。
func newTransferFixture(t *testing.T) (*TransferService, string, string, *sql.DB, *sql.DB, *repository.Store) {
	t.Helper()
	store := repository.NewStore()
	open := func(name, schema string) (string, *sql.DB) {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("创建测试文件失败: %v", err)
		}
		info, err := store.Open(path, false, false)
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", name, err)
		}
		db, _ := store.DB(info.ConnID)
		if schema != "" {
			if _, err := db.Exec(schema); err != nil {
				t.Fatalf("初始化 %s 失败: %v", name, err)
			}
		}
		return info.ConnID, db
	}
	srcConn, srcDB := open("src.db", `
		CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, score REAL, avatar BLOB);
		CREATE INDEX idx_users_name ON users(name);
		CREATE TABLE logs (msg TEXT, at TEXT);
		CREATE TRIGGER trg_users_log AFTER INSERT ON users BEGIN INSERT INTO logs(msg) VALUES ('new'); END;
		INSERT INTO users VALUES (1, '张三', 90.5, x'0102AB'), (2, '李四', NULL, NULL), (3, '王五', 70, x'');
	`)
	tgtConn, tgtDB := open("tgt.db", "")
	t.Cleanup(func() { _ = store.Close() })
	return NewTransferService(store), srcConn, tgtConn, srcDB, tgtDB, store
}

func TestTransferStructureAndData(t *testing.T) {
	svc, srcConn, tgtConn, srcDB, tgtDB, _ := newTransferFixture(t)

	req := domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: tgtConn, TargetTable: "users",
		IncludeData: true, IncludeIndexes: true, IncludeTriggers: true,
	}
	preview, err := svc.PreviewTransfer(req)
	if err != nil {
		t.Fatalf("预检失败: %v", err)
	}
	if preview.Blocked != "" {
		t.Fatalf("不应阻断: %s", preview.Blocked)
	}
	if preview.ColumnCount != 4 || preview.RowCount != 3 {
		t.Fatalf("列/行数错误: %+v", preview)
	}
	if len(preview.IndexDDLs) != 1 || len(preview.TriggerDDLs) != 1 {
		t.Fatalf("索引/触发器清单错误: %v %v", preview.IndexDDLs, preview.TriggerDDLs)
	}
	joined := strings.Join(preview.Warnings, "\n")
	if !strings.Contains(joined, "AUTOINCREMENT") || !strings.Contains(joined, "触发器") {
		t.Fatalf("缺少预期提示: %v", preview.Warnings)
	}

	result, err := svc.ApplyTransfer(req)
	if err != nil {
		t.Fatalf("传输失败: %v", err)
	}
	if result.RowsCopied != 3 {
		t.Fatalf("行数错误: %+v", result)
	}

	// 数据一致（含 NULL / BLOB / 空 BLOB / 浮点）
	var name string
	var score sql.NullFloat64
	var avatar []byte
	if err := tgtDB.QueryRow(`SELECT name, score, avatar FROM users WHERE id = 1`).Scan(&name, &score, &avatar); err != nil {
		t.Fatalf("读取目标失败: %v", err)
	}
	if name != "张三" || !score.Valid || score.Float64 != 90.5 || len(avatar) != 3 || avatar[0] != 0x01 {
		t.Fatalf("行 1 数据错误: %q %v %v", name, score, avatar)
	}
	if err := tgtDB.QueryRow(`SELECT score FROM users WHERE id = 2`).Scan(&score); err != nil || score.Valid {
		t.Fatalf("NULL 传输错误: %+v %v", score, err)
	}
	if err := tgtDB.QueryRow(`SELECT avatar FROM users WHERE id = 3`).Scan(&avatar); err != nil {
		t.Fatalf("空 BLOB 读取失败: %v", err)
	}
	// 注意：modernc 驱动把空 BLOB 扫成 nil，长度为 0 即正确
	if len(avatar) != 0 {
		t.Fatalf("空 BLOB 错误: %v", avatar)
	}

	// 索引与触发器连带
	var idxCount, trgCount int
	tgtDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_users_name'`).Scan(&idxCount)
	tgtDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='trg_users_log'`).Scan(&trgCount)
	if idxCount != 1 || trgCount != 1 {
		t.Fatalf("索引/触发器未传输: %d %d", idxCount, trgCount)
	}
	// 触发器在目标库生效（其 body 引用的 logs 表需先存在——SQLite 建触发器
	// 时不校验 body 引用的表，只在执行时才报错）
	if _, err := tgtDB.Exec(`CREATE TABLE logs (msg TEXT, at TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := tgtDB.Exec(`INSERT INTO users (name) VALUES ('新')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	var logCount int
	tgtDB.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("触发器未生效: %d", logCount)
	}
	// 大整数精度（与 ① 联动）：传输不受 JSON 编码影响，直接驱动值拷贝
	if _, err := srcDB.Exec(`INSERT INTO users VALUES (9223372036854775807, '大', 1, NULL)`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	req2 := domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: tgtConn, TargetTable: "users_big",
		IncludeData: true,
	}
	if _, err := svc.ApplyTransfer(req2); err != nil {
		t.Fatalf("二次传输失败: %v", err)
	}
	var big int64
	if err := tgtDB.QueryRow(`SELECT id FROM users_big WHERE name = '大'`).Scan(&big); err != nil || big != 9223372036854775807 {
		t.Fatalf("大整数传输错误: %d %v", big, err)
	}
}

func TestTransferStructureOnly(t *testing.T) {
	svc, srcConn, tgtConn, _, tgtDB, _ := newTransferFixture(t)

	result, err := svc.ApplyTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: tgtConn, TargetTable: "users_empty",
		IncludeData: false, IncludeIndexes: true, IncludeTriggers: false,
	})
	if err != nil {
		t.Fatalf("仅结构传输失败: %v", err)
	}
	if result.RowsCopied != 0 {
		t.Fatalf("仅结构不应有行: %+v", result)
	}
	var n int
	if err := tgtDB.QueryRow(`SELECT COUNT(*) FROM users_empty`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("目标应为空表: %d %v", n, err)
	}
	// 目标表 DDL 与源一致（表名已改写）
	var ddl string
	if err := tgtDB.QueryRow(`SELECT sql FROM sqlite_master WHERE name='users_empty'`).Scan(&ddl); err != nil ||
		!strings.Contains(ddl, `"users_empty"`) || !strings.Contains(ddl, "AUTOINCREMENT") {
		t.Fatalf("目标 DDL 错误: %q %v", ddl, err)
	}
}

func TestTransferBlockedAndRollback(t *testing.T) {
	svc, srcConn, tgtConn, _, tgtDB, _ := newTransferFixture(t)

	// 目标同名冲突（先在目标库建同名表）
	if _, err := tgtDB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	preview, err := svc.PreviewTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: tgtConn, TargetTable: "users",
	})
	if err != nil {
		t.Fatalf("预检失败: %v", err)
	}
	if preview.Blocked == "" || !strings.Contains(preview.Blocked, "同名") {
		t.Fatalf("同名应阻断: %q", preview.Blocked)
	}

	// 源对象不是表
	if _, err := svc.PreviewTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "idx_users_name",
		TargetConnID: tgtConn, TargetTable: "x",
	}); err == nil {
		t.Fatal("非表对象应报错")
	}

	// 传输失败整体回滚：目标库已存在同名索引（挂在别的表上）→
	// CREATE INDEX idx_users_name 失败 → 整体回滚，不残留表/数据
	if _, err := tgtDB.Exec(`CREATE TABLE other (name TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := tgtDB.Exec(`CREATE INDEX idx_users_name ON other(name)`); err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	var tableCount int
	tgtDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tableCount)
	if _, err := svc.ApplyTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: tgtConn, TargetTable: "rb_target",
		IncludeData: true, IncludeIndexes: true, IncludeTriggers: true,
	}); err == nil {
		t.Fatal("索引名冲突应失败")
	}
	var after int
	tgtDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&after)
	if after != tableCount {
		t.Fatalf("失败后应整体回滚: before=%d after=%d", tableCount, after)
	}
	var rowCount int
	tgtDB.QueryRow(`SELECT COUNT(*) FROM rb_target`).Scan(&rowCount) // 表不应存在
	_ = rowCount
}

func TestTransferSameConnectionRejected(t *testing.T) {
	svc, srcConn, _, _, _, _ := newTransferFixture(t)

	// 同连接传输必须拒绝：目标事务会占住唯一连接（SetMaxOpenConns(1)），
	// 读源数据会死锁；同库复制表走 SQL 页
	if _, err := svc.PreviewTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: srcConn, TargetTable: "users_copy",
	}); err == nil || !strings.Contains(err.Error(), "同一连接") {
		t.Fatalf("同连接应拒绝: %v", err)
	}
	if _, err := svc.ApplyTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "users",
		TargetConnID: srcConn, TargetTable: "users_copy",
		IncludeData: true,
	}); err == nil {
		t.Fatal("同连接 Apply 应拒绝")
	}
}

func TestTransferForeignKeyWarning(t *testing.T) {
	svc, srcConn, _, srcDB, _, store := newTransferFixture(t)

	// 外键父表在目标库缺失 → 预检给出警告
	if _, err := srcDB.Exec(`CREATE TABLE child (id INTEGER PRIMARY KEY, uid INT REFERENCES users(id))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// 目标 = 同一 Store 下的第三个独立临时库（无 users 表）
	path := filepath.Join(t.TempDir(), "tgt2.db")
	os.WriteFile(path, nil, 0o644)
	info, err := store.Open(path, false, false)
	if err != nil {
		t.Fatalf("打开目标失败: %v", err)
	}

	preview, err := svc.PreviewTransfer(domain.TransferRequest{
		SourceConnID: srcConn, SourceTable: "child",
		TargetConnID: info.ConnID, TargetTable: "child", IncludeData: true,
	})
	if err != nil {
		t.Fatalf("预检失败: %v", err)
	}
	if preview.Blocked != "" {
		t.Fatalf("不应阻断: %s", preview.Blocked)
	}
	joined := strings.Join(preview.Warnings, "\n")
	if !strings.Contains(joined, "users") || !strings.Contains(joined, "外键") {
		t.Fatalf("缺少外键缺失提示: %v", preview.Warnings)
	}
}
