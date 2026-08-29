package appdata

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/redjack/redsqlite/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{dir: dir}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)

	got := s.Settings()
	if got != domain.DefaultSettings() {
		t.Fatalf("默认设置错误: %+v", got)
	}

	updated, err := s.UpdateSettings(domain.Settings{
		PageSize: 50, QueryTimeoutMs: 5000, EnableWAL: true, MaxQueryRows: 100000,
	})
	if err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	if updated.PageSize != 50 || !updated.EnableWAL || updated.MaxQueryRows != 100000 {
		t.Fatalf("保存后设置错误: %+v", updated)
	}

	// 越界值回落默认
	updated, err = s.UpdateSettings(domain.Settings{PageSize: 5, QueryTimeoutMs: 10})
	if err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	if updated.PageSize != domain.DefaultSettings().PageSize {
		t.Errorf("非法 PageSize 应回落: %+v", updated)
	}
	if updated.EnableWAL {
		t.Errorf("未提供的 EnableWAL 应为 false: %+v", updated)
	}
}

func TestSettingsPersist(t *testing.T) {
	dir := t.TempDir()
	s1 := &Store{dir: dir}
	if _, err := s1.UpdateSettings(domain.Settings{PageSize: 200}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	s2 := &Store{dir: dir}
	if got := s2.Settings().PageSize; got != 200 {
		t.Fatalf("重新加载设置失败: %d", got)
	}
}

func TestRecent(t *testing.T) {
	s := newTestStore(t)

	a := filepath.Join(t.TempDir(), "a.db")
	b := filepath.Join(t.TempDir(), "b.db")

	s.AddRecent(a)
	s.AddRecent(b)
	s.AddRecent(a) // 去重并前置

	list := s.Recent()
	if len(list) != 2 || list[0].Path != a || list[1].Path != b {
		t.Fatalf("最近列表顺序错误: %+v", list)
	}

	s.RemoveRecent(b)
	list = s.Recent()
	if len(list) != 1 || list[0].Path != a {
		t.Fatalf("删除最近记录失败: %+v", list)
	}

	s.ClearRecent()
	if got := s.Recent(); len(got) != 0 {
		t.Fatalf("清空失败: %+v", got)
	}
}

func TestRecentCap(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	for i := 0; i < 15; i++ {
		s.AddRecent(filepath.Join(dir, fmt.Sprintf("f%d.db", i)))
	}
	if got := len(s.Recent()); got != maxRecentItems {
		t.Fatalf("最近列表应截断到 %d: %d", maxRecentItems, got)
	}
}

func TestNewUsesUserConfigDir(t *testing.T) {
	if _, err := New(); err != nil {
		t.Fatalf("New 失败: %v", err)
	}
}
