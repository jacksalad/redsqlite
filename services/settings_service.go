package services

import (
	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/appdata"
)

// SettingsService 应用设置（文档 3.2）。
type SettingsService struct {
	apps *appdata.Store
}

// NewSettingsService 创建设置服务。
func NewSettingsService(apps *appdata.Store) *SettingsService {
	return &SettingsService{apps: apps}
}

// Get 返回当前设置。
func (s *SettingsService) Get() domain.Settings {
	return s.apps.Settings()
}

// Update 校验并保存设置，返回合并后的有效设置。
func (s *SettingsService) Update(next domain.Settings) (domain.Settings, error) {
	return s.apps.UpdateSettings(next)
}
