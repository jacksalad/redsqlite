package services

import (
	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/appdata"
)

// RecentService 最近打开的数据库（文档 3.2）。
type RecentService struct {
	apps *appdata.Store
}

// NewRecentService 创建最近记录服务。
func NewRecentService(apps *appdata.Store) *RecentService {
	return &RecentService{apps: apps}
}

// List 返回最近打开列表（新的在前）。
func (s *RecentService) List() []domain.RecentEntry {
	list := s.apps.Recent()
	if list == nil {
		list = []domain.RecentEntry{}
	}
	return list
}

// Remove 删除一条最近记录。
func (s *RecentService) Remove(path string) {
	s.apps.RemoveRecent(path)
}

// Clear 清空最近记录。
func (s *RecentService) Clear() {
	s.apps.ClearRecent()
}
