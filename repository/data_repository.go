package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/cell"
	"github.com/redjack/redsqlite/internal/identifier"
)

// DataRepository 表数据的分页浏览与编辑（文档 6.3 / 6.4）。
// V2.1：绑定单个连接（connID），内部自带同连接的结构仓库。
type DataRepository struct {
	store  *Store
	connID string
	schema *SchemaRepository
}

// NewDataRepository 创建绑定到指定连接的数据仓库。
func NewDataRepository(store *Store, connID string) *DataRepository {
	return &DataRepository{
		store:  store,
		connID: connID,
		schema: NewSchemaRepository(store, connID),
	}
}

// KeyColumns 返回行定位列：优先主键，其次 rowid；不可编辑时返回 nil。
// 返回 (keyColumns, editable, err)。
func (r *DataRepository) KeyColumns(table string) ([]string, bool, error) {
	isView, err := r.schema.IsView(table)
	if err != nil {
		return nil, false, err
	}
	if isView {
		return nil, false, nil
	}

	columns, err := r.schema.GetTableColumns(table)
	if err != nil {
		return nil, false, err
	}

	var pk []string
	for _, c := range columns {
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}

	if len(pk) > 0 {
		return pk, true, nil
	}

	// 无主键：rowid 表可用 rowid 定位（文档 6.4）
	if r.schema.HasRowidColumn(table) {
		return []string{identifier.RowidName}, true, nil
	}
	return nil, false, nil
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// GetTableData 分页读取表数据。
// 存在唯一游标列（rowid 或单列主键）时使用游标分页（深翻页代价与表规模无关）：
// 默认顺序按游标列单列定位；排序浏览（V3.0）按 排序列+游标列 复合排序，
// 用 row-value 比较 (a,b) > (?,?) 定位（DESC 时方向翻转）；排序列可空且
// 实际存在 NULL 时回退 LIMIT/OFFSET（row-value 比较对 NULL 不成立，会丢页）。
// 视图/复合主键等场景回退 LIMIT/OFFSET。两种模式都多取一行以判断 HasNext。
// 行数总数由 CountFiltered 异步获取，不在这里执行 COUNT(*)（V1.1 性能目标）。
func (r *DataRepository) GetTableData(ctx context.Context, req domain.TableDataRequest) (domain.TableData, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return domain.TableData{}, err
	}

	quotedTable, err := identifier.ValidateAndQuote(req.Table)
	if err != nil {
		return domain.TableData{}, fmt.Errorf("表名非法: %w", err)
	}

	columns, err := r.schema.GetTableColumns(req.Table)
	if err != nil {
		return domain.TableData{}, err
	}
	columnNames := make([]string, 0, len(columns))
	for _, c := range columns {
		columnNames = append(columnNames, c.Name)
	}

	keys, editable, err := r.KeyColumns(req.Table)
	if err != nil {
		return domain.TableData{}, err
	}

	useRowid := editable && len(keys) == 1 && keys[0] == identifier.RowidName

	// 游标列判定（V3.0 扩展到排序浏览）：rowid 表用 rowid，单列主键表
	//（含 WITHOUT ROWID）用主键；视图/复合主键无唯一游标列 → 回退 OFFSET。
	var cursorCol, cursorAlias string
	if useRowid {
		cursorCol, cursorAlias = "rowid", identifier.RowidName
	} else if editable && len(keys) == 1 {
		if quoted, qerr := identifier.ValidateAndQuote(keys[0]); qerr == nil {
			cursorCol, cursorAlias = quoted, keys[0]
		}
	}

	// ---------- 游标模式决策 ----------
	// 默认顺序：单列游标。
	// 排序浏览：排序列 + 游标列复合排序，用 row-value 比较 (a,b) > (?,?)
	// 做游标（DESC 时比较方向同步翻转）；排序列即游标列时退化为单列游标。
	// 排序列可空且存在 NULL 时回退 OFFSET：row-value 比较对 NULL 不成立，
	// NULL 行会丢页（SQLite NULL 排在 ASC 首位/DESC 末位）。
	sortCol := strings.TrimSpace(req.OrderBy)
	cursorMode := cursorCol != ""
	compositeCursor := false
	sortQuoted := ""
	dir := "ASC"
	cmpOp := ">"
	if cursorMode && sortCol != "" {
		if req.OrderDesc {
			dir, cmpOp = "DESC", "<"
		}
		if (sortCol == identifier.RowidName && cursorCol == "rowid") ||
			(cursorCol != "rowid" && sortCol == cursorAlias) {
			// 排序列即游标列：等价默认顺序（方向相反即可）
		} else {
			nullable := false
			found := false
			for _, c := range columns {
				if c.Name == sortCol {
					found, nullable = true, !c.NotNull
					break
				}
			}
			if !found {
				return domain.TableData{}, fmt.Errorf("排序列 %q 不存在", sortCol)
			}
			q, qerr := identifier.ValidateAndQuote(sortCol)
			if qerr != nil {
				return domain.TableData{}, fmt.Errorf("排序列名非法: %w", qerr)
			}
			if !nullable || !r.sortColumnHasNulls(ctx, quotedTable, q) {
				sortQuoted = q
				compositeCursor = true
			} else {
				cursorMode = false // 存在 NULL，回退 OFFSET
			}
		}
	}

	// 组装 SELECT
	var sb strings.Builder
	var args []any
	if useRowid {
		sb.WriteString(fmt.Sprintf("SELECT rowid AS %s, * FROM %s", identifier.Quote(identifier.RowidName), quotedTable))
	} else {
		sb.WriteString(fmt.Sprintf("SELECT * FROM %s", quotedTable))
	}

	var where []string
	filterSQL, filterArgs, err := filterCondition(columnNames, req.Filter)
	if err != nil {
		return domain.TableData{}, err
	}
	if filterSQL != "" {
		where = append(where, "("+filterSQL+")")
		args = append(args, filterArgs...)
	}
	condSQL, condArgs, err := buildFilterConditions(req.Conditions, columnNames)
	if err != nil {
		return domain.TableData{}, err
	}
	if condSQL != "" {
		where = append(where, "("+condSQL+")")
		args = append(args, condArgs...)
	}

	if cursorMode && req.Cursor != "" {
		if compositeCursor {
			// 复合游标：JSON 数组 [排序值, 游标值]
			sortVal, curVal, cerr := parseCursorPair(req.Cursor)
			if cerr != nil {
				return domain.TableData{}, cerr
			}
			typed, terr := parseCursorValue(cursorCol == "rowid", curVal)
			if terr != nil {
				return domain.TableData{}, terr
			}
			where = append(where, fmt.Sprintf("(%s, %s) %s (?, ?)", sortQuoted, cursorCol, cmpOp))
			args = append(args, sortVal, typed)
		} else {
			value, cerr := parseCursorValue(cursorCol == "rowid", req.Cursor)
			if cerr != nil {
				return domain.TableData{}, cerr
			}
			op := ">"
			if sortCol != "" {
				op = cmpOp
			}
			where = append(where, fmt.Sprintf("%s %s ?", cursorCol, op))
			args = append(args, value)
		}
	}
	if len(where) > 0 {
		sb.WriteString(" WHERE " + strings.Join(where, " AND "))
	}

	if cursorMode {
		switch {
		case compositeCursor:
			sb.WriteString(fmt.Sprintf(" ORDER BY %s %s, %s %s", sortQuoted, dir, cursorCol, dir))
		case sortCol != "":
			sb.WriteString(" ORDER BY " + cursorCol + " " + dir)
		default:
			sb.WriteString(" ORDER BY " + cursorCol + " ASC")
		}
	} else if err := appendOrderBy(&sb, req.OrderBy, req.OrderDesc, columnNames, useRowid); err != nil {
		return domain.TableData{}, err
	}

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 1000 {
		req.PageSize = 100
	}
	// 多取一行判断是否还有下一页
	sb.WriteString(" LIMIT ?")
	args = append(args, req.PageSize+1)
	if !cursorMode {
		sb.WriteString(" OFFSET ?")
		args = append(args, (req.Page-1)*req.PageSize)
	}

	start := now()
	rows, err := db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return domain.TableData{}, fmt.Errorf("查询表数据失败: %w", err)
	}
	result, err := scanRows(rows, 0) // 分页读取无截断
	rows.Close()
	if err != nil {
		return domain.TableData{}, err
	}

	hasNext := len(result.Rows) > req.PageSize
	if hasNext {
		result.Rows = result.Rows[:req.PageSize]
	}

	out := domain.TableData{
		Columns:     result.Columns,
		ColumnTypes: result.ColumnTypes,
		Rows:        result.Rows,
		PageSize:    req.PageSize,
		ElapsedMs:   sinceMs(start),
		Editable:    editable && !r.store.ReadOnly(r.connID),
		KeyColumns:  keys,
	}

	if cursorMode {
		out.CursorMode = true
		out.HasPrev = req.Cursor != ""
		out.HasNext = hasNext
		if n := len(result.Rows); n > 0 {
			idx := indexOf(result.Columns, cursorAlias)
			if idx >= 0 {
				if compositeCursor {
					// 复合游标：[排序值, 游标值] JSON 数组
					idxSort := indexOf(result.Columns, sortCol)
					if idxSort < 0 {
						out.HasNext = false
					} else {
						first := encodeCursorPair(result.Rows[0][idxSort], result.Rows[0][idx])
						last := encodeCursorPair(result.Rows[n-1][idxSort], result.Rows[n-1][idx])
						if first != "" {
							out.PrevCursor = first
						}
						if last != "" {
							out.NextCursor = last
						} else {
							// 排序值不可编码（BLOB/截断标记），无法继续向后翻页
							out.HasNext = false
						}
					}
				} else {
					if first, ok := encodeCursor(result.Rows[0][idx]); ok {
						out.PrevCursor = first
					}
					if last, ok := encodeCursor(result.Rows[n-1][idx]); ok {
						out.NextCursor = last
					} else {
						// 游标值不可编码（如超长文本被截断标记替换），无法继续向后翻页
						out.HasNext = false
					}
				}
			}
		}
	} else {
		out.Page = req.Page
		out.HasPrev = req.Page > 1
		out.HasNext = hasNext
	}
	return out, nil
}

// indexOf 返回列名下标，不存在返回 -1。
func indexOf(names []string, target string) int {
	for i, n := range names {
		if n == target {
			return i
		}
	}
	return -1
}

// encodeCursor 把结果单元格值转为可回传的游标字符串。
// BLOB/截断标记不可作游标（值不完整），返回 false；
// bigint 标记（V3.0）以十进制字符串原样回传。
func encodeCursor(v any) (string, bool) {
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10), true
	case int:
		return strconv.Itoa(t), true
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64), true
	case string:
		return t, true
	case cell.Marker:
		if t.Kind == cell.KindBigint {
			return t.Str, true
		}
		return "", false
	default:
		return fmt.Sprintf("%v", t), true
	}
}

// parseCursorValue 把游标字符串还原为驱动参数。
// rowid 游标必须是整数；主键游标能解析为整数则用整数，否则按原文传入。
func parseCursorValue(isRowid bool, cursor string) (any, error) {
	if n, err := strconv.ParseInt(cursor, 10, 64); err == nil {
		return n, nil
	}
	if isRowid {
		return nil, fmt.Errorf("游标值无效")
	}
	return cursor, nil
}

// encodeCursorPair 把复合游标（排序值 + 游标列值）编码为 JSON 数组字符串；
// 任一值不可编码（BLOB/截断标记）返回空串。
func encodeCursorPair(sortVal, cursorVal any) string {
	s, ok1 := encodeCursor(sortVal)
	c, ok2 := encodeCursor(cursorVal)
	if !ok1 || !ok2 {
		return ""
	}
	b, err := json.Marshal([2]string{s, c})
	if err != nil {
		return ""
	}
	return string(b)
}

// parseCursorPair 解析复合游标 JSON 数组 [排序值, 游标值]。
func parseCursorPair(cursor string) (string, string, error) {
	var pair []string
	if err := json.Unmarshal([]byte(cursor), &pair); err != nil || len(pair) != 2 {
		return "", "", fmt.Errorf("游标值无效")
	}
	return pair[0], pair[1], nil
}

// sortColumnHasNulls 检查排序浏览列是否存在 NULL 行（V3.0）。
// row-value 游标比较对 NULL 不成立（NULL 行会丢页），此时回退 OFFSET。
// 查询失败按"存在 NULL"保守处理。
func (r *DataRepository) sortColumnHasNulls(ctx context.Context, quotedTable, quotedCol string) bool {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return true
	}
	var one int
	err = db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE %s IS NULL LIMIT 1", quotedTable, quotedCol)).Scan(&one)
	return err == nil
}

// CountAll 统计表总行数（不含过滤）。
func (r *DataRepository) CountAll(table string) (int64, error) {
	ctx := context.Background()
	if base, err := r.store.Context(r.connID); err == nil {
		ctx = base
	}
	return r.countRows(ctx, table, nil, "", nil)
}

// CountFiltered 统计行数（可带文本过滤与结构化筛选），供前端翻页后异步调用。
func (r *DataRepository) CountFiltered(ctx context.Context, table string, filter string, conditions []domain.FilterCondition) (int64, error) {
	columns, err := r.schema.GetTableColumns(table)
	if err != nil {
		return 0, err
	}
	columnNames := make([]string, 0, len(columns))
	for _, c := range columns {
		columnNames = append(columnNames, c.Name)
	}
	return r.countRows(ctx, table, columnNames, filter, conditions)
}

func (r *DataRepository) countRows(ctx context.Context, table string, columnNames []string, filter string, conditions []domain.FilterCondition) (int64, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return 0, err
	}
	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return 0, fmt.Errorf("表名非法: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SELECT COUNT(*) FROM %s", quotedTable))
	var args []any
	var where []string
	if filter != "" {
		filterSQL, filterArgs, err := filterCondition(columnNames, filter)
		if err != nil {
			return 0, err
		}
		if filterSQL != "" {
			where = append(where, "("+filterSQL+")")
			args = append(args, filterArgs...)
		}
	}
	condSQL, condArgs, err := buildFilterConditions(conditions, columnNames)
	if err != nil {
		return 0, err
	}
	if condSQL != "" {
		where = append(where, "("+condSQL+")")
		args = append(args, condArgs...)
	}
	if len(where) > 0 {
		sb.WriteString(" WHERE " + strings.Join(where, " AND "))
	}

	var total int64
	if err := db.QueryRowContext(ctx, sb.String(), args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("统计行数失败: %w", err)
	}
	return total, nil
}

// filterCondition 构造跨列文本过滤条件（不含 WHERE 前缀）。
func filterCondition(columnNames []string, filter string) (string, []any, error) {
	if strings.TrimSpace(filter) == "" {
		return "", nil, nil
	}
	var conds []string
	var args []any
	for _, name := range columnNames {
		quoted, err := identifier.ValidateAndQuote(name)
		if err != nil {
			return "", nil, fmt.Errorf("列名非法: %w", err)
		}
		conds = append(conds, fmt.Sprintf("CAST(%s AS TEXT) LIKE ? ESCAPE '\\'", quoted))
		args = append(args, "%"+escapeLike(strings.TrimSpace(filter))+"%")
	}
	if len(conds) == 0 {
		return "", nil, nil
	}
	return strings.Join(conds, " OR "), args, nil
}

// buildFilterConditions 构造结构化筛选条件（V3.0 列筛选构建器）。
// 多条件 AND 组合；列名必须存在于实际列清单（白名单校验，不变量 3），
// 值一律参数绑定；数值列由 SQLite 列亲和性自动转换字符串值。
func buildFilterConditions(conditions []domain.FilterCondition, columnNames []string) (string, []any, error) {
	if len(conditions) == 0 {
		return "", nil, nil
	}
	valid := make(map[string]bool, len(columnNames))
	for _, name := range columnNames {
		valid[name] = true
	}
	var parts []string
	var args []any
	for i, c := range conditions {
		if !valid[c.Column] {
			return "", nil, fmt.Errorf("第 %d 个筛选条件的列 %q 不存在", i+1, c.Column)
		}
		quoted, err := identifier.ValidateAndQuote(c.Column)
		if err != nil {
			return "", nil, fmt.Errorf("第 %d 个筛选条件的列名非法: %w", i+1, err)
		}
		switch c.Op {
		case domain.FilterEq, domain.FilterNe, domain.FilterGt, domain.FilterGe,
			domain.FilterLt, domain.FilterLe:
			op := map[domain.FilterOp]string{
				domain.FilterEq: "=", domain.FilterNe: "<>",
				domain.FilterGt: ">", domain.FilterGe: ">=",
				domain.FilterLt: "<", domain.FilterLe: "<=",
			}[c.Op]
			parts = append(parts, fmt.Sprintf("%s %s ?", quoted, op))
			args = append(args, c.Value)
		case domain.FilterLike:
			parts = append(parts, fmt.Sprintf("%s LIKE ? ESCAPE '\\'", quoted))
			args = append(args, "%"+escapeLike(c.Value)+"%")
		case domain.FilterRange:
			low, high := strings.TrimSpace(c.Value), strings.TrimSpace(c.Value2)
			switch {
			case low != "" && high != "":
				parts = append(parts, fmt.Sprintf("(%s >= ? AND %s <= ?)", quoted, quoted))
				args = append(args, low, high)
			case low != "":
				parts = append(parts, fmt.Sprintf("%s >= ?", quoted))
				args = append(args, low)
			case high != "":
				parts = append(parts, fmt.Sprintf("%s <= ?", quoted))
				args = append(args, high)
			default:
				return "", nil, fmt.Errorf("第 %d 个筛选条件（范围）至少填写一侧边界", i+1)
			}
		case domain.FilterIsNull:
			parts = append(parts, fmt.Sprintf("%s IS NULL", quoted))
		case domain.FilterNotNull:
			parts = append(parts, fmt.Sprintf("%s IS NOT NULL", quoted))
		default:
			return "", nil, fmt.Errorf("第 %d 个筛选条件的操作符 %q 不受支持", i+1, c.Op)
		}
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	return strings.Join(parts, " AND "), args, nil
}

func appendOrderBy(sb *strings.Builder, orderBy string, desc bool, columnNames []string, useRowid bool) error {
	if strings.TrimSpace(orderBy) == "" {
		return nil
	}
	valid := orderBy == identifier.RowidName && useRowid
	if !valid {
		for _, name := range columnNames {
			if name == orderBy {
				valid = true
				break
			}
		}
	}
	if !valid {
		return fmt.Errorf("排序列 %q 不存在", orderBy)
	}
	quoted := identifier.Quote(orderBy)
	if orderBy == identifier.RowidName {
		quoted = "rowid"
	}
	sb.WriteString(" ORDER BY " + quoted)
	if desc {
		sb.WriteString(" DESC")
	}
	return nil
}

// InsertRow 插入一行。values 的键必须是表内真实列名（文档 6.4）。
func (r *DataRepository) InsertRow(ctx context.Context, table string, values map[string]any) (int64, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return 0, err
	}
	if r.store.ReadOnly(r.connID) {
		return 0, fmt.Errorf("数据库以只读方式打开，无法写入")
	}

	columns, err := r.schema.GetTableColumns(table)
	if err != nil {
		return 0, err
	}
	validCols := map[string]bool{}
	for _, c := range columns {
		validCols[c.Name] = true
	}

	var colParts, placeholders []string
	var args []any
	for name, raw := range values {
		if !validCols[name] {
			return 0, fmt.Errorf("列 %q 不存在于表 %q", name, table)
		}
		quoted, err := identifier.ValidateAndQuote(name)
		if err != nil {
			return 0, fmt.Errorf("列名非法: %w", err)
		}
		value, err := cell.Decode(raw)
		if err != nil {
			return 0, fmt.Errorf("列 %q 的值无效: %w", name, err)
		}
		colParts = append(colParts, quoted)
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	if len(colParts) == 0 {
		return 0, fmt.Errorf("没有可插入的字段")
	}

	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return 0, fmt.Errorf("表名非法: %w", err)
	}
	sqlText := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotedTable, strings.Join(colParts, ", "), strings.Join(placeholders, ", "))

	res, err := db.ExecContext(ctx, sqlText, args...)
	if err != nil {
		return 0, fmt.Errorf("插入数据失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// UpdateCell 更新由 key 定位的行的某一列。
// key 形如 {"id": 5} 或 {"__rowid__": 12}（文档 6.4）。
func (r *DataRepository) UpdateCell(ctx context.Context, table string, key map[string]any, column string, value any) (int64, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return 0, err
	}
	if r.store.ReadOnly(r.connID) {
		return 0, fmt.Errorf("数据库以只读方式打开，无法写入")
	}

	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return 0, fmt.Errorf("表名非法: %w", err)
	}

	columns, err := r.schema.GetTableColumns(table)
	if err != nil {
		return 0, err
	}
	colValid := false
	for _, c := range columns {
		if c.Name == column {
			colValid = true
			break
		}
	}
	if !colValid {
		return 0, fmt.Errorf("列 %q 不存在于表 %q", column, table)
	}
	quotedColumn, err := identifier.ValidateAndQuote(column)
	if err != nil {
		return 0, fmt.Errorf("列名非法: %w", err)
	}

	decoded, err := cell.Decode(value)
	if err != nil {
		return 0, fmt.Errorf("单元格值无效: %w", err)
	}

	whereSQL, whereArgs, err := buildRowSelector(key)
	if err != nil {
		return 0, err
	}

	sqlText := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s", quotedTable, quotedColumn, whereSQL)
	res, err := db.ExecContext(ctx, sqlText, append([]any{decoded}, whereArgs...)...)
	if err != nil {
		return 0, fmt.Errorf("更新数据失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return 0, fmt.Errorf("更新数据失败：没有匹配的行（行可能已被修改或删除）")
	}
	return affected, nil
}

// DeleteRows 在事务中删除多行（文档 6.4 / 10.3）。
func (r *DataRepository) DeleteRows(ctx context.Context, table string, keys []map[string]any) (int64, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return 0, err
	}
	if r.store.ReadOnly(r.connID) {
		return 0, fmt.Errorf("数据库以只读方式打开，无法写入")
	}
	if len(keys) == 0 {
		return 0, fmt.Errorf("没有选择要删除的行")
	}

	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return 0, fmt.Errorf("表名非法: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	var total int64
	for i, key := range keys {
		whereSQL, whereArgs, err := buildRowSelector(key)
		if err != nil {
			return 0, fmt.Errorf("第 %d 行定位信息无效: %w", i+1, err)
		}
		res, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", quotedTable, whereSQL), whereArgs...)
		if err != nil {
			return 0, fmt.Errorf("删除数据失败: %w", err)
		}
		n, _ := res.RowsAffected()
		total += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交删除事务失败: %w", err)
	}
	return total, nil
}

// GetCellBlob 读取由 key 定位的行的某一列完整 BLOB 内容（V2.5）。
// 单元格列表里的 BLOB 标记只带 64 KB 预览，查看/另存需要完整数据。
// NULL 或非二进制值返回错误；超过 32 MB 拒绝读取（避免 IPC 传输过大）。
func (r *DataRepository) GetCellBlob(ctx context.Context, table string, key map[string]any, column string) (domain.BinaryContent, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return domain.BinaryContent{}, err
	}

	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return domain.BinaryContent{}, fmt.Errorf("表名非法: %w", err)
	}
	quotedColumn, err := identifier.ValidateAndQuote(column)
	if err != nil {
		return domain.BinaryContent{}, fmt.Errorf("列名非法: %w", err)
	}

	whereSQL, whereArgs, err := buildRowSelector(key)
	if err != nil {
		return domain.BinaryContent{}, err
	}

	// 空长度 BLOB 经驱动扫描会变成 nil []byte，用 IS NULL 区分 NULL 与空 BLOB
	var data []byte
	var isNull int64
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s, (%s IS NULL) FROM %s WHERE %s LIMIT 1",
			quotedColumn, quotedColumn, quotedTable, whereSQL),
		whereArgs...,
	).Scan(&data, &isNull); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BinaryContent{}, fmt.Errorf("没有匹配的行（行可能已被修改或删除）")
		}
		return domain.BinaryContent{}, fmt.Errorf("读取单元格失败: %w", err)
	}
	if isNull == 1 {
		return domain.BinaryContent{}, fmt.Errorf("该单元格不是 BLOB（可能为 NULL）")
	}
	if int64(len(data)) > maxBlobTransferBytes {
		return domain.BinaryContent{}, fmt.Errorf("BLOB 大小超过 %d MB，不支持在线查看/另存", maxBlobTransferBytes/(1<<20))
	}
	return domain.BinaryContent{Size: int64(len(data)), B64: base64.StdEncoding.EncodeToString(data)}, nil
}

// maxBlobTransferBytes 单个 BLOB 在线查看/另存的大小上限。
const maxBlobTransferBytes = int64(32 << 20)

// DuplicateRows 在单个事务中复制选中的行（V2.5）。
// 单列 INTEGER 主键（rowid 别名）不参与复制，让新行取新的自增值；
// 其他形态（TEXT/复合主键、唯一约束）如冲突则整体回滚并报错，
// 提示先改键值。生成列不参与 INSERT SELECT。
func (r *DataRepository) DuplicateRows(ctx context.Context, table string, keys []map[string]any) (int64, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return 0, err
	}
	if r.store.ReadOnly(r.connID) {
		return 0, fmt.Errorf("数据库以只读方式打开，无法写入")
	}
	if len(keys) == 0 {
		return 0, fmt.Errorf("没有选择要复制的行")
	}

	quotedTable, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return 0, fmt.Errorf("表名非法: %w", err)
	}

	columns, err := r.schema.InsertableColumns(table)
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, fmt.Errorf("表没有可复制的普通列")
	}

	// 单列 INTEGER 主键（rowid 别名）排除，复制后取新自增 id
	skipCol := ""
	if ks, editable, err := r.KeyColumns(table); err == nil && editable && len(ks) == 1 &&
		ks[0] != identifier.RowidName && r.schema.HasRowidColumn(table) {
		cols, err := r.schema.GetTableColumns(table)
		if err == nil {
			for _, c := range cols {
				if c.Name == ks[0] && c.PrimaryKey &&
					strings.EqualFold(strings.TrimSpace(c.Type), "INTEGER") {
					skipCol = c.Name
					break
				}
			}
		}
	}

	var copyCols []string
	for _, c := range columns {
		if c != skipCol {
			copyCols = append(copyCols, identifier.Quote(c))
		}
	}
	colList := strings.Join(copyCols, ", ")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	var total int64
	for i, key := range keys {
		whereSQL, whereArgs, err := buildRowSelector(key)
		if err != nil {
			return 0, fmt.Errorf("第 %d 行定位信息无效: %w", i+1, err)
		}
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s WHERE %s",
				quotedTable, colList, colList, quotedTable, whereSQL),
			whereArgs...)
		if err != nil {
			return 0, fmt.Errorf("复制行失败（主键/唯一约束冲突时请复制后修改键值）: %w", err)
		}
		n, _ := res.RowsAffected()
		total += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交复制事务失败: %w", err)
	}
	return total, nil
}

// buildRowSelector 根据主键 map 构造 WHERE 片段与参数。
func buildRowSelector(key map[string]any) (string, []any, error) {
	if len(key) == 0 {
		return "", nil, fmt.Errorf("缺少行定位字段（主键或 rowid）")
	}
	var conds []string
	var args []any
	for name, raw := range key {
		value, err := cell.Decode(raw)
		if err != nil {
			return "", nil, fmt.Errorf("定位字段 %q 的值无效: %w", name, err)
		}
		if name == identifier.RowidName {
			conds = append(conds, "rowid = ?")
		} else {
			quoted, err := identifier.ValidateAndQuote(name)
			if err != nil {
				return "", nil, fmt.Errorf("定位字段名非法: %w", err)
			}
			conds = append(conds, fmt.Sprintf("%s = ?", quoted))
		}
		args = append(args, value)
	}
	return strings.Join(conds, " AND "), args, nil
}
