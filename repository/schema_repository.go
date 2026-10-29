package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
)

// SchemaRepository 查询数据库结构（文档 6.2）。
// V2.1：绑定单个连接（connID），由服务层按连接构造。
type SchemaRepository struct {
	store  *Store
	connID string
}

// NewSchemaRepository 创建绑定到指定连接的结构仓库。
func NewSchemaRepository(store *Store, connID string) *SchemaRepository {
	return &SchemaRepository{store: store, connID: connID}
}

// GetSchema 返回表、视图、索引列表。
func (r *SchemaRepository) GetSchema() (domain.SchemaInfo, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return domain.SchemaInfo{}, err
	}

	tables := []domain.TableInfo{}
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("查询表列表失败: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return domain.SchemaInfo{}, fmt.Errorf("读取表列表失败: %w", err)
		}
		tables = append(tables, domain.TableInfo{Name: name})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("读取表列表失败: %w", err)
	}

	views := []domain.ViewInfo{}
	rows, err = db.Query(`
		SELECT name, COALESCE(sql, '') FROM sqlite_master
		WHERE type = 'view' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("查询视图列表失败: %w", err)
	}
	for rows.Next() {
		var v domain.ViewInfo
		if err := rows.Scan(&v.Name, &v.SQL); err != nil {
			rows.Close()
			return domain.SchemaInfo{}, fmt.Errorf("读取视图列表失败: %w", err)
		}
		views = append(views, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("读取视图列表失败: %w", err)
	}

	indexes := []domain.IndexInfo{}
	rows, err = db.Query(`
		SELECT name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'index' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("查询索引列表失败: %w", err)
	}
	for rows.Next() {
		var idx domain.IndexInfo
		if err := rows.Scan(&idx.Name, &idx.Table, &idx.SQL); err != nil {
			rows.Close()
			return domain.SchemaInfo{}, fmt.Errorf("读取索引列表失败: %w", err)
		}
		indexes = append(indexes, idx)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("读取索引列表失败: %w", err)
	}

	triggers := []domain.TriggerInfo{}
	rows, err = db.Query(`
		SELECT name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'trigger' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("查询触发器列表失败: %w", err)
	}
	for rows.Next() {
		var tr domain.TriggerInfo
		if err := rows.Scan(&tr.Name, &tr.Table, &tr.SQL); err != nil {
			rows.Close()
			return domain.SchemaInfo{}, fmt.Errorf("读取触发器列表失败: %w", err)
		}
		triggers = append(triggers, tr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.SchemaInfo{}, fmt.Errorf("读取触发器列表失败: %w", err)
	}

	return domain.SchemaInfo{Tables: tables, Views: views, Indexes: indexes, Triggers: triggers}, nil
}

// GetTableColumns 返回表字段信息（含视图）。
func (r *SchemaRepository) GetTableColumns(table string) ([]domain.ColumnInfo, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoted))
	if err != nil {
		return nil, fmt.Errorf("查询表结构失败: %w", err)
	}
	defer rows.Close()

	columns := []domain.ColumnInfo{}
	for rows.Next() {
		var c domain.ColumnInfo
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("读取表结构失败: %w", err)
		}
		c.NotNull = notNull == 1
		c.PrimaryKey = pk > 0
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取表结构失败: %w", err)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("对象 %q 不存在或没有字段", table)
	}
	return columns, nil
}

// GetObjectSQL 返回 sqlite_master 中的建库语句。
func (r *SchemaRepository) GetObjectSQL(name string) (string, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return "", err
	}
	var ddl sql.NullString
	err = db.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE name = ? AND name NOT LIKE 'sqlite_%'`, name).Scan(&ddl)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("对象 %q 不存在", name)
	}
	if err != nil {
		return "", fmt.Errorf("查询对象定义失败: %w", err)
	}
	return ddl.String, nil
}

// IsView 判断对象是否是视图。
func (r *SchemaRepository) IsView(name string) (bool, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return false, err
	}
	var typ string
	err = db.QueryRow(`SELECT type FROM sqlite_master WHERE name = ?`, name).Scan(&typ)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("对象 %q 不存在", name)
	}
	if err != nil {
		return false, fmt.Errorf("查询对象类型失败: %w", err)
	}
	return typ == "view", nil
}

// GetTableIndexes 返回指定表的索引（含自动索引）。
func (r *SchemaRepository) GetTableIndexes(table string) ([]domain.IndexInfo, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", quoted))
	if err != nil {
		return nil, fmt.Errorf("查询索引失败: %w", err)
	}

	var idxNames []domain.IndexInfo
	for rows.Next() {
		var idx domain.IndexInfo
		var seq, unique, partial int
		if err := rows.Scan(&seq, &idx.Name, &unique, &idx.Origin, &partial); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取索引失败: %w", err)
		}
		idx.Table = table
		idx.Unique = unique == 1
		idxNames = append(idxNames, idx)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取索引失败: %w", err)
	}

	// 补充索引列与定义
	for i := range idxNames {
		idxQuoted, err := identifier.ValidateAndQuote(idxNames[i].Name)
		if err != nil {
			continue
		}
		colRows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", idxQuoted))
		if err != nil {
			continue
		}
		for colRows.Next() {
			var seqno, cid int
			var name sql.NullString
			if err := colRows.Scan(&seqno, &cid, &name); err == nil && name.Valid {
				idxNames[i].Columns = append(idxNames[i].Columns, name.String)
			}
		}
		colRows.Close()
	}

	return idxNames, nil
}

// HasRowidColumn 判断表是否可使用 rowid 定位行
// （WITHOUT ROWID 表与视图不支持）。
func (r *SchemaRepository) HasRowidColumn(table string) bool {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return false
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return false
	}
	rows, err := db.Query(fmt.Sprintf("SELECT rowid FROM %s LIMIT 1", quoted))
	if err != nil {
		return false
	}
	rows.Close()
	return true
}

// ---------- V2.4 表重建支持 ----------

// IndexFull 重建所需的完整索引信息（含 partial 标记与表达式列检测）。
type IndexFull struct {
	Name          string
	Unique        bool
	Origin        string // c=手动 / u=唯一约束 / pk=WITHOUT ROWID 主键
	Partial       bool
	SQL           string // sqlite_master 原文；自动索引为空
	Cols          []string
	HasExpression bool // index_info 中出现 cid<0（表达式/rowid 列）
}

// GetTableIndexesFull 返回表的全部索引（含 partial/表达式检测），供表重建决策。
func (r *SchemaRepository) GetTableIndexesFull(table string) ([]IndexFull, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", quoted))
	if err != nil {
		return nil, fmt.Errorf("查询索引失败: %w", err)
	}
	var out []IndexFull
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取索引失败: %w", err)
		}
		out = append(out, IndexFull{
			Name:    name,
			Unique:  unique == 1,
			Origin:  origin,
			Partial: partial == 1,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 补充 sqlite_master 的 SQL 与索引列
	for i := range out {
		_ = db.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='index' AND name = ?`, out[i].Name).
			Scan(&out[i].SQL)
		q, err := identifier.ValidateAndQuote(out[i].Name)
		if err != nil {
			continue
		}
		infoRows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", q))
		if err != nil {
			continue
		}
		for infoRows.Next() {
			var seqno, cid int
			var name sql.NullString
			if err := infoRows.Scan(&seqno, &cid, &name); err == nil {
				if cid < 0 {
					out[i].HasExpression = true // 表达式列或 rowid
				} else if name.Valid {
					out[i].Cols = append(out[i].Cols, name.String)
				}
			}
		}
		infoRows.Close()
	}
	return out, nil
}

// TableTriggers 返回挂在该表上的触发器定义（sqlite_master type='trigger' 且 tbl_name=表）。
func (r *SchemaRepository) TableTriggers(table string) ([]domain.TriggerInfo, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT name, tbl_name, COALESCE(sql,'') FROM sqlite_master
		WHERE type = 'trigger' AND tbl_name = ?`, table)
	if err != nil {
		return nil, fmt.Errorf("查询触发器失败: %w", err)
	}
	defer rows.Close()
	var out []domain.TriggerInfo
	for rows.Next() {
		var tr domain.TriggerInfo
		if err := rows.Scan(&tr.Name, &tr.Table, &tr.SQL); err != nil {
			return nil, fmt.Errorf("读取触发器失败: %w", err)
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

// HasGeneratedColumns 检测表是否含生成列（PRAGMA table_xinfo 的 hidden > 0）。
func (r *SchemaRepository) HasGeneratedColumns(table string) (bool, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return false, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return false, fmt.Errorf("表名非法: %w", err)
	}
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_xinfo(%s)", quoted))
	if err != nil {
		return false, fmt.Errorf("查询表结构失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// 列序：cid, name, type, notnull, dflt_value, pk, hidden（pk 在 hidden 前！）
		var cid, notNull, pk, hidden int
		var name, colType string
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk, &hidden); err != nil {
			return false, fmt.Errorf("读取表结构失败: %w", err)
		}
		if hidden > 0 {
			return true, nil
		}
	}
	return false, rows.Err()
}

// InsertableColumns 返回可直接 INSERT 的普通列名（按 cid 顺序，
// 排除生成列与虚拟表隐藏列；复制行的 INSERT SELECT 用，V2.5）。
func (r *SchemaRepository) InsertableColumns(table string) ([]string, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_xinfo(%s)", quoted))
	if err != nil {
		return nil, fmt.Errorf("查询表结构失败: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		// 列序：cid, name, type, notnull, dflt_value, pk, hidden（pk 在 hidden 前！）
		var cid, notNull, pk, hidden int
		var name, colType string
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk, &hidden); err != nil {
			return nil, fmt.Errorf("读取表结构失败: %w", err)
		}
		if hidden == 0 {
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

// SetForeignKeys 开关连接级外键约束。必须在事务外调用
// （事务内是 no-op）；本项目单连接（SetMaxOpenConns(1)）保证无并发窗口。
func (r *SchemaRepository) SetForeignKeys(on bool) error {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return err
	}
	value := 0
	if on {
		value = 1
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA foreign_keys(%d)", value)); err != nil {
		return fmt.Errorf("切换外键约束失败: %w", err)
	}
	return nil
}

// ForeignKeysEnabled 返回连接当前的外键约束开关。
func (r *SchemaRepository) ForeignKeysEnabled() bool {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return false
	}
	var v int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&v); err != nil {
		return false
	}
	return v == 1
}

// RedesignCopyStep 表重建的数据拷贝步骤（可按 rowid 分块，回报进度）。
type RedesignCopyStep struct {
	Target     string   // 目标表（临时新表，已引用）
	TargetCols []string // 目标列（已引用，与 SourceCols 一一对应）
	Source     string   // 源表（已引用）
	SourceCols []string // 源列（已引用）
	ByRowid    bool     // 源表是 rowid 表：分块拷贝并回报进度；否则整条执行
	Total      int64    // 预计行数（进度分母；0 = 未知，仅报已拷行数）
}

// RedesignPlan 表重建执行计划：Pre（建表）→ Copy（拷数据，可分块）→
// Post（删旧表/改名/重建索引触发器）。
type RedesignPlan struct {
	Pre  []string
	Copy *RedesignCopyStep
	Post []string
}

// redesignChunkMin/Max 分块行数边界：实际块大小按 total/200 自适应
// （服务层），本层强制钳制在 [min, max]——小表一次整拷，超大表块不过大，
// 进度回调次数约 total/max 起步、上限约 total/min。
const (
	redesignChunkMin = 1000
	redesignChunkMax = 50000
)

// ExecRedesignPlan 单事务执行重建计划。数据拷贝按 rowid 分块（块间调用
// progress 回报累计行数，供服务层发进度事件），分块不改变原子性——
// 全部在同一事务内，任一步失败整体回滚；提交前 foreign_key_check。
func (r *SchemaRepository) ExecRedesignPlan(ctx context.Context, plan RedesignPlan, chunkSize int, progress func(copied int64)) error {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	execAll := func(statements []string, phase string) error {
		for i, stmt := range statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("重建%s第 %d 步失败（已回滚）: %w", phase, i+1, err)
			}
		}
		return nil
	}
	if err := execAll(plan.Pre, ""); err != nil {
		return err
	}

	if step := plan.Copy; step != nil {
		if chunkSize <= 0 {
			chunkSize = redesignChunkMin
		} else if chunkSize > redesignChunkMax {
			chunkSize = redesignChunkMax
		}
		if err := r.execRedesignCopy(ctx, tx, step, chunkSize, progress); err != nil {
			return err
		}
	}
	if err := execAll(plan.Post, "后续"); err != nil {
		return err
	}

	if err := r.checkForeignKeys(ctx, tx); err != nil {
		return err
	}

	return tx.Commit()
}

// checkForeignKeys 在事务内运行 PRAGMA foreign_key_check 并清点违例。
// 重建期间 foreign_keys 已被关闭（PRAGMA 在事务内是 no-op，只能事务外切），
// 这次检查是提交前的唯一防线：查询或读取本身失败也必须返回错误阻断提交，
// 而不是跳过校验放行可能含脏外键数据的事务。
func (r *SchemaRepository) checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("外键检查执行失败（已回滚）: %w", err)
	}
	defer rows.Close()
	violations := 0
	sample := ""
	for rows.Next() {
		var tbl string
		var rowid any
		var refTable string
		var fkid int
		if err := rows.Scan(&tbl, &rowid, &refTable, &fkid); err != nil {
			return fmt.Errorf("外键检查读取失败（已回滚）: %w", err)
		}
		violations++
		if sample == "" {
			sample = fmt.Sprintf("%s 行 %v -> %s", tbl, rowid, refTable)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("外键检查读取失败（已回滚）: %w", err)
	}
	if violations > 0 {
		return fmt.Errorf("外键检查未通过（%d 处违例，首例：%s），已回滚", violations, sample)
	}
	return nil
}

// execRedesignCopy 执行数据拷贝：rowid 表按 rowid 范围分块（每块一个
// INSERT...SELECT，块间回调进度并检查取消）；非 rowid 表整条拷贝。
func (r *SchemaRepository) execRedesignCopy(ctx context.Context, tx *sql.Tx, step *RedesignCopyStep, chunkSize int, progress func(copied int64)) error {
	colList := strings.Join(step.TargetCols, ", ")
	srcList := strings.Join(step.SourceCols, ", ")
	insert := func(where string, args ...any) (int64, error) {
		res, err := tx.ExecContext(ctx, fmt.Sprintf(
			"INSERT INTO %s (%s) SELECT %s FROM %s%s",
			step.Target, colList, srcList, step.Source, where), args...)
		if err != nil {
			return 0, fmt.Errorf("拷贝数据失败（已回滚）: %w", err)
		}
		n, _ := res.RowsAffected()
		return n, nil
	}

	if !step.ByRowid {
		n, err := insert("")
		if err != nil {
			return err
		}
		if progress != nil {
			progress(n)
		}
		return nil
	}

	// rowid 分块：先用边界探测查询求每块上界（rowid <= upper），
	// 避免对源表做 OFFSET 扫描；全程对 rowid 索引线性推进
	var copied int64
	var prev *int64
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("重建已取消（已回滚）: %w", err)
		}
		boundQuery := fmt.Sprintf("SELECT rowid FROM %s", step.Source)
		var boundArgs []any
		if prev != nil {
			boundQuery += " WHERE rowid > ?"
			boundArgs = append(boundArgs, *prev)
		}
		boundQuery += fmt.Sprintf(" ORDER BY rowid LIMIT 1 OFFSET %d", chunkSize-1)
		var upper int64
		err := tx.QueryRowContext(ctx, boundQuery, boundArgs...).Scan(&upper)
		hasBound := true
		if errors.Is(err, sql.ErrNoRows) {
			hasBound = false
		} else if err != nil {
			return fmt.Errorf("探测分块边界失败（已回滚）: %w", err)
		}

		var n int64
		if !hasBound {
			// 剩余不足一块：全部拷完收尾
			if prev != nil {
				n, err = insert(" WHERE rowid > ?", *prev)
			} else {
				n, err = insert("")
			}
			if err != nil {
				return err
			}
			copied += n
			if progress != nil {
				progress(copied)
			}
			return nil
		}
		if prev != nil {
			n, err = insert(" WHERE rowid > ? AND rowid <= ?", *prev, upper)
		} else {
			n, err = insert(" WHERE rowid <= ?", upper)
		}
		if err != nil {
			return err
		}
		copied += n
		if progress != nil {
			progress(copied)
		}
		if n == 0 {
			return nil
		}
		prev = &upper
	}
}

// ExecTransaction 在单事务中按序执行多条语句（V3.0 视图的 DROP+CREATE 等
// 原子组合操作），任一步失败整体回滚。不含外键检查（表重建专用 ExecRedesign）。
func (r *SchemaRepository) ExecTransaction(ctx context.Context, statements []string) error {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	for i, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("第 %d 条语句执行失败（已回滚）: %w", i+1, err)
		}
	}
	return tx.Commit()
}

// GetTableForeignKeys 返回表自身定义的外键（PRAGMA foreign_key_list，
// 8 列，多列外键共享同一 ID）。
func (r *SchemaRepository) GetTableForeignKeys(table string) ([]domain.ForeignKeyInfo, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoted))
	if err != nil {
		return nil, fmt.Errorf("查询外键失败: %w", err)
	}
	defer rows.Close()
	var fks []domain.ForeignKeyInfo
	for rows.Next() {
		var fk domain.ForeignKeyInfo
		var fromCol, toCol sql.NullString
		// 列序：id, seq, table, from, to, on_update, on_delete, match（on_update 在前！）
		if err := rows.Scan(&fk.ID, &fk.Seq, &fk.RefTable, &fromCol, &toCol,
			&fk.OnUpdate, &fk.OnDelete, new(string)); err != nil {
			return nil, fmt.Errorf("读取外键失败: %w", err)
		}
		fk.Column = fromCol.String
		fk.RefColumn = toCol.String
		fks = append(fks, fk)
	}
	return fks, rows.Err()
}

// CompletionSchema 返回表/视图名 → 列名列表，供 SQL 编辑器自动补全（V2.3）。
func (r *SchemaRepository) CompletionSchema() (map[string][]string, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("查询对象列表失败: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取对象列表失败: %w", err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(names))
	for _, name := range names {
		quoted, err := identifier.ValidateAndQuote(name)
		if err != nil {
			continue
		}
		colRows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoted))
		if err != nil {
			continue
		}
		cols := []string{}
		for colRows.Next() {
			var cid int
			var colName, colType string
			var notNull, pk int
			var dflt any
			if err := colRows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err == nil {
				cols = append(cols, colName)
			}
		}
		colRows.Close()
		out[name] = cols
	}
	return out, nil
}

// Exec 执行服务层生成的单条 DDL（ALTER/CREATE/DROP）。
func (r *SchemaRepository) Exec(sqlText string) error {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return err
	}
	if _, err := db.Exec(sqlText); err != nil {
		return err
	}
	return nil
}

// ObjectType 返回 sqlite_master 中的对象类型
// （table/view/index/trigger），不存在时返回空串。
func (r *SchemaRepository) ObjectType(name string) (string, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return "", err
	}
	var typ string
	err = db.QueryRow(`SELECT type FROM sqlite_master WHERE name = ?`, name).Scan(&typ)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("查询对象类型失败: %w", err)
	}
	return typ, nil
}

// NameExists 检查是否已存在同名对象（表/视图/索引/触发器任一）。
func (r *SchemaRepository) NameExists(name string) (bool, error) {
	typ, err := r.ObjectType(name)
	if err != nil {
		return false, err
	}
	return typ != "", nil
}

// ForeignKeyRef 一条外键引用描述。
type ForeignKeyRef struct {
	FromTable string // 定义外键的表（子表）
	FromCol   string // 子表列
	ToTable   string // 被引用表（父表）
	ToCol     string // 被引用列；空表示引用父表主键
}

// ForeignKeysReferencing 扫描全库外键定义，返回与给定表/列相关的外键。
// column 为空时匹配"引用了该表"的外键（用于重命名/删除表）；
// column 非空时额外匹配"以该列作为子表列"的本表外键（用于删列）。
func (r *SchemaRepository) ForeignKeysReferencing(table, column string) ([]ForeignKeyRef, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	if err := identifier.Validate(table); err != nil {
		return nil, fmt.Errorf("表名非法: %w", err)
	}

	rows, err := db.Query(`SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("查询表列表失败: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取表列表失败: %w", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取表列表失败: %w", err)
	}

	var refs []ForeignKeyRef
	for _, tbl := range tables {
		qtbl, err := identifier.ValidateAndQuote(tbl)
		if err != nil {
			continue
		}
		fkRows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", qtbl))
		if err != nil {
			continue
		}
		for fkRows.Next() {
			// 新版 SQLite 的 foreign_key_list 返回 8 列：
			// id, seq, table, from, to, on_update, on_delete, match（必须全收）
			var id, seq int
			var toTable, onUpdate, onDelete, match string
			var fromCol, toCol sql.NullString
			if err := fkRows.Scan(&id, &seq, &toTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
				continue
			}
			ref := ForeignKeyRef{
				FromTable: tbl,
				FromCol:   fromCol.String,
				ToTable:   toTable,
				ToCol:     toCol.String,
			}
			if column == "" {
				if toTable == table {
					refs = append(refs, ref)
				}
				continue
			}
			// 列级：父表方向（其他表引用 table.column）与子表方向（本表以 column 为外键列）
			if toTable == table && (toCol.String == column || !toCol.Valid) {
				refs = append(refs, ref)
			} else if tbl == table && fromCol.String == column {
				refs = append(refs, ref)
			}
		}
		fkRows.Close()
	}
	return refs, nil
}

// ObjectsReferencing 在视图/触发器的 SQL 定义中查找按词引用了
// identifier 的对象名（大小写不敏感），用于删列/删表前的失效提示。
func (r *SchemaRepository) ObjectsReferencing(name string) ([]string, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT name, type, COALESCE(sql, '') FROM sqlite_master
		WHERE type IN ('view', 'trigger') AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, fmt.Errorf("查询对象定义失败: %w", err)
	}
	var refs []string
	for rows.Next() {
		var objName, objType, sqlText string
		if err := rows.Scan(&objName, &objType, &sqlText); err != nil {
			continue
		}
		if ContainsIdentifier(sqlText, name) {
			label := objName
			switch objType {
			case "view":
				label += "（视图）"
			case "trigger":
				label += "（触发器）"
			}
			refs = append(refs, label)
		}
	}
	rows.Close()
	return refs, rows.Err()
}

// ContainsIdentifier 判断 SQL 文本中是否以"完整词"出现标识符
// （前后不是标识符字符），避免 username 误命中 user。
// 按字节匹配两侧已统一 ToLower，多字节中文标识符同样成立。
func ContainsIdentifier(sqlText, name string) bool {
	if name == "" || sqlText == "" {
		return false
	}
	lowerSQL := strings.ToLower(sqlText)
	target := strings.ToLower(name)
	for from := 0; from+len(target) <= len(lowerSQL); {
		idx := strings.Index(lowerSQL[from:], target)
		if idx < 0 {
			return false
		}
		pos := from + idx
		end := pos + len(target)
		beforeOK := pos == 0 || !isIdentByte(lowerSQL[pos-1])
		afterOK := end >= len(lowerSQL) || !isIdentByte(lowerSQL[end])
		if beforeOK && afterOK {
			return true
		}
		from = pos + 1
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		c >= 0x80 // 中文等非 ASCII 标识符字符
}
