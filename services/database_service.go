// Package services 是暴露给前端的 Wails 服务层（文档 7）。
package services

import (
	"log/slog"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/appdata"
	"github.com/redjack/redsqlite/repository"
)

// DatabaseService 多连接的打开、关闭与状态（V2.1）。
type DatabaseService struct {
	store    *repository.Store
	settings *appdata.Store
	recent   *appdata.Store
}

// NewDatabaseService 创建数据库服务。
func NewDatabaseService(store *repository.Store, settings *appdata.Store) *DatabaseService {
	return &DatabaseService{
		store:    store,
		settings: settings,
		recent:   settings,
	}
}

// PickDatabaseFile 弹出原生文件选择对话框，返回所选绝对路径；
// 用户取消时返回空字符串。
func (s *DatabaseService) PickDatabaseFile() (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	path, err := app.Dialog.OpenFile().
		SetTitle("打开 SQLite 数据库").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("SQLite 数据库", "*.db;*.sqlite;*.sqlite3;*.db3").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// Open 打开数据库文件（可多个并存）并记录最近打开，返回连接信息。
// forceReadOnly 为 true 时强制以只读模式打开。
// 同一路径重复打开时复用已有连接（info.reused = true）。
func (s *DatabaseService) Open(path string, forceReadOnly bool) (domain.DatabaseInfo, error) {
	info, err := s.store.Open(path, s.settings.Settings().EnableWAL, forceReadOnly)
	if err != nil {
		slog.Error("打开数据库失败", "path", path, "readOnly", forceReadOnly, "err", err)
		return domain.DatabaseInfo{}, err
	}
	if info.Reused {
		slog.Info("复用已打开的数据库连接", "connID", info.ConnID, "path", info.Path)
		return info, nil
	}
	slog.Info("已打开数据库", "connID", info.ConnID, "path", info.Path,
		"readOnly", info.ReadOnly, "sqlite", info.SQLiteVer)
	s.recent.AddRecent(info.Path)
	return info, nil
}

// Close 关闭指定连接。
func (s *DatabaseService) Close(connID string) error {
	err := s.store.CloseConn(connID)
	if err != nil {
		slog.Error("关闭数据库连接失败", "connID", connID, "err", err)
	}
	return err
}

// GetInfo 返回指定连接的数据库信息。
func (s *DatabaseService) GetInfo(connID string) (domain.DatabaseInfo, error) {
	return s.store.Info(connID)
}

// PickBackupPath 弹出备份保存对话框（V2.5）。defaultName 建议带 .db 扩展名；
// 用户取消时返回空字符串。
func (s *DatabaseService) PickBackupPath(defaultName string) (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	path, err := app.Dialog.SaveFile().
		SetMessage("备份数据库").
		SetFilename(defaultName).
		AddFilter("SQLite 数据库", "*.db;*.sqlite;*.sqlite3").
		AddFilter("所有文件", "*.*").
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// BackupDatabase 用 VACUUM INTO 把连接的数据库在线备份到 path（V2.5）。
// 生成紧凑的独立副本；目标文件不能已存在。只读连接同样可以备份。
func (s *DatabaseService) BackupDatabase(connID string, path string) (domain.BackupResult, error) {
	ctx, err := s.store.Context(connID)
	if err != nil {
		return domain.BackupResult{}, err
	}
	result, err := s.store.Backup(ctx, connID, path)
	if err != nil {
		slog.Error("备份数据库失败", "connID", connID, "path", path, "err", err)
		return domain.BackupResult{}, err
	}
	slog.Info("已备份数据库", "connID", connID, "path", result.Path,
		"size", result.Size, "elapsedMs", result.ElapsedMs)
	return result, nil
}

// ServiceShutdown 应用退出时关闭全部连接（Wails 生命周期）。
func (s *DatabaseService) ServiceShutdown() error {
	return s.store.Close()
}
