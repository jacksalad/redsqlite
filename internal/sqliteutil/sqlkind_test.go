package sqliteutil

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  SELECT 1;  ", "SELECT 1"},
		{"-- comment\nSELECT 1", "SELECT 1"},
		{"/* c */ DELETE FROM t ;;", "DELETE FROM t"},
		{"SELECT '--not comment';", "SELECT '--not comment'"},
		{"SELECT 1;;;", "SELECT 1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in   string
		want SQLKind
	}{
		{"SELECT * FROM t", KindQuery},
		{"  -- x\nselect 1", KindQuery},
		{"WITH c AS (SELECT 1) SELECT * FROM c", KindQuery},
		{"PRAGMA table_info(users)", KindQuery},
		{"EXPLAIN SELECT 1", KindQuery},
		{"INSERT INTO t VALUES (1)", KindExec},
		{"update t set a=1", KindExec},
		{"DELETE FROM t", KindExec},
		{"CREATE TABLE t(a)", KindExec},
		{"", KindInvalid},
	}
	for _, c := range cases {
		if got := Classify(c.in); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCountStatements(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"SELECT 1", 1},
		{"SELECT 1;", 1},
		{"SELECT 1; SELECT 2", 2},
		{"SELECT ';' ; SELECT 2", 2},                 // 字符串内分号
		{"SELECT a\"b;c\" FROM t; DELETE FROM t", 2}, // 引号标识符内分号
		{"-- a; b\nSELECT 1", 1},                     // 注释内分号
		{"/* ; */ SELECT 1;", 1},
		{"", 0},
		{";;;", 0},
	}
	for _, c := range cases {
		if got := CountStatements(c.in); got != c.want {
			t.Errorf("CountStatements(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestValidateSingle(t *testing.T) {
	if _, err := ValidateSingle("SELECT 1; DELETE FROM t"); !errors.Is(err, ErrMultipleStatements) {
		t.Fatalf("应报多语句错误，实际: %v", err)
	}
	if _, err := ValidateSingle("  -- only comment\n"); !errors.Is(err, ErrEmptySQL) {
		t.Fatalf("应报空 SQL 错误，实际: %v", err)
	}
	if s, err := ValidateSingle(" select 1 ; "); err != nil || s != "select 1" {
		t.Fatalf("ValidateSingle 合法输入失败: %q, %v", s, err)
	}
}

func TestAnalyzeDangerous(t *testing.T) {
	cases := []struct {
		in       string
		danger   bool
		contains string
	}{
		{"DROP TABLE users", true, "DROP"},
		{"DELETE FROM users WHERE id = 1", true, "删除"},
		{"DELETE FROM users", true, "WHERE"}, // 无 WHERE
		{"UPDATE users SET a = 1", true, "WHERE"},
		{"UPDATE users SET a = 1 WHERE id = 2", false, ""},
		// 子查询里的 WHERE 不能算顶层条件：仍须提示整表更新
		{"UPDATE t SET x = (SELECT 1 WHERE 1=1)", true, "WHERE"},
		// 字符串字面量里的 WHERE 同理
		{"UPDATE t SET note = 'the where clause'", true, "WHERE"},
		{"SELECT * FROM users", false, ""},
		{"VACUUM", true, "VACUUM"},
		{"ALTER TABLE t ADD COLUMN c", true, "ALTER"},
		{"-- DROP TABLE x\nSELECT 1", false, ""},
	}
	for _, c := range cases {
		rep := AnalyzeDangerous(c.in)
		if rep.Dangerous != c.danger {
			t.Errorf("AnalyzeDangerous(%q).Dangerous = %v, want %v (reason=%s)", c.in, rep.Dangerous, c.danger, rep.Reason)
			continue
		}
		if c.contains != "" && rep.Reason == "" {
			t.Errorf("AnalyzeDangerous(%q) 缺少原因说明", c.in)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{";;;", []string{}},
		{"SELECT 1", []string{"SELECT 1"}},
		{"SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"  INSERT INTO t VALUES ('a;b') ;  DELETE FROM t  ", []string{"INSERT INTO t VALUES ('a;b')", "DELETE FROM t"}},
		{"-- comment\nSELECT 1; /* ; */ SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"SELECT 'it''s;ok'; SELECT 3", []string{"SELECT 'it''s;ok'", "SELECT 3"}},
		{"SELECT \"a;b\" FROM t", []string{`SELECT "a;b" FROM t`}},
	}
	for _, c := range cases {
		got := SplitStatements(c.in)
		if len(got) != len(c.want) {
			t.Errorf("SplitStatements(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("SplitStatements(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestHasTopLevelWhere(t *testing.T) {
	yes := []string{
		"UPDATE t SET a=1 WHERE id=2",
		"update t set a=1 where id=2",
		"DELETE FROM t WHERE id IN (SELECT id FROM b WHERE x=1)",
		"UPDATE t SET a='x' WHERE id=1",
		`UPDATE t SET a="WHERE" WHERE id=1`,
	}
	no := []string{
		"UPDATE t SET a=1",
		"DELETE FROM t",
		"UPDATE t SET x=(SELECT 1 WHERE 1=1)",
		"UPDATE t SET note='the where clause'",
		"UPDATE t SET a=WHEREID", // WHEREID 是单个标识符，不算关键字
	}
	for _, s := range yes {
		if !hasTopLevelWhere(Normalize(s)) {
			t.Errorf("hasTopLevelWhere(%q) 应为 true", s)
		}
	}
	for _, s := range no {
		if hasTopLevelWhere(Normalize(s)) {
			t.Errorf("hasTopLevelWhere(%q) 应为 false", s)
		}
	}
}

func TestIsTransactionControl(t *testing.T) {
	yes := []string{"BEGIN", "begin transaction", "COMMIT", "rollback", "SAVEPOINT sp1", "RELEASE sp1", "END"}
	no := []string{"SELECT 1", "BEGINNING", "INSERT INTO t VALUES (1)", "CREATE TABLE t(a)"}
	for _, s := range yes {
		if !IsTransactionControl(s) {
			t.Errorf("IsTransactionControl(%q) 应为 true", s)
		}
	}
	for _, s := range no {
		if IsTransactionControl(s) {
			t.Errorf("IsTransactionControl(%q) 应为 false", s)
		}
	}
}
