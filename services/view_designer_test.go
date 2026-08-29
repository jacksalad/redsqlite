package services

import (
	"strings"
	"testing"

	"github.com/redjack/redsqlite/domain"
)

// TestViewDesignerCreateApply 新建视图：预览 SQL 由后端生成、重名阻断、
// 执行后可查询，非法主体（写入语句/多语句/PRAGMA）被拒绝。
func TestViewDesignerCreateApply(t *testing.T) {
	svc, conn := newTestSchemaService(t)

	// 正常创建
	p, err := svc.PreviewCreateView(conn, domain.ViewSpec{Name: "v_adult", Body: "SELECT id, name FROM users WHERE score > 85"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("不应阻断: %s", p.Blocked)
	}
	if len(p.SQL) != 1 || !strings.Contains(p.SQL[0], `CREATE VIEW "v_adult" AS SELECT id, name FROM users WHERE score > 85`) {
		t.Fatalf("SQL 不符: %v", p.SQL)
	}
	if _, err := svc.ApplyCreateView(conn, domain.ViewSpec{Name: "v_adult", Body: "SELECT id, name FROM users WHERE score > 85"}); err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 重名阻断
	p, err = svc.PreviewCreateView(conn, domain.ViewSpec{Name: "v_adult", Body: "SELECT 1"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked == "" || !strings.Contains(p.Blocked, "同名") {
		t.Fatalf("重名应阻断: %q", p.Blocked)
	}

	// 非查询主体 / 多语句注入 / PRAGMA
	for _, body := range []string{
		"DELETE FROM users",
		"SELECT 1; DROP TABLE users",
		"PRAGMA user_version",
		"",
	} {
		p, err := svc.PreviewCreateView(conn, domain.ViewSpec{Name: "v_bad", Body: body})
		if err != nil {
			t.Fatalf("预览失败: %v", err)
		}
		if p.Blocked == "" {
			t.Fatalf("主体 %q 应被阻断", body)
		}
	}

	// 非法视图名
	if _, err := svc.ApplyCreateView(conn, domain.ViewSpec{Name: `bad"name`, Body: "SELECT 1"}); err == nil {
		t.Fatal("非法视图名应报错")
	}
}

// TestViewDesignerAlter 修改视图 = DROP+CREATE 单事务；
// 引用该视图的其他视图得到警告提示。
func TestViewDesignerAlter(t *testing.T) {
	svc, conn := newTestSchemaService(t)
	db, _ := svc.store.DB(conn)
	if _, err := db.Exec(`CREATE VIEW v_high AS SELECT * FROM v_users WHERE id > 1`); err != nil {
		t.Fatalf("建嵌套视图失败: %v", err)
	}

	p, err := svc.PreviewAlterView(conn, domain.ViewSpec{Name: "v_users", Body: "SELECT id FROM users"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("不应阻断: %s", p.Blocked)
	}
	if len(p.SQL) != 2 || !strings.HasPrefix(p.SQL[0], `DROP VIEW "v_users"`) || !strings.HasPrefix(p.SQL[1], `CREATE VIEW "v_users"`) {
		t.Fatalf("DROP+CREATE 不符: %v", p.SQL)
	}
	joined := strings.Join(p.Warnings, "\n")
	if !strings.Contains(joined, "v_high") {
		t.Fatalf("应提示引用视图: %v", p.Warnings)
	}

	if err := svc.ApplyAlterView(conn, domain.ViewSpec{Name: "v_users", Body: "SELECT id FROM users"}); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	// 嵌套视图引用的 name 列已不存在（SQLite 运行时才报错，符合预期）
	// 注意：单连接池（SetMaxOpenConns(1)）下 Query 的 rows 必须及时关闭，否则阻塞后续查询
	rows, err := db.Query(`SELECT * FROM v_users`)
	if err != nil {
		t.Fatalf("新视图应可查询: %v", err)
	}
	rows.Close()

	// 修改不存在的视图 → 阻断
	p, err = svc.PreviewAlterView(conn, domain.ViewSpec{Name: "v_missing", Body: "SELECT 1"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked == "" {
		t.Fatal("不存在的视图应阻断")
	}

	// 修改表对象 → 阻断
	p, err = svc.PreviewAlterView(conn, domain.ViewSpec{Name: "users", Body: "SELECT 1"})
	if err != nil || p.Blocked == "" {
		t.Fatalf("修改表对象应阻断: %v %q", err, p.Blocked)
	}

	// 重命名视图：DROP 旧名 + CREATE 新名；引用旧名的对象得到警告
	p, err = svc.PreviewAlterView(conn, domain.ViewSpec{OldName: "v_high", Name: "v_top", Body: "SELECT 1 AS n"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("重命名不应阻断: %s", p.Blocked)
	}
	if len(p.SQL) != 2 || !strings.Contains(p.SQL[0], `DROP VIEW "v_high"`) || !strings.Contains(p.SQL[1], `CREATE VIEW "v_top"`) {
		t.Fatalf("重命名 SQL 不符: %v", p.SQL)
	}
	// 重命名到已存在名称 → 阻断
	p, err = svc.PreviewAlterView(conn, domain.ViewSpec{OldName: "v_high", Name: "users", Body: "SELECT 1"})
	if err != nil || p.Blocked == "" {
		t.Fatalf("重命名到同名对象应阻断: %v %q", err, p.Blocked)
	}
	if err := svc.ApplyAlterView(conn, domain.ViewSpec{OldName: "v_high", Name: "v_top", Body: "SELECT 1 AS n"}); err != nil {
		t.Fatalf("重命名执行失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('v_high','v_top')`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("重命名结果错误: %d %v", n, err)
	}
}

// TestViewDesignerDrop 删除视图：引用警告、非视图对象阻断。
func TestViewDesignerDrop(t *testing.T) {
	svc, conn := newTestSchemaService(t)
	db, _ := svc.store.DB(conn)
	if _, err := db.Exec(`CREATE VIEW v_high AS SELECT * FROM v_users WHERE id > 1`); err != nil {
		t.Fatalf("建嵌套视图失败: %v", err)
	}

	p, err := svc.PreviewDropView(conn, "v_users")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if p.Blocked != "" {
		t.Fatalf("不应阻断: %s", p.Blocked)
	}
	joined := strings.Join(p.Warnings, "\n")
	if !strings.Contains(joined, "v_high") {
		t.Fatalf("应警告引用视图: %v", p.Warnings)
	}

	if err := svc.DropView(conn, "v_users"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'v_users'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("视图应已删除: %d %v", n, err)
	}

	p2, err := svc.PreviewDropView(conn, "users")
	if err != nil || p2.Blocked == "" {
		t.Fatalf("删除表对象应阻断: %v %q", err, p2.Blocked)
	}
}

// TestGetViewDefinition 编辑回填：从 sqlite_master 原文提取 SELECT 主体。
func TestGetViewDefinition(t *testing.T) {
	svc, conn := newTestSchemaService(t)
	db, _ := svc.store.DB(conn)

	spec, err := svc.GetViewDefinition(conn, "v_users")
	if err != nil {
		t.Fatalf("读取定义失败: %v", err)
	}
	if spec.Name != "v_users" || !strings.Contains(spec.Body, "SELECT id, name FROM users") {
		t.Fatalf("定义解析错误: %+v", spec)
	}

	// 特殊名称（含空格与中文）
	if _, err := db.Exec(`CREATE VIEW "我的 视图" AS SELECT 1 AS n`); err != nil {
		t.Fatalf("建视图失败: %v", err)
	}
	spec, err = svc.GetViewDefinition(conn, "我的 视图")
	if err != nil {
		t.Fatalf("读取定义失败: %v", err)
	}
	if spec.Name != "我的 视图" || !strings.Contains(spec.Body, "SELECT 1 AS n") {
		t.Fatalf("特殊名称解析错误: %+v", spec)
	}

	// 非视图对象
	if _, err := svc.GetViewDefinition(conn, "users"); err == nil {
		t.Fatal("读取表对象应报错")
	}
}
