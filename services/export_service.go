package services

import (
	"fmt"
	"log/slog"

	"github.com/redjack/redsqlite/internal/exporter"
	"github.com/redjack/redsqlite/internal/identifier"
	"github.com/redjack/redsqlite/internal/sqliteutil"
	"github.com/redjack/redsqlite/repository"
)

// ExportService 导出 CSV / JSON（文档 6.6）。
type ExportService struct {
	store *repository.Store
}

// NewExportService 创建导出服务。
func NewExportService(store *repository.Store) *ExportService {
	return &ExportService{store: store}
}

// PickExportPath 弹出原生保存对话框。defaultName 不含扩展名。
// 用户取消时返回空字符串。
func (s *ExportService) PickExportPath(defaultName string, format string) (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	ext, filterName := "*.csv", "CSV 文件"
	if format == exporter.FormatJSON {
		ext, filterName = "*.json", "JSON 文件"
	} else if format == exporter.FormatXLSX {
		ext, filterName = "*.xlsx", "Excel 文件"
	}
	path, err := app.Dialog.SaveFile().
		SetMessage("导出数据").
		SetFilename(defaultName).
		AddFilter(filterName, ext).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// ExportQueryResult 重新执行查询并流式导出全部结果（不受行数限制）。
func (s *ExportService) ExportQueryResult(connID string, sqlText string, format string, path string) (exporter.Result, error) {
	normalized, err := sqliteutil.ValidateSingle(sqlText)
	if err != nil {
		return exporter.Result{}, err
	}
	if sqliteutil.Classify(normalized) != sqliteutil.KindQuery {
		return exporter.Result{}, fmt.Errorf("仅支持导出 SELECT / PRAGMA / EXPLAIN 查询结果")
	}

	db, err := s.store.DB(connID)
	if err != nil {
		return exporter.Result{}, err
	}
	ctx, err := s.store.Context(connID)
	if err != nil {
		return exporter.Result{}, err
	}
	result, err := exporter.Export(ctx, db, normalized, format, path)
	if err != nil {
		slog.Error("导出查询结果失败", "path", path, "err", err)
		return exporter.Result{}, err
	}
	slog.Info("已导出查询结果", "path", path, "rows", result.Rows)
	return result, nil
}

// ExportTable 流式导出整张表数据。
func (s *ExportService) ExportTable(connID string, table string, format string, path string) (exporter.Result, error) {
	quoted, err := identifier.ValidateAndQuote(table)
	if err != nil {
		return exporter.Result{}, fmt.Errorf("表名非法: %w", err)
	}
	db, err := s.store.DB(connID)
	if err != nil {
		return exporter.Result{}, err
	}
	ctx, err := s.store.Context(connID)
	if err != nil {
		return exporter.Result{}, err
	}
	result, err := exporter.Export(ctx, db, fmt.Sprintf("SELECT * FROM %s", quoted), format, path)
	if err != nil {
		slog.Error("导出表失败", "table", table, "path", path, "err", err)
		return exporter.Result{}, err
	}
	slog.Info("已导出表", "table", table, "path", path, "rows", result.Rows)
	return result, nil
}
