// Package appdata 管理应用配置目录中的 JSON 持久化数据
// （设置、最近打开记录），位于用户 AppData 目录（文档 4.2）。
package appdata

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/redjack/redsqlite/domain"
)

const (
	appDirName     = "RedSQLite"
	settingsFile   = "settings.json"
	recentFile     = "recent.json"
	maxRecentItems = 10
)

// Store 线程安全地管理设置与最近打开记录。
type Store struct {
	mu      sync.Mutex
	dir     string
	s       domain.Settings
	recent  []domain.RecentEntry
	loaded  bool
	loadErr error
}

// New 返回基于默认配置目录的 Store。
func New() (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir 返回应用配置目录（%APPDATA%\RedSQLite）。
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	return filepath.Join(base, appDirName), nil
}

func (s *Store) ensureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

func (s *Store) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.s = domain.DefaultSettings()

	if data, err := os.ReadFile(filepath.Join(s.dir, settingsFile)); err == nil {
		var loaded domain.Settings
		if json.Unmarshal(data, &loaded) == nil {
			s.s = mergeSettings(loaded)
		}
	}

	if data, err := os.ReadFile(filepath.Join(s.dir, recentFile)); err == nil {
		var loaded []domain.RecentEntry
		if json.Unmarshal(data, &loaded) == nil {
			s.recent = loaded
		}
	}
}

func mergeSettings(s domain.Settings) domain.Settings {
	d := domain.DefaultSettings()
	if s.PageSize >= 10 && s.PageSize <= 1000 {
		d.PageSize = s.PageSize
	}
	if s.QueryTimeoutMs >= 1000 && s.QueryTimeoutMs <= 600000 {
		d.QueryTimeoutMs = s.QueryTimeoutMs
	}
	if s.MaxQueryRows >= 100 && s.MaxQueryRows <= 1000000 {
		d.MaxQueryRows = s.MaxQueryRows
	}
	d.EnableWAL = s.EnableWAL
	d.Theme = domain.ValidTheme(s.Theme)
	return d
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Settings 返回当前设置。
func (s *Store) Settings() domain.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.s
}

// UpdateSettings 校验并保存设置。
func (s *Store) UpdateSettings(next domain.Settings) (domain.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if err := s.ensureDir(); err != nil {
		return s.s, fmt.Errorf("创建配置目录失败: %w", err)
	}
	s.s = mergeSettings(next)
	err := writeJSON(filepath.Join(s.dir, settingsFile), s.s)
	return s.s, err
}

// Recent 返回最近打开列表（新的在前）。
func (s *Store) Recent() []domain.RecentEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	out := make([]domain.RecentEntry, len(s.recent))
	copy(out, s.recent)
	return out
}

// AddRecent 记录一次打开并去重、截断。
func (s *Store) AddRecent(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	entry := domain.RecentEntry{
		Path:     path,
		FileName: filepath.Base(path),
		OpenedAt: nowISO(),
	}
	list := make([]domain.RecentEntry, 0, maxRecentItems)
	list = append(list, entry)
	for _, e := range s.recent {
		if !samePath(e.Path, path) {
			list = append(list, e)
		}
	}
	if len(list) > maxRecentItems {
		list = list[:maxRecentItems]
	}
	s.recent = list
	s.persistRecent(list)
}

// RemoveRecent 从最近列表删除一条。
func (s *Store) RemoveRecent(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	list := make([]domain.RecentEntry, 0, len(s.recent))
	for _, e := range s.recent {
		if !samePath(e.Path, path) {
			list = append(list, e)
		}
	}
	s.recent = list
	s.persistRecent(list)
}

// ClearRecent 清空最近列表。
func (s *Store) ClearRecent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	s.recent = nil
	s.persistRecent([]domain.RecentEntry{})
}

// persistRecent 把最近列表落盘；失败只记日志（最近列表非关键数据，
// 磁盘满/权限问题时不应阻塞打开数据库的主流程）。
func (s *Store) persistRecent(list []domain.RecentEntry) {
	if err := s.ensureDir(); err != nil {
		slog.Warn("创建配置目录失败，最近列表未保存", "dir", s.dir, "err", err)
		return
	}
	path := filepath.Join(s.dir, recentFile)
	if err := writeJSON(path, list); err != nil {
		slog.Warn("保存最近列表失败", "path", path, "err", err)
	}
}
