package services

import (
	"fmt"
	"log/slog"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/repository"
)

// DataService 分页查询、编辑、插入、删除（文档 7.4）。
// V2.1：所有方法以 connID 指定目标连接，仓库按连接临时构造。
type DataService struct {
	store *repository.Store
}

// NewDataService 创建数据服务。
func NewDataService(store *repository.Store) *DataService {
	return &DataService{store: store}
}

// GetTableData 分页读取表数据。
func (s *DataService) GetTableData(connID string, req domain.TableDataRequest) (domain.TableData, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return domain.TableData{}, err
	}
	result, err := repository.NewDataRepository(s.store, connID).GetTableData(ctx, req)
	if err != nil {
		slog.Error("读取表数据失败", "table", req.Table, "err", err)
		return domain.TableData{}, err
	}
	return result, nil
}

// CountTableRows 异步统计行数（V1.1：翻页不再阻塞在 COUNT(*) 上）。
// 可同时带快速文本过滤与结构化筛选（V3.0）。
func (s *DataService) CountTableRows(connID string, table string, filter string, conditions []domain.FilterCondition) (int64, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return 0, err
	}
	total, err := repository.NewDataRepository(s.store, connID).CountFiltered(ctx, table, filter, conditions)
	if err != nil {
		slog.Error("统计行数失败", "table", table, "err", err)
		return 0, err
	}
	return total, nil
}

// InsertRow 插入一行，返回 LastInsertId。
func (s *DataService) InsertRow(connID string, table string, values map[string]any) (int64, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return 0, err
	}
	id, err := repository.NewDataRepository(s.store, connID).InsertRow(ctx, table, values)
	if err != nil {
		slog.Error("插入行失败", "table", table, "err", err)
		return 0, err
	}
	return id, nil
}

// UpdateCell 更新单元格。key 为主键值或 rowid。
func (s *DataService) UpdateCell(connID string, table string, key map[string]any, column string, value any) (int64, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return 0, err
	}
	if column == "" {
		return 0, fmt.Errorf("缺少要更新的列名")
	}
	n, err := repository.NewDataRepository(s.store, connID).UpdateCell(ctx, table, key, column, value)
	if err != nil {
		slog.Error("更新单元格失败", "table", table, "column", column, "err", err)
		return 0, err
	}
	return n, nil
}

// GetCellBlob 读取单元格完整 BLOB 内容（V2.5：列表标记只有 64 KB 预览）。
func (s *DataService) GetCellBlob(connID string, table string, key map[string]any, column string) (domain.BinaryContent, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return domain.BinaryContent{}, err
	}
	content, err := repository.NewDataRepository(s.store, connID).GetCellBlob(ctx, table, key, column)
	if err != nil {
		slog.Error("读取单元格 BLOB 失败", "table", table, "column", column, "err", err)
		return domain.BinaryContent{}, err
	}
	return content, nil
}

// DeleteRows 在事务中删除多行，返回删除行数。
func (s *DataService) DeleteRows(connID string, table string, keys []map[string]any) (int64, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return 0, err
	}
	n, err := repository.NewDataRepository(s.store, connID).DeleteRows(ctx, table, keys)
	if err != nil {
		slog.Error("删除行失败", "table", table, "count", len(keys), "err", err)
		return 0, err
	}
	return n, nil
}

// DuplicateRows 在事务中复制选中的行（V2.5）。
// 单列 INTEGER 主键不参与复制（新行取新自增值）；其他主键/唯一
// 约束冲突时整体回滚并报错。
func (s *DataService) DuplicateRows(connID string, table string, keys []map[string]any) (int64, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return 0, err
	}
	n, err := repository.NewDataRepository(s.store, connID).DuplicateRows(ctx, table, keys)
	if err != nil {
		slog.Error("复制行失败", "table", table, "count", len(keys), "err", err)
		return 0, err
	}
	return n, nil
}
