package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/appdata"
	"github.com/redjack/redsqlite/internal/sqliteutil"
	"github.com/redjack/redsqlite/repository"
)

// queryCancel 记录某个连接当前正在执行的查询（V2.1：按连接取消）。
type queryCancel struct {
	gen    uint64
	cancel context.CancelFunc
}

// QueryService 执行任意 SQL（文档 7.5）。V2.1：按 connID 执行与取消。
type QueryService struct {
	store *repository.Store
	apps  *appdata.Store

	mu     sync.Mutex
	gen    uint64
	active map[string]*queryCancel // connID → 当前查询
}

// NewQueryService 创建查询服务。
func NewQueryService(store *repository.Store, apps *appdata.Store) *QueryService {
	return &QueryService{
		store:  store,
		apps:   apps,
		active: map[string]*queryCancel{},
	}
}

// begin 开始一次执行：取消该连接上一个未完成的查询并登记新的取消句柄。
func (s *QueryService) begin(connID string, cancel context.CancelFunc) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.active[connID]; ok && old.cancel != nil {
		old.cancel()
	}
	s.gen++
	gen := s.gen
	s.active[connID] = &queryCancel{gen: gen, cancel: cancel}
	return gen
}

// finish 结束一次执行：仍是当前代数时清除登记。
func (s *QueryService) finish(connID string, gen uint64, cancel context.CancelFunc) {
	s.mu.Lock()
	if cur, ok := s.active[connID]; ok && cur.gen == gen {
		delete(s.active, connID)
	}
	s.mu.Unlock()
	cancel() // 幂等，及时释放 context 资源
}

// Execute 在指定连接上执行单条 SQL。开始前会取消该连接上一个未完成的查询。
func (s *QueryService) Execute(connID string, sqlText string) (domain.QueryResult, error) {
	normalized, err := sqliteutil.ValidateSingle(sqlText)
	if err != nil {
		return domain.QueryResult{}, err
	}

	baseCtx, err := s.store.Context(connID)
	if err != nil {
		return domain.QueryResult{}, err
	}

	timeout := s.apps.Settings().QueryTimeoutMs
	ctx, cancel := context.WithTimeout(baseCtx, msDuration(timeout))
	gen := s.begin(connID, cancel)

	result, err := repository.NewQueryRepository(s.store, connID).Execute(ctx, normalized, s.apps.Settings().MaxQueryRows)

	s.finish(connID, gen, cancel)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return domain.QueryResult{}, fmt.Errorf("查询已中止")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.QueryResult{}, fmt.Errorf("查询超时（可在设置中调整超时时间）")
		}
		slog.Error("执行 SQL 失败", "sql", sqlPreview(normalized), "err", err)
		return domain.QueryResult{}, err
	}
	return result, nil
}

// sqlPreview 截取 SQL 前缀用于日志，避免整条超长 SQL 刷屏。
func sqlPreview(sqlText string) string {
	const max = 200
	r := []rune(sqlText)
	if len(r) <= max {
		return sqlText
	}
	return string(r[:max]) + "…"
}

// Cancel 取消指定连接正在执行的查询（文档 7.5）。
func (s *QueryService) Cancel(connID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.active[connID]; ok {
		if cur.cancel != nil {
			cur.cancel()
		}
		delete(s.active, connID)
	}
	return nil
}

// Preview 逐条拆分 SQL 并给出类型与危险标记（V1.2 多语句确认对话框数据源）。
// 纯文本分析，与连接无关。返回条数即待执行语句数；空 SQL 返回错误。
func (s *QueryService) Preview(sqlText string) ([]domain.StatementPreview, error) {
	statements := sqliteutil.SplitStatements(sqlText)
	if len(statements) == 0 {
		return nil, sqliteutil.ErrEmptySQL
	}
	out := make([]domain.StatementPreview, 0, len(statements))
	for _, stmt := range statements {
		kind := "exec"
		if sqliteutil.Classify(stmt) == sqliteutil.KindQuery {
			kind = "query"
		}
		item := domain.StatementPreview{SQL: stmt, Kind: kind}
		if rep := sqliteutil.AnalyzeDangerous(stmt); rep.Dangerous {
			item.Dangerous = true
			item.Reason = rep.Reason
		}
		if sqliteutil.IsTransactionControl(stmt) {
			item.Dangerous = true
			item.Reason = "多语句执行由事务包裹，不能包含显式事务控制（BEGIN/COMMIT 等）"
		}
		out = append(out, item)
	}
	return out, nil
}

// ExecuteBatch 多语句事务执行（V1.2）：逐条执行、任一失败整体回滚。
// 单条输入走 Execute 的普通路径（支持 VACUUM 等事务外语句）。
func (s *QueryService) ExecuteBatch(connID string, sqlText string) (domain.BatchResult, error) {
	statements := sqliteutil.SplitStatements(sqlText)
	if len(statements) == 0 {
		return domain.BatchResult{}, sqliteutil.ErrEmptySQL
	}
	if len(statements) == 1 {
		return domain.BatchResult{}, fmt.Errorf("单条语句请使用普通执行")
	}

	baseCtx, err := s.store.Context(connID)
	if err != nil {
		return domain.BatchResult{}, err
	}

	timeout := s.apps.Settings().QueryTimeoutMs
	ctx, cancel := context.WithTimeout(baseCtx, msDuration(timeout))
	gen := s.begin(connID, cancel)

	result, err := repository.NewQueryRepository(s.store, connID).ExecuteBatch(ctx, statements, s.apps.Settings().MaxQueryRows)

	s.finish(connID, gen, cancel)

	if err != nil {
		slog.Error("多语句执行失败", "count", len(statements), "err", err)
		return domain.BatchResult{}, err
	}
	if result.FailedIndex >= 0 {
		slog.Error("多语句中途回滚",
			"count", len(statements), "failedIndex", result.FailedIndex,
			"reason", result.Items[result.FailedIndex].Error)
	} else {
		slog.Info("多语句执行完成", "count", len(statements), "elapsedMs", result.TotalElapsedMs)
	}
	return result, nil
}

func msDuration(ms int) time.Duration {
	if ms <= 0 {
		ms = 30000
	}
	return time.Duration(ms) * time.Millisecond
}
