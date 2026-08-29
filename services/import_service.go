package services

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/redjack/redsqlite/internal/csvimport"
	"github.com/redjack/redsqlite/internal/tabular"
	"github.com/redjack/redsqlite/internal/xlsximport"
	"github.com/redjack/redsqlite/repository"
)

// ImportService 数据导入向导（V1.2 CSV；V2.5 增加 Excel .xlsx）：
// 选文件 → 预览 → 列映射 → 事务导入。Excel 先选工作表。
type ImportService struct {
	store *repository.Store
}

// NewImportService 创建导入服务。
func NewImportService(store *repository.Store) *ImportService {
	return &ImportService{store: store}
}

// PickImportFile 弹出原生文件选择对话框（CSV / Excel），
// 返回所选绝对路径；用户取消时返回空字符串。
func (s *ImportService) PickImportFile() (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	path, err := app.Dialog.OpenFile().
		SetTitle("选择要导入的表格文件").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("CSV / Excel 文件", "*.csv;*.txt;*.tsv;*.xlsx;*.xlsm").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// ListSheets 列出 Excel 工作簿的工作表（CSV 返回错误）。
func (s *ImportService) ListSheets(path string) ([]xlsximport.SheetInfo, error) {
	sheets, err := xlsximport.Sheets(path)
	if err != nil {
		slog.Error("列出工作表失败", "path", path, "err", err)
		return nil, err
	}
	return sheets, nil
}

// PreviewImport 预览表格前 50 行。按扩展名路由：.xlsx/.xlsm 走 Excel
// （sheet 为空时用第一个工作表），其余走 CSV。hasHeader 为 true 时首行作为列名。
func (s *ImportService) PreviewImport(path string, sheet string, hasHeader bool) (tabular.Preview, error) {
	var (
		preview tabular.Preview
		err     error
	)
	if isExcelPath(path) {
		preview, err = xlsximport.Parse(path, sheet, hasHeader)
	} else {
		preview, err = csvimport.Parse(path, hasHeader)
	}
	if err != nil {
		slog.Error("预览导入文件失败", "path", path, "sheet", sheet, "err", err)
		return tabular.Preview{}, err
	}
	return preview, nil
}

// Import 按列映射把表格数据导入目标表（单个事务，失败整体回滚）。
func (s *ImportService) Import(connID string, req tabular.ImportRequest) (tabular.Result, error) {
	if s.store.ReadOnly(connID) {
		return tabular.Result{}, &simpleError{"数据库以只读方式打开，无法导入"}
	}
	db, err := s.store.DB(connID)
	if err != nil {
		return tabular.Result{}, err
	}
	ctx, err := s.store.Context(connID)
	if err != nil {
		return tabular.Result{}, err
	}
	var result tabular.Result
	if isExcelPath(req.Path) {
		result, err = xlsximport.Import(ctx, db, req)
	} else {
		result, err = csvimport.Import(ctx, db, req)
	}
	if err != nil {
		slog.Error("导入数据失败", "path", req.Path, "sheet", req.Sheet, "table", req.Table, "err", err)
		return tabular.Result{}, err
	}
	slog.Info("已导入数据", "path", req.Path, "sheet", req.Sheet, "table", req.Table, "inserted", result.Inserted)
	return result, nil
}

// isExcelPath 按扩展名判断是否走 Excel 导入管线。
func isExcelPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return true
	}
	return false
}
