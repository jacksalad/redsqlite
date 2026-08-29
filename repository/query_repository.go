package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/cell"
	"github.com/redjack/redsqlite/internal/sqliteutil"
)

func now() time.Time { return time.Now() }

func sinceMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// QueryRepository 执行任意单条 SQL（文档 6.5）。绑定单个连接（V2.1）。
type QueryRepository struct {
	store  *Store
	connID string
}

// NewQueryRepository 创建绑定到指定连接的查询仓库。
func NewQueryRepository(store *Store, connID string) *QueryRepository {
	return &QueryRepository{store: store, connID: connID}
}

// Execute 执行单条 SQL：查询类返回行，写入类返回影响行数。
// maxRows 为 0 表示不限制；超过 maxRows 时停止读取并标记 Truncated。
func (r *QueryRepository) Execute(ctx context.Context, sqlText string, maxRows int) (domain.QueryResult, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return domain.QueryResult{}, err
	}

	normalized, err := sqliteutil.ValidateSingle(sqlText)
	if err != nil {
		return domain.QueryResult{}, err
	}

	start := now()

	kind := sqliteutil.Classify(normalized)
	if kind == sqliteutil.KindQuery {
		rows, err := db.QueryContext(ctx, normalized)
		if err != nil {
			return domain.QueryResult{}, fmt.Errorf("执行 SQL 失败: %w", err)
		}
		scanned, err := scanRows(rows, maxRows)
		rows.Close()
		if err != nil {
			return domain.QueryResult{}, err
		}
		scanned.ElapsedMs = sinceMs(start)
		scanned.IsQuery = true
		return scanned, nil
	}

	res, err := db.ExecContext(ctx, normalized)
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("执行 SQL 失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	insertID, _ := res.LastInsertId()
	return domain.QueryResult{
		RowsAffected: affected,
		LastInsertID: insertID,
		ElapsedMs:    sinceMs(start),
		IsQuery:      false,
	}, nil
}

// ExecuteBatch 在单个事务中逐条执行多条语句（V1.2 多语句执行）。
// 任一语句失败时回滚整个事务，返回已完成条目、失败索引与失败原因
// （错误不作为 error 返回，交给前端结构化展示）。语句中的显式事务控制
// （BEGIN/COMMIT 等）会被拒绝；VACUUM 等无法在事务内运行的语句会由
// SQLite 报错并整体回滚。
func (r *QueryRepository) ExecuteBatch(ctx context.Context, statements []string, maxRows int) (domain.BatchResult, error) {
	db, err := r.store.DB(r.connID)
	if err != nil {
		return domain.BatchResult{}, err
	}
	if len(statements) == 0 {
		return domain.BatchResult{}, sqliteutil.ErrEmptySQL
	}
	for i, stmt := range statements {
		if sqliteutil.IsTransactionControl(stmt) {
			return domain.BatchResult{}, fmt.Errorf("第 %d 条语句包含显式事务控制（BEGIN/COMMIT 等），多语句执行已由事务包裹", i+1)
		}
	}

	start := now()
	result := domain.BatchResult{
		Items:       make([]domain.BatchItem, 0, len(statements)),
		FailedIndex: -1,
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.BatchResult{}, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	for i, stmt := range statements {
		item := domain.BatchItem{SQL: stmt}
		stmtStart := now()

		if sqliteutil.Classify(stmt) == sqliteutil.KindQuery {
			item.IsQuery = true
			rows, qerr := tx.QueryContext(ctx, stmt)
			if qerr == nil {
				var scanned domain.QueryResult
				scanned, qerr = scanRows(rows, maxRows)
				rows.Close()
				if qerr == nil {
					scanned.ElapsedMs = sinceMs(stmtStart)
					scanned.IsQuery = true
					item.Result = &scanned
				}
			}
			if qerr != nil {
				item.Error = qerr.Error()
			}
		} else {
			res, eerr := tx.ExecContext(ctx, stmt)
			if eerr != nil {
				item.Error = eerr.Error()
			} else {
				item.RowsAffected, _ = res.RowsAffected()
			}
		}

		result.Items = append(result.Items, item)
		if item.Error != "" {
			result.FailedIndex = i
			result.RolledBack = true
			result.TotalElapsedMs = sinceMs(start)
			// defer tx.Rollback() 负责回滚
			return result, nil
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.BatchResult{
			Items:          result.Items,
			FailedIndex:    len(statements),
			RolledBack:     true,
			TotalElapsedMs: sinceMs(start),
		}, fmt.Errorf("提交事务失败: %w", err)
	}
	result.TotalElapsedMs = sinceMs(start)
	return result, nil
}

// scanRows 读取 *sql.Rows，值经 cell.Encode 转为 JSON 安全形式。
// 立即编码也保证了 []byte 引用在下一次 Next 前完成拷贝。
func scanRows(rows *sql.Rows, maxRows int) (domain.QueryResult, error) {
	columns, err := rows.Columns()
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("读取结果列失败: %w", err)
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("读取结果列类型失败: %w", err)
	}
	typeNames := make([]string, len(colTypes))
	for i, ct := range colTypes {
		name := ct.DatabaseTypeName()
		if name == "" {
			name = "ANY"
		}
		typeNames[i] = name
	}

	result := domain.QueryResult{
		Columns:     columns,
		ColumnTypes: typeNames,
		Rows:        [][]any{},
	}

	for rows.Next() {
		if maxRows > 0 && len(result.Rows) >= maxRows {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return domain.QueryResult{}, fmt.Errorf("读取结果行失败: %w", err)
		}
		encoded := make([]any, len(values))
		for i, v := range values {
			encoded[i] = cell.Encode(v)
		}
		result.Rows = append(result.Rows, encoded)
	}
	if err := rows.Err(); err != nil {
		return domain.QueryResult{}, fmt.Errorf("读取结果失败: %w", err)
	}
	return result, nil
}
