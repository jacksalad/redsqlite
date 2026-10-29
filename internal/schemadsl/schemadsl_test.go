package schemadsl

import (
	"strings"
	"testing"

	"github.com/redjack/redsqlite/domain"
)

func TestBuildAlter(t *testing.T) {
	cases := []struct {
		name    string
		op      domain.TableOp
		want    string
		wantErr string
	}{
		{
			name: "改表名",
			op:   domain.TableOp{Kind: "renameTable", Table: "users", NewName: "members"},
			want: `ALTER TABLE "users" RENAME TO "members"`,
		},
		{
			name: "改列名",
			op:   domain.TableOp{Kind: "renameColumn", Table: "users", Column: "name", NewName: "nickname"},
			want: `ALTER TABLE "users" RENAME COLUMN "name" TO "nickname"`,
		},
		{
			name: "删列",
			op:   domain.TableOp{Kind: "dropColumn", Table: "users", Column: "score"},
			want: `ALTER TABLE "users" DROP COLUMN "score"`,
		},
		{
			name: "删表",
			op:   domain.TableOp{Kind: "dropTable", Table: "logs"},
			want: `DROP TABLE "logs"`,
		},
		{
			name: "加列",
			op: domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
				Name: "age", Type: "INTEGER", Default: "0",
			}},
			want: `ALTER TABLE "users" ADD COLUMN "age" INTEGER DEFAULT 0`,
		},
		{
			name: "加列 NOT NULL 无默认值",
			op: domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
				Name: "age", Type: "INTEGER", NotNull: true,
			}},
			wantErr: "非 NULL 默认值",
		},
		{
			name: "加列表达式默认值",
			op: domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
				Name: "age", Type: "INTEGER", Default: "CURRENT_TIMESTAMP", DefaultIsExpression: true,
			}},
			wantErr: "表达式默认值",
		},
		{
			name: "加列带主键",
			op: domain.TableOp{Kind: "addColumn", Table: "users", ColumnSpec: &domain.ColumnSpec{
				Name: "id2", Type: "INTEGER", PrimaryKey: true,
			}},
			wantErr: "主键",
		},
		{
			name:    "未知操作",
			op:      domain.TableOp{Kind: "nope", Table: "users"},
			wantErr: "不支持的操作类型",
		},
		{
			name:    "非法表名",
			op:      domain.TableOp{Kind: "dropTable", Table: `u"x`},
			wantErr: "表名非法",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildAlter(tc.op)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("期望错误包含 %q，实际: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if got != tc.want {
				t.Fatalf("SQL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildCreateTable(t *testing.T) {
	cases := []struct {
		name     string
		spec     domain.CreateTableSpec
		contains []string
		wantErr  string
	}{
		{
			name: "单 INTEGER 主键自增",
			spec: domain.CreateTableSpec{
				Table: "t1",
				Columns: []domain.ColumnSpec{
					{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
					{Name: "name", Type: "TEXT", NotNull: true},
				},
			},
			contains: []string{`CREATE TABLE "t1"`, `"id" INTEGER PRIMARY KEY AUTOINCREMENT`, `"name" TEXT NOT NULL`},
		},
		{
			name: "复合主键表级渲染",
			spec: domain.CreateTableSpec{
				Table: "t2",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "TEXT", PrimaryKey: true},
					{Name: "b", Type: "INTEGER", PrimaryKey: true},
				},
			},
			contains: []string{`PRIMARY KEY ("a", "b")`},
		},
		{
			name: "表达式默认值带括号",
			spec: domain.CreateTableSpec{
				Table: "t3",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INTEGER", Default: "1+2", DefaultIsExpression: true},
					{Name: "b", Type: "TEXT", Default: "'x'"},
					{Name: "c", Type: "TEXT", Default: "CURRENT_TIMESTAMP", DefaultIsExpression: true},
				},
			},
			contains: []string{"DEFAULT (1+2)", `DEFAULT 'x'`, "DEFAULT CURRENT_TIMESTAMP"},
		},
		{
			name: "无类型列",
			spec: domain.CreateTableSpec{
				Table:   "t4",
				Columns: []domain.ColumnSpec{{Name: "v"}},
			},
			contains: []string{`"v"`},
		},
		{
			name: "字段重复",
			spec: domain.CreateTableSpec{
				Table:   "t5",
				Columns: []domain.ColumnSpec{{Name: "a"}, {Name: "a"}},
			},
			wantErr: "字段名重复",
		},
		{
			name: "非法默认值",
			spec: domain.CreateTableSpec{
				Table:   "t6",
				Columns: []domain.ColumnSpec{{Name: "a", Default: "abc"}},
			},
			wantErr: "默认值非法",
		},
		{
			name: "非法类型",
			spec: domain.CreateTableSpec{
				Table:   "t7",
				Columns: []domain.ColumnSpec{{Name: "a", Type: "TEXT; DROP TABLE x"}},
			},
			wantErr: "类型非法",
		},
		{
			name: "表达式含分号",
			spec: domain.CreateTableSpec{
				Table:   "t8",
				Columns: []domain.ColumnSpec{{Name: "a", Default: "1); DROP TABLE x; --", DefaultIsExpression: true}},
			},
			wantErr: "分号",
		},
		{
			name: "AUTOINCREMENT 需 INTEGER",
			spec: domain.CreateTableSpec{
				Table:   "t9",
				Columns: []domain.ColumnSpec{{Name: "id", Type: "BIGINT", PrimaryKey: true, AutoIncrement: true}},
			},
			wantErr: "AUTOINCREMENT",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildCreateTable(tc.spec)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("期望错误包含 %q，实际: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("生成的 SQL 缺少 %q:\n%s", sub, got)
				}
			}
		})
	}
}

// 表达式默认值/生成列/类型名的"提前闭括号"与顶层逗号注入防护
// （V3.1 审计加固：无法逃出 DEFAULT(...) / GENERATED ALWAYS AS(...) 包裹）。
func TestBuildCreateTableConstraintInjection(t *testing.T) {
	reject := []struct {
		name    string
		spec    domain.CreateTableSpec
		wantErr string
	}{
		{
			name: "表达式默认值提前闭括号注入 CHECK",
			spec: domain.CreateTableSpec{
				Table: "t1",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INT", Default: "1) , CHECK (a > 999)", DefaultIsExpression: true},
					{Name: "b", Type: "TEXT"},
				},
			},
			wantErr: "括号不配对",
		},
		{
			name: "闭后再开维持总数配平的注入仍在首个右括号被拒",
			spec: domain.CreateTableSpec{
				Table: "t2",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INT", Default: "1), CHECK (a > 1), (", DefaultIsExpression: true},
				},
			},
			wantErr: "括号不配对",
		},
		{
			name: "生成列表达式注入外键",
			spec: domain.CreateTableSpec{
				Table: "t3",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INT"},
					{Name: "b", Generated: "a * 2) , FOREIGN KEY (a) REFERENCES logs(rowid) ON DELETE CASCADE , ("},
				},
			},
			wantErr: "括号不配对",
		},
		{
			name: "表达式顶层逗号",
			spec: domain.CreateTableSpec{
				Table: "t4",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INT", Default: "1 + 2, 3", DefaultIsExpression: true},
				},
			},
			wantErr: "逗号",
		},
		{
			name: "类型名逗号注入约束",
			spec: domain.CreateTableSpec{
				Table:   "t5",
				Columns: []domain.ColumnSpec{{Name: "a", Type: "INT, UNIQUE"}},
			},
			wantErr: "逗号",
		},
		{
			name: "类型名闭括号注入约束",
			spec: domain.CreateTableSpec{
				Table:   "t6",
				Columns: []domain.ColumnSpec{{Name: "a", Type: "INT), CHECK (a)"}},
			},
			wantErr: "括号不配对",
		},
		{
			name: "类型名括号不配平",
			spec: domain.CreateTableSpec{
				Table:   "t7",
				Columns: []domain.ColumnSpec{{Name: "a", Type: "VARCHAR(10"}},
			},
			wantErr: "括号不配对",
		},
	}
	for _, tc := range reject {
		t.Run("拒绝/"+tc.name, func(t *testing.T) {
			_, err := BuildCreateTable(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("期望错误包含 %q，实际: %v", tc.wantErr, err)
			}
		})
	}

	accept := []domain.CreateTableSpec{
		{Table: "ok1", Columns: []domain.ColumnSpec{{Name: "a", Type: "DECIMAL(10,5)"}}},
		{Table: "ok2", Columns: []domain.ColumnSpec{{Name: "a", Type: "UNSIGNED BIG INT"}}},
		{Table: "ok3", Columns: []domain.ColumnSpec{{Name: "a", Type: "TEXT", Default: "substr('ab', 1, 2)", DefaultIsExpression: true}}},
		{Table: "ok4", Columns: []domain.ColumnSpec{{Name: "a", Type: "TEXT", Default: "'a)b'", DefaultIsExpression: true}}},
		{Table: "ok5", Columns: []domain.ColumnSpec{
			{Name: "a", Type: "TEXT"},
			{Name: "b", Generated: "coalesce(a, 'it''s, (x)')"},
		}},
		{Table: "ok6", Columns: []domain.ColumnSpec{{Name: "a", Type: "INT", Default: "(a + 1) * 2", DefaultIsExpression: true}}},
	}
	for _, spec := range accept {
		t.Run("通过/"+spec.Table, func(t *testing.T) {
			if _, err := BuildCreateTable(spec); err != nil {
				t.Fatalf("合法输入不应报错: %v", err)
			}
		})
	}
}

func TestParseLiteral(t *testing.T) {
	ok := []string{"0", "-1", "3.14", "1e3", "0x1F", "NULL", "null", "true", "'abc'", "'it''s'", "x'616263'", "X''"}
	for _, s := range ok {
		if _, err := ParseLiteral(s); err != nil {
			t.Errorf("ParseLiteral(%q) 不应报错: %v", s, err)
		}
	}
	bad := []string{"", "abc", "'unterminated", "x'zz'", "1; DROP TABLE x", "'a' 'b'", "current_timestamp"}
	for _, s := range bad {
		if _, err := ParseLiteral(s); err == nil {
			t.Errorf("ParseLiteral(%q) 应报错", s)
		}
	}
}

func TestBuildCreateTableV23(t *testing.T) {
	cases := []struct {
		name     string
		spec     domain.CreateTableSpec
		contains []string
		wantErr  string
	}{
		{
			name: "虚拟生成列",
			spec: domain.CreateTableSpec{
				Table: "t1",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "INTEGER"},
					{Name: "b", Type: "INTEGER", Generated: "a * 2"},
				},
			},
			contains: []string{`"b" INTEGER GENERATED ALWAYS AS (a * 2)`},
		},
		{
			name: "存储生成列 + NOT NULL",
			spec: domain.CreateTableSpec{
				Table: "t2",
				Columns: []domain.ColumnSpec{
					{Name: "a", Type: "TEXT"},
					{Name: "b", Type: "TEXT", NotNull: true, Generated: "a || '!'", GeneratedStored: true},
				},
			},
			contains: []string{`"b" TEXT NOT NULL GENERATED ALWAYS AS (a || '!') STORED`},
		},
		{
			name: "生成列带默认值",
			spec: domain.CreateTableSpec{
				Table: "t3",
				Columns: []domain.ColumnSpec{
					{Name: "b", Generated: "1", Default: "0"},
				},
			},
			wantErr: "不能有默认值",
		},
		{
			name: "外键子句",
			spec: domain.CreateTableSpec{
				Table: "orders",
				Columns: []domain.ColumnSpec{
					{Name: "id", Type: "INTEGER", PrimaryKey: true},
					{Name: "user_id", Type: "INTEGER"},
				},
				ForeignKeys: []domain.ForeignKeySpec{
					{Column: "user_id", RefTable: "users", RefColumn: "id", OnDelete: "cascade", OnUpdate: "RESTRICT"},
				},
			},
			contains: []string{`FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE ON UPDATE RESTRICT`},
		},
		{
			name: "外键引用主键（省略列）",
			spec: domain.CreateTableSpec{
				Table: "t4",
				Columns: []domain.ColumnSpec{
					{Name: "uid", Type: "INTEGER"},
				},
				ForeignKeys: []domain.ForeignKeySpec{
					{Column: "uid", RefTable: "users"},
				},
			},
			contains: []string{`FOREIGN KEY ("uid") REFERENCES "users"`},
		},
		{
			name: "外键列未定义",
			spec: domain.CreateTableSpec{
				Table:   "t5",
				Columns: []domain.ColumnSpec{{Name: "a"}},
				ForeignKeys: []domain.ForeignKeySpec{
					{Column: "missing", RefTable: "users"},
				},
			},
			wantErr: "未定义",
		},
		{
			name: "外键动作非法",
			spec: domain.CreateTableSpec{
				Table:   "t6",
				Columns: []domain.ColumnSpec{{Name: "a"}},
				ForeignKeys: []domain.ForeignKeySpec{
					{Column: "a", RefTable: "users", OnDelete: "DROP"},
				},
			},
			wantErr: "动作",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildCreateTable(tc.spec)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("期望错误包含 %q，实际: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("生成的 SQL 缺少 %q:\n%s", sub, got)
				}
			}
		})
	}
}

func TestBuildCreateIndex(t *testing.T) {
	sqlText, err := BuildCreateIndex(domain.CreateIndexSpec{
		Table: "users", Name: "idx_users_name", Unique: true, Columns: []string{"name", "score"},
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	want := `CREATE UNIQUE INDEX "idx_users_name" ON "users" ("name", "score")`
	if sqlText != want {
		t.Fatalf("SQL = %q, want %q", sqlText, want)
	}

	bad := []domain.CreateIndexSpec{
		{Table: "t", Name: "", Columns: []string{"a"}},       // 空名
		{Table: "t", Name: "i", Columns: nil},                // 无列
		{Table: "t", Name: "i", Columns: []string{"a", "a"}}, // 重复列
		{Table: "t", Name: `i"x`, Columns: []string{"a"}},    // 非法名
		{Table: `t;x`, Name: "i", Columns: []string{"a"}},    // 非法表名
	}
	for _, spec := range bad {
		if _, err := BuildCreateIndex(spec); err == nil {
			t.Errorf("spec %+v 应报错", spec)
		}
	}
}
