// Package schemadsl 生成表结构 DDL（ALTER TABLE / CREATE TABLE）。
// SQL 一律由后端生成、前端只展示（不变量 3：标识符经 identifier 校验引用，
// 值类输入按字面量/表达式白名单校验，杜绝拼接注入）。
package schemadsl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
)

// 默认值表达式长度与类型名长度上限，防止超长输入。
const (
	maxExprLen  = 512
	maxTypeLen  = 64
	maxIdentLen = 128
)

// BuildAlter 生成单条 ALTER TABLE 语句。仅支持 SQLite 原生能力：
// RENAME TABLE / RENAME COLUMN / ADD COLUMN / DROP COLUMN / DROP TABLE。
func BuildAlter(op domain.TableOp) (string, error) {
	if len(op.Table) > maxIdentLen || len(op.NewName) > maxIdentLen || len(op.Column) > maxIdentLen {
		return "", fmt.Errorf("名称过长")
	}
	table, err := identifier.ValidateAndQuote(op.Table)
	if err != nil {
		return "", fmt.Errorf("表名非法: %w", err)
	}

	switch domain.TableOpKind(op.Kind) {
	case domain.OpRenameTable:
		newName, err := identifier.ValidateAndQuote(op.NewName)
		if err != nil {
			return "", fmt.Errorf("新表名非法: %w", err)
		}
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", table, newName), nil

	case domain.OpRenameColumn:
		if op.Column == "" || op.NewName == "" {
			return "", fmt.Errorf("列名不能为空")
		}
		col, err := identifier.ValidateAndQuote(op.Column)
		if err != nil {
			return "", fmt.Errorf("列名非法: %w", err)
		}
		newName, err := identifier.ValidateAndQuote(op.NewName)
		if err != nil {
			return "", fmt.Errorf("新列名非法: %w", err)
		}
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, col, newName), nil

	case domain.OpDropColumn:
		if op.Column == "" {
			return "", fmt.Errorf("列名不能为空")
		}
		col, err := identifier.ValidateAndQuote(op.Column)
		if err != nil {
			return "", fmt.Errorf("列名非法: %w", err)
		}
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, col), nil

	case domain.OpAddColumn:
		if op.ColumnSpec == nil {
			return "", fmt.Errorf("缺少列定义")
		}
		if err := validateColumnSpec(*op.ColumnSpec, true); err != nil {
			return "", err
		}
		def, err := renderColumnDef(*op.ColumnSpec, false)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, def), nil

	case domain.OpDropTable:
		return fmt.Sprintf("DROP TABLE %s", table), nil

	default:
		return "", fmt.Errorf("不支持的操作类型: %s", op.Kind)
	}
}

// BuildCreateTable 生成 CREATE TABLE 语句。
// 单列 INTEGER 主键渲染为列级 PRIMARY KEY [AUTOINCREMENT]；
// 复合主键渲染为表级 PRIMARY KEY (...)；外键渲染为表级 FOREIGN KEY 子句（V2.3）。
func BuildCreateTable(spec domain.CreateTableSpec) (string, error) {
	if len(spec.Table) > maxIdentLen {
		return "", fmt.Errorf("表名过长")
	}
	table, err := identifier.ValidateAndQuote(spec.Table)
	if err != nil {
		return "", fmt.Errorf("表名非法: %w", err)
	}
	if len(spec.Columns) == 0 {
		return "", fmt.Errorf("至少需要定义一个字段")
	}

	seen := map[string]bool{}
	pkCols := []domain.ColumnSpec{}
	for _, c := range spec.Columns {
		if len(c.Name) > maxIdentLen {
			return "", fmt.Errorf("字段名过长: %s", c.Name)
		}
		if err := validateColumnSpec(c, false); err != nil {
			return "", err
		}
		if seen[c.Name] {
			return "", fmt.Errorf("字段名重复: %s", c.Name)
		}
		seen[c.Name] = true
		if c.PrimaryKey {
			pkCols = append(pkCols, c)
		}
	}
	if len(pkCols) > 1 {
		for _, c := range pkCols {
			if c.AutoIncrement {
				return "", fmt.Errorf("AUTOINCREMENT 只能用于单个 INTEGER 主键")
			}
		}
	}
	if len(pkCols) == 1 && pkCols[0].AutoIncrement && !strings.EqualFold(strings.TrimSpace(pkCols[0].Type), "INTEGER") {
		return "", fmt.Errorf("AUTOINCREMENT 只能用于 INTEGER 类型的主键")
	}

	// 仅精确 INTEGER 主键渲染为列级（rowid 别名）；INT 等走表级，同样合法
	singleIntPK := len(pkCols) == 1 && strings.EqualFold(strings.TrimSpace(pkCols[0].Type), "INTEGER")
	defs := make([]string, 0, len(spec.Columns)+1+len(spec.ForeignKeys))
	for _, c := range spec.Columns {
		def, err := renderColumnDef(c, singleIntPK && c.PrimaryKey)
		if err != nil {
			return "", err
		}
		defs = append(defs, def)
	}
	if len(pkCols) > 1 {
		quoted := make([]string, len(pkCols))
		for i, c := range pkCols {
			quoted[i], _ = identifier.ValidateAndQuote(c.Name)
		}
		defs = append(defs, "PRIMARY KEY ("+strings.Join(quoted, ", ")+")")
	}
	for _, fk := range spec.ForeignKeys {
		def, err := renderForeignKey(fk, seen)
		if err != nil {
			return "", err
		}
		defs = append(defs, def)
	}

	sqlText := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", table, strings.Join(defs, ",\n  "))
	if spec.WithoutRowid {
		sqlText += " WITHOUT ROWID"
	}
	return sqlText, nil
}

// BuildCreateIndex 生成 CREATE INDEX 语句（V2.3）。
func BuildCreateIndex(spec domain.CreateIndexSpec) (string, error) {
	if len(spec.Table) > maxIdentLen || len(spec.Name) > maxIdentLen {
		return "", fmt.Errorf("名称过长")
	}
	table, err := identifier.ValidateAndQuote(spec.Table)
	if err != nil {
		return "", fmt.Errorf("表名非法: %w", err)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return "", fmt.Errorf("索引名不能为空（留空由服务端自动命名）")
	}
	name, err := identifier.ValidateAndQuote(spec.Name)
	if err != nil {
		return "", fmt.Errorf("索引名非法: %w", err)
	}
	if len(spec.Columns) == 0 {
		return "", fmt.Errorf("至少选择一个索引列")
	}
	seenCol := map[string]bool{}
	quoted := make([]string, 0, len(spec.Columns))
	for _, col := range spec.Columns {
		if len(col) > maxIdentLen {
			return "", fmt.Errorf("列名过长")
		}
		q, err := identifier.ValidateAndQuote(col)
		if err != nil {
			return "", fmt.Errorf("索引列非法: %w", err)
		}
		if seenCol[col] {
			return "", fmt.Errorf("索引列重复: %s", col)
		}
		seenCol[col] = true
		quoted = append(quoted, q)
	}
	unique := ""
	if spec.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, name, table, strings.Join(quoted, ", ")), nil
}

// fkActions 外键动作白名单（V2.3）。
var fkActions = map[string]string{
	"":            "",
	"NO ACTION":   "NO ACTION",
	"RESTRICT":    "RESTRICT",
	"SET NULL":    "SET NULL",
	"SET DEFAULT": "SET DEFAULT",
	"CASCADE":     "CASCADE",
}

// normalizeFKAction 归一化外键动作（大小写不敏感），非法值报错。
func normalizeFKAction(action string) (string, error) {
	key := strings.ToUpper(strings.TrimSpace(action))
	v, ok := fkActions[key]
	if !ok {
		return "", fmt.Errorf("外键动作 %q 非法（支持 NO ACTION/RESTRICT/SET NULL/SET DEFAULT/CASCADE）", action)
	}
	return v, nil
}

// renderForeignKey 渲染表级 FOREIGN KEY 子句；columnNames 为已定义列名集合。
// Columns 非空时渲染复合外键（V3.0 表重建）。
func renderForeignKey(fk domain.ForeignKeySpec, columnNames map[string]bool) (string, error) {
	if len(fk.Columns) > 0 {
		return renderCompositeForeignKey(fk, columnNames)
	}
	if len(fk.Column) > maxIdentLen || len(fk.RefTable) > maxIdentLen || len(fk.RefColumn) > maxIdentLen {
		return "", fmt.Errorf("外键定义名称过长")
	}
	if !columnNames[fk.Column] {
		return "", fmt.Errorf("外键引用的本表列 %q 未定义", fk.Column)
	}
	col, err := identifier.ValidateAndQuote(fk.Column)
	if err != nil {
		return "", fmt.Errorf("外键列非法: %w", err)
	}
	refTable, err := identifier.ValidateAndQuote(fk.RefTable)
	if err != nil {
		return "", fmt.Errorf("外键引用表非法: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "FOREIGN KEY (%s) REFERENCES %s", col, refTable)
	if refCol := strings.TrimSpace(fk.RefColumn); refCol != "" {
		q, err := identifier.ValidateAndQuote(refCol)
		if err != nil {
			return "", fmt.Errorf("外键引用列非法: %w", err)
		}
		b.WriteString(" (" + q + ")")
	}
	if err := appendFKActions(&b, fk); err != nil {
		return "", err
	}
	return b.String(), nil
}

// renderCompositeForeignKey 渲染多列复合外键（V3.0 表重建）。
// RefColumns 任一为空则省略被引用列清单（引用父表主键）。
func renderCompositeForeignKey(fk domain.ForeignKeySpec, columnNames map[string]bool) (string, error) {
	if len(fk.Columns) == 0 || len(fk.RefColumns) != 0 && len(fk.RefColumns) != len(fk.Columns) {
		return "", fmt.Errorf("复合外键的列数不匹配")
	}
	quoteAll := func(names []string) (string, error) {
		quoted := make([]string, 0, len(names))
		for _, name := range names {
			if len(name) > maxIdentLen {
				return "", fmt.Errorf("外键定义名称过长")
			}
			if !columnNames[name] {
				return "", fmt.Errorf("外键引用的本表列 %q 未定义", name)
			}
			q, err := identifier.ValidateAndQuote(name)
			if err != nil {
				return "", fmt.Errorf("外键列非法: %w", err)
			}
			quoted = append(quoted, q)
		}
		return strings.Join(quoted, ", "), nil
	}
	cols, err := quoteAll(fk.Columns)
	if err != nil {
		return "", err
	}
	refTable, err := identifier.ValidateAndQuote(fk.RefTable)
	if err != nil {
		return "", fmt.Errorf("外键引用表非法: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "FOREIGN KEY (%s) REFERENCES %s", cols, refTable)
	if len(fk.RefColumns) > 0 && strings.Join(fk.RefColumns, "") != "" {
		refCols, err := quoteRefColumns(fk.RefColumns)
		if err != nil {
			return "", err
		}
		b.WriteString(" (" + refCols + ")")
	}
	if err := appendFKActions(&b, fk); err != nil {
		return "", err
	}
	return b.String(), nil
}

// quoteRefColumns 校验并引用被引用列清单（不要求在 columnNames 中，
// 它们属于父表）。
func quoteRefColumns(names []string) (string, error) {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		q, err := identifier.ValidateAndQuote(name)
		if err != nil {
			return "", fmt.Errorf("外键引用列非法: %w", err)
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, ", "), nil
}

// appendFKActions 追加 ON DELETE / ON UPDATE 子句（动作白名单校验）。
func appendFKActions(b *strings.Builder, fk domain.ForeignKeySpec) error {
	if fk.OnDelete != "" {
		action, err := normalizeFKAction(fk.OnDelete)
		if err != nil {
			return fmt.Errorf("ON DELETE %w", err)
		}
		fmt.Fprintf(b, " ON DELETE %s", action)
	}
	if fk.OnUpdate != "" {
		action, err := normalizeFKAction(fk.OnUpdate)
		if err != nil {
			return fmt.Errorf("ON UPDATE %w", err)
		}
		fmt.Fprintf(b, " ON UPDATE %s", action)
	}
	return nil
}

// validateColumnSpec 校验列定义。forAddColumn=true 时附加 SQLite
// 对 ALTER TABLE ADD COLUMN 的限制（无 PK/UNIQUE、无常量以外默认值、
// NOT NULL 必须有非 NULL 默认）。
func validateColumnSpec(c domain.ColumnSpec, forAddColumn bool) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("字段名不能为空")
	}
	if err := identifier.Validate(c.Name); err != nil {
		return fmt.Errorf("字段名 %q 非法: %w", c.Name, err)
	}
	if err := validateTypeName(c.Type); err != nil {
		return fmt.Errorf("字段 %q 的类型非法: %w", c.Name, err)
	}
	generated := strings.TrimSpace(c.Generated)
	if generated != "" {
		if forAddColumn {
			return fmt.Errorf("加列不支持生成列，请新建表实现")
		}
		if err := validateExpression(generated); err != nil {
			return fmt.Errorf("字段 %q 的生成列表达式非法: %w", c.Name, err)
		}
		if strings.TrimSpace(c.Default) != "" {
			return fmt.Errorf("生成列 %q 不能有默认值", c.Name)
		}
		if c.PrimaryKey || c.AutoIncrement || c.Unique {
			return fmt.Errorf("生成列 %q 暂不支持主键/唯一约束", c.Name)
		}
	}
	if forAddColumn {
		if c.PrimaryKey || c.AutoIncrement || c.Unique {
			return fmt.Errorf("SQLite 不支持通过加列添加主键/唯一约束，请新建表实现")
		}
		if c.DefaultIsExpression {
			return fmt.Errorf("SQLite 不允许加列使用表达式默认值（CURRENT_TIMESTAMP 等）")
		}
	}
	def := strings.TrimSpace(c.Default)
	if def != "" {
		if c.DefaultIsExpression {
			if err := validateExpression(def); err != nil {
				return fmt.Errorf("字段 %q 的默认值表达式非法: %w", c.Name, err)
			}
		} else if _, err := ParseLiteral(def); err != nil {
			return fmt.Errorf("字段 %q 的默认值非法: %w", c.Name, err)
		}
	}
	if forAddColumn && c.NotNull {
		if def == "" || strings.EqualFold(def, "NULL") {
			return fmt.Errorf("NOT NULL 字段必须提供非 NULL 默认值")
		}
	}
	if c.AutoIncrement && !c.PrimaryKey {
		return fmt.Errorf("AUTOINCREMENT 只能配合主键使用")
	}
	return nil
}

// renderColumnDef 渲染单个列定义。columnPK=true 时输出列级
// PRIMARY KEY [AUTOINCREMENT]（仅限单 INTEGER 主键场景）；
// 生成列输出 GENERATED ALWAYS AS (expr) [STORED]。
func renderColumnDef(c domain.ColumnSpec, columnPK bool) (string, error) {
	name, err := identifier.ValidateAndQuote(c.Name)
	if err != nil {
		return "", err
	}
	parts := []string{name}
	if t := strings.TrimSpace(c.Type); t != "" {
		parts = append(parts, t)
	}
	if generated := strings.TrimSpace(c.Generated); generated != "" {
		if c.NotNull {
			parts = append(parts, "NOT NULL")
		}
		parts = append(parts, "GENERATED ALWAYS AS ("+generated+")")
		if c.GeneratedStored {
			parts = append(parts, "STORED")
		}
		return strings.Join(parts, " "), nil
	}
	if columnPK {
		pk := "PRIMARY KEY"
		if c.AutoIncrement {
			pk += " AUTOINCREMENT"
		}
		parts = append(parts, pk)
	}
	if c.NotNull && !c.PrimaryKey {
		parts = append(parts, "NOT NULL")
	}
	if c.Unique && !columnPK {
		parts = append(parts, "UNIQUE")
	}
	if def := strings.TrimSpace(c.Default); def != "" {
		if c.DefaultIsExpression {
			parts = append(parts, "DEFAULT "+renderExpression(def))
		} else {
			lit, err := ParseLiteral(def)
			if err != nil {
				return "", err
			}
			parts = append(parts, "DEFAULT "+lit)
		}
	}
	return strings.Join(parts, " "), nil
}

// validateTypeName 类型名仅允许字母/数字/空格与 ( ) , _ .（如 DECIMAL(10,5)、
// UNSIGNED BIG INT），且括号必须配平、逗号只能出现在括号内（类型参数）——
// 防止借类型名向列定义注入表级约束（如 "INT, UNIQUE"、"INT), CHECK (a)"）。
// 空类型合法（SQLite 允许无类型列）。
func validateTypeName(t string) error {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}
	if len(t) > maxTypeLen {
		return fmt.Errorf("类型名过长")
	}
	for _, r := range t {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		case r == ' ' || r == '(' || r == ')' || r == ',' || r == '_' || r == '.':
		default:
			return fmt.Errorf("包含非法字符 %q", r)
		}
	}
	return scanParens(t)
}

// ParseLiteral 校验并原样返回单条 SQLite 字面量：
// NULL / TRUE / FALSE / 整数 / 浮点 / 0x 十六进制 / '文本' / x'十六进制'。
func ParseLiteral(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("默认值为空")
	}
	if len(s) > maxExprLen {
		return "", fmt.Errorf("默认值过长")
	}
	switch strings.ToUpper(s) {
	case "NULL", "TRUE", "FALSE":
		return strings.ToUpper(s), nil
	}
	if strings.HasPrefix(s, "'") {
		if _, err := unquoteString(s); err != nil {
			return "", err
		}
		return s, nil
	}
	if len(s) >= 3 && (s[0] == 'x' || s[0] == 'X') && s[1] == '\'' && strings.HasSuffix(s, "'") {
		hex := s[2 : len(s)-1] // x'' 为合法的零长度 BLOB
		for _, r := range hex {
			if !isHexRune(r) {
				return "", fmt.Errorf("BLOB 字面量包含非十六进制字符")
			}
		}
		return s, nil
	}
	if _, err := strconv.ParseInt(s, 0, 64); err == nil {
		return s, nil // 含 0x 十六进制整数
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s, nil
	}
	return "", fmt.Errorf("只支持数字、'文本'、x'十六进制'、NULL 等字面量")
}

// unquoteString 校验 '...' 形式的字符串字面量（内部双写单引号转义）。
func unquoteString(s string) (string, error) {
	if len(s) < 2 || !strings.HasPrefix(s, "'") || !strings.HasSuffix(s, "'") {
		return "", fmt.Errorf("文本字面量必须以单引号包围")
	}
	inner := s[1 : len(s)-1]
	if strings.ContainsRune(inner, '\x00') {
		return "", fmt.Errorf("文本字面量包含非法字符")
	}
	// 未转义的单引号（结尾配对以外）非法
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\'' {
			if i+1 < len(inner) && inner[i+1] == '\'' {
				i++
				continue
			}
			return "", fmt.Errorf("文本字面量中的单引号需双写转义（如 'it''s'）")
		}
	}
	return inner, nil
}

// renderExpression 渲染表达式默认值：时间关键字裸输出，其余包裹括号。
// 表达式已通过 validateExpression（拒绝分号与注释，防语句拆分注入）。
func renderExpression(expr string) string {
	switch strings.ToUpper(expr) {
	case "CURRENT_TIME", "CURRENT_DATE", "CURRENT_TIMESTAMP":
		return strings.ToUpper(expr)
	}
	return "(" + expr + ")"
}

// validateExpression 表达式默认值仅拒绝可能拆分语句/破坏 DDL 结构的内容：
// 分号、注释起始、引号外的 NUL（防语句拆分注入），以及括号不配平或顶层
// 逗号（防提前闭合 DEFAULT(...) / GENERATED ALWAYS AS(...) 的包裹括号、
// 向 CREATE TABLE 注入表级约束），其余交给 SQLite 解析器裁决。
func validateExpression(expr string) error {
	if expr == "" {
		return fmt.Errorf("表达式为空")
	}
	if len(expr) > maxExprLen {
		return fmt.Errorf("表达式过长")
	}
	if strings.Contains(expr, ";") {
		return fmt.Errorf("表达式不能包含分号")
	}
	if strings.Contains(expr, "--") || strings.Contains(expr, "/*") {
		return fmt.Errorf("表达式不能包含注释")
	}
	if strings.ContainsRune(expr, '\x00') {
		return fmt.Errorf("表达式包含非法字符")
	}
	return scanParens(expr)
}

// scanParens 检查括号配平与顶层逗号：'...'（双写转义）、"..."、`...`
// 引号区内的括号与逗号不计数（'a)b'、"col" 等合法内容不受影响）。
// 右括号提前出现（深度为负）、结尾深度非零、顶层逗号或引号区未闭合
// 都判定非法——表达式/类型名最终会被包进 DEFAULT(...)、
// GENERATED ALWAYS AS(...) 或列定义里，配平保证无法逃出包裹结构。
func scanParens(s string) error {
	depth := 0
	var quote byte // 0 = 不在引号区，否则为 ' " ` 之一
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				if i+1 < len(s) && s[i+1] == quote {
					i++ // 引号双写转义（如 'it''s'）
					continue
				}
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("括号不配对")
			}
		case ',':
			if depth == 0 {
				return fmt.Errorf("括号外不允许逗号")
			}
		}
	}
	if quote != 0 {
		return fmt.Errorf("引号未闭合")
	}
	if depth != 0 {
		return fmt.Errorf("括号不配对")
	}
	return nil
}

func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
