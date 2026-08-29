package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
)

// TransferRepository 跨连接表传输引擎（V3.0）。源/目标是两个独立连接，
// 各自独立事务（绝无跨库事务）；数据按"读一批 → 关闭结果集 → 写一批"
// 流水执行，同一路径传输（同一连接）也不会占死唯一连接。
type TransferRepository struct {
	store     *Store
	sourceID  string
	targetID  string
	sourceSch *SchemaRepository
	targetSch *SchemaRepository
}

// NewTransferRepository 创建绑定源/目标连接的传输引擎。
func NewTransferRepository(store *Store, sourceConnID, targetConnID string) *TransferRepository {
	return &TransferRepository{
		store:     store,
		sourceID:  sourceConnID,
		targetID:  targetConnID,
		sourceSch: NewSchemaRepository(store, sourceConnID),
		targetSch: NewSchemaRepository(store, targetConnID),
	}
}

// ---------- DDL 表名改写 ----------

// identAlternatives 生成标识符四种引用形式的正则备选（双引号/方括号/反引号/裸）。
func identAlternatives(name string) string {
	escaped := regexp.QuoteMeta(name)
	return fmt.Sprintf(`"%s"|\[%s\]|`+"`%s`"+`|%s`, escaped, escaped, escaped, escaped)
}

var createTableNameRe = regexp.MustCompile(
	`(?is)^(\s*CREATE\s+(?:TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?)` +
		`("((?:[^"]|"")+)"|\[([^\]]+)\]|` + "`([^`]+)`" + `|([A-Za-z_][A-Za-z0-9_$]*))`)

// rewriteCreateTableName 把 CREATE TABLE 原文中的表名替换为新表名（保持其余原文不变）。
func rewriteCreateTableName(ddl, newName string) (string, error) {
	quoted, err := identifier.ValidateAndQuote(newName)
	if err != nil {
		return "", fmt.Errorf("目标表名非法: %w", err)
	}
	m := createTableNameRe.FindStringSubmatch(strings.TrimSpace(ddl))
	if m == nil {
		return "", fmt.Errorf("无法解析源表的建表语句（表名改写失败）")
	}
	return m[1] + quoted + strings.TrimSpace(ddl)[len(m[0]):], nil
}

// rewriteOnTable 把 DDL 中的 `ON <旧表> (` 子句（索引/触发器）改写为新表名。
// onRe 由调用方按旧表名编译一次复用（每次 Apply 旧表名固定，避免逐条 DDL 重编译）；
// onRe 为 nil 表示新旧表名相同，无需改写。
func rewriteOnTable(ddl string, onRe *regexp.Regexp, quotedNew string) (string, bool) {
	if onRe == nil {
		return ddl, true
	}
	if !onRe.MatchString(ddl) {
		return ddl, false
	}
	return onRe.ReplaceAllString(ddl, "${1}"+quotedNew+"${2}"), true
}

// ---------- 预检 ----------

// Preview 汇总源表结构信息（DDL 原文/索引/触发器/行数）。
// 只读校验与目标冲突检测由服务层完成（这里只读源侧）。
func (r *TransferRepository) Preview(ctx context.Context, sourceTable string) (domain.TransferPreview, error) {
	out := domain.TransferPreview{}
	typ, err := r.sourceSch.ObjectType(sourceTable)
	if err != nil {
		return out, err
	}
	if typ != "table" {
		return out, fmt.Errorf("对象 %q 不是表，无法传输", sourceTable)
	}
	out.SourceDDL, err = r.sourceSch.GetObjectSQL(sourceTable)
	if err != nil {
		return out, err
	}
	indexes, err := r.sourceSch.GetTableIndexesFull(sourceTable)
	if err != nil {
		return out, err
	}
	for _, idx := range indexes {
		// origin=c 为手动创建的索引；u/pk 由表约束自动创建，随建表语句走
		if idx.Origin == "c" && strings.TrimSpace(idx.SQL) != "" {
			out.IndexDDLs = append(out.IndexDDLs, idx.SQL)
		}
	}
	triggers, err := r.sourceSch.TableTriggers(sourceTable)
	if err != nil {
		return out, err
	}
	for _, tr := range triggers {
		if strings.TrimSpace(tr.SQL) != "" {
			out.TriggerDDLs = append(out.TriggerDDLs, tr.SQL)
		}
	}
	cols, err := r.sourceSch.GetTableColumns(sourceTable)
	if err != nil {
		return out, err
	}
	out.ColumnCount = len(cols)
	data := NewDataRepository(r.store, r.sourceID)
	if out.RowCount, err = data.CountAll(sourceTable); err != nil {
		return out, err
	}
	return out, nil
}

// ---------- 执行 ----------

// transferBatchSize 每批读取的行数（读一批关一批，内存有界）。
const transferBatchSize = 1000

// TransferProgress 传输进度回调：phase = copy / finalize。
type TransferProgress func(phase string, copied int64)

// Apply 执行传输：目标单事务（建表 → 拷数据 → 建索引 → 建触发器），
// 任一步失败整体回滚目标侧；源侧只读不受影响。
// 目标连接的 foreign_keys 由服务层在事务外切换（事务内是 no-op）。
func (r *TransferRepository) Apply(ctx context.Context, req domain.TransferRequest, progress TransferProgress) (int64, error) {
	srcDB, err := r.store.DB(r.sourceID)
	if err != nil {
		return 0, err
	}
	tgtDB, err := r.store.DB(r.targetID)
	if err != nil {
		return 0, err
	}

	preview, err := r.Preview(ctx, req.SourceTable)
	if err != nil {
		return 0, err
	}

	createSQL, err := rewriteCreateTableName(preview.SourceDDL, req.TargetTable)
	if err != nil {
		return 0, err
	}

	// 表名改写规则按旧表名编译一次，索引/触发器循环复用
	quotedTarget, err := identifier.ValidateAndQuote(req.TargetTable)
	if err != nil {
		return 0, fmt.Errorf("目标表名非法: %w", err)
	}
	var onRe *regexp.Regexp
	if req.SourceTable != req.TargetTable {
		onRe, err = regexp.Compile(`(?i)(\bON\s+)(?:` + identAlternatives(req.SourceTable) + `)(\s*\()`)
		if err != nil {
			return 0, fmt.Errorf("构建表名改写规则失败: %v", err)
		}
	}

	tx, err := tgtDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启目标事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return 0, fmt.Errorf("创建目标表失败（已回滚）: %w", err)
	}

	var copied int64
	if req.IncludeData {
		if copied, err = r.copyData(ctx, srcDB, tx, req, preview.RowCount, progress); err != nil {
			return 0, err
		}
	}

	if progress != nil {
		progress("finalize", copied)
	}
	// 索引/触发器按原文重建（ON 子句改写为目标表名；名称冲突时整体回滚）
	if req.IncludeIndexes {
		for _, ddl := range preview.IndexDDLs {
			rewritten, ok := rewriteOnTable(ddl, onRe, quotedTarget)
			if !ok {
				return 0, fmt.Errorf("索引 DDL 无法改写目标表名（已回滚）: %s", ddl)
			}
			if _, err := tx.ExecContext(ctx, rewritten); err != nil {
				return 0, fmt.Errorf("创建索引失败（已回滚）: %w", err)
			}
		}
	}
	if req.IncludeTriggers {
		for _, ddl := range preview.TriggerDDLs {
			rewritten, ok := rewriteOnTable(ddl, onRe, quotedTarget)
			if !ok {
				return 0, fmt.Errorf("触发器 DDL 无法改写目标表名（已回滚）: %s", ddl)
			}
			if _, err := tx.ExecContext(ctx, rewritten); err != nil {
				return 0, fmt.Errorf("创建触发器失败（已回滚）: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交目标事务失败: %w", err)
	}
	return copied, nil
}

// copyData 分批拷贝数据：源侧 SELECT 一批 → 立即扫描到内存 → 关闭结果集 →
// 目标事务内逐行参数绑定 INSERT。rowid 表按 rowid 范围分批（免 OFFSET 扫描）；
// WITHOUT ROWID 表按 OFFSET 分批（此类表通常较小）。
func (r *TransferRepository) copyData(
	ctx context.Context,
	srcDB *sql.DB,
	tx *sql.Tx,
	req domain.TransferRequest,
	total int64,
	progress TransferProgress,
) (int64, error) {
	columns, err := r.sourceSch.InsertableColumns(req.SourceTable)
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, fmt.Errorf("源表没有可复制的普通列")
	}
	quotedCols := make([]string, 0, len(columns))
	for _, c := range columns {
		q, err := identifier.ValidateAndQuote(c)
		if err != nil {
			return 0, fmt.Errorf("源列名非法: %w", err)
		}
		quotedCols = append(quotedCols, q)
	}
	colList := strings.Join(quotedCols, ", ")
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		identifier.Quote(req.TargetTable), colList, placeholders)

	// 预编译插入语句：同一 SQL 逐行执行，免每行重解析（同 tabular.Import 引擎）
	insertStmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("准备插入语句失败: %w", err)
	}
	defer insertStmt.Close()

	srcQ, err := identifier.ValidateAndQuote(req.SourceTable)
	if err != nil {
		return 0, err
	}

	// 读一批 → 写一批；scanRow 立即拷贝 []byte（驱动复用缓冲）
	readBatch := func(where string, args ...any) ([][]any, error) {
		rows, err := srcDB.QueryContext(ctx,
			fmt.Sprintf("SELECT %s FROM %s%s", colList, srcQ, where), args...)
		if err != nil {
			return nil, fmt.Errorf("读取源数据失败: %w", err)
		}
		var batch [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("读取源数据行失败: %w", err)
			}
			row := make([]any, len(values))
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					row[i] = append([]byte(nil), b...) // 拷贝，防驱动复用缓冲
				} else {
					row[i] = v
				}
			}
			batch = append(batch, row)
			if len(batch) >= transferBatchSize {
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取源数据失败: %w", err)
		}
		rows.Close()
		return batch, nil
	}
	insertBatch := func(batch [][]any) (int64, error) {
		var n int64
		for _, row := range batch {
			if _, err := insertStmt.ExecContext(ctx, row...); err != nil {
				return n, fmt.Errorf("写入目标数据失败（已回滚）: %w", err)
			}
			n++
		}
		return n, nil
	}

	var copied int64
	if r.sourceSch.HasRowidColumn(req.SourceTable) {
		// rowid 范围分批：边界探测（同表重建的分块技术，免 OFFSET）
		var prev *int64
		for {
			if err := ctx.Err(); err != nil {
				return copied, fmt.Errorf("传输已取消（已回滚）: %w", err)
			}
			boundQuery := fmt.Sprintf("SELECT rowid FROM %s", srcQ)
			var boundArgs []any
			if prev != nil {
				boundQuery += " WHERE rowid > ?"
				boundArgs = append(boundArgs, *prev)
			}
			boundQuery += fmt.Sprintf(" ORDER BY rowid LIMIT 1 OFFSET %d", transferBatchSize-1)
			var upper int64
			err := srcDB.QueryRowContext(ctx, boundQuery, boundArgs...).Scan(&upper)
			hasBound := true
			if errors.Is(err, sql.ErrNoRows) {
				hasBound = false
			} else if err != nil {
				return copied, fmt.Errorf("探测分批边界失败: %w", err)
			}

			var where string
			var args []any
			switch {
			case prev != nil && hasBound:
				where, args = " WHERE rowid > ? AND rowid <= ?", []any{*prev, upper}
			case prev != nil:
				where, args = " WHERE rowid > ?", []any{*prev}
			case hasBound:
				where, args = " WHERE rowid <= ?", []any{upper}
			}
			batch, err := readBatch(where, args...)
			if err != nil {
				return copied, err
			}
			if len(batch) == 0 {
				return copied, nil
			}
			n, err := insertBatch(batch)
			copied += n
			if progress != nil {
				progress("copy", copied)
			}
			if err != nil {
				return copied, err
			}
			if !hasBound || int64(len(batch)) < transferBatchSize {
				return copied, nil
			}
			prev = &upper
		}
	}

	// WITHOUT ROWID：按 OFFSET 分批
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return copied, fmt.Errorf("传输已取消（已回滚）: %w", err)
		}
		batch, err := readBatch(fmt.Sprintf(" LIMIT %d OFFSET %d", transferBatchSize, offset), nil...)
		if err != nil {
			return copied, err
		}
		if len(batch) == 0 {
			return copied, nil
		}
		n, err := insertBatch(batch)
		copied += n
		if progress != nil {
			progress("copy", copied)
		}
		if err != nil {
			return copied, err
		}
		if int64(len(batch)) < transferBatchSize {
			return copied, nil
		}
		offset += transferBatchSize
	}
}
