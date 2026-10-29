// Package repository 封装所有 SQLite 数据访问。
// 服务层不允许直接触碰 *sql.DB（文档 10.1）。
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/redjack/redsqlite/domain"

	// 纯 Go 实现的 SQLite 驱动（文档 4.2：避免 CGO，简化 Windows 构建）
	_ "modernc.org/sqlite"
)

// sqliteHeader SQLite 文件头（文档 6.1：打开前校验）。
const sqliteHeader = "SQLite format 3\x00"

// Conn 单个数据库连接：每连接一个 *sql.DB（SetMaxOpenConns(1)），
// 以及随连接关闭而取消的基础 context。
type Conn struct {
	id       string
	db       *sql.DB
	path     string
	readOnly bool
	baseCtx  context.Context
	cancel   context.CancelFunc
}

// Store 多连接注册表（V2.1）：可同时打开多个数据库文件，
// 每个连接保持"单文件、单连接"语义；同一路径复用同一连接，
// 避免两个连接指向同一文件造成的自锁 SQLITE_BUSY。
type Store struct {
	mu     sync.Mutex
	conns  map[string]*Conn
	byPath map[string]string // 绝对路径 -> connID
	seq    int
}

// NewStore 创建空 Store。
func NewStore() *Store {
	return &Store{
		conns:  map[string]*Conn{},
		byPath: map[string]string{},
	}
}

// pathKey 归一化绝对路径作为 byPath 复用索引的键：先解析符号链接，
// 再在大小写不敏感的文件系统（Windows）上折叠大小写。否则最近列表与
// 文件对话框返回的同一物理文件（大小写/符号链接写法不同）会被当成
// 两个路径各建一个连接，两个连接互相竞争文件锁导致 SQLITE_BUSY。
// 解析失败时退回原路径（保持旧行为）。c.path 仍保存用户选择的原始路径。
func pathKey(abs string) string {
	p := abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		p = resolved
	}
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// Open 校验并打开数据库文件（可多个并存），返回连接信息。
// enableWAL 为 true 时设置 journal_mode=WAL；forceReadOnly 为 true 时
// 无视文件可写性强制以只读打开。同一路径重复打开返回已有连接（Reused=true）。
func (s *Store) Open(path string, enableWAL bool, forceReadOnly bool) (domain.DatabaseInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return domain.DatabaseInfo{}, fmt.Errorf("解析路径失败 %q: %w", path, err)
	}

	st, err := os.Stat(abs)
	if err != nil {
		return domain.DatabaseInfo{}, fmt.Errorf("文件不存在: %s", abs)
	}
	if st.IsDir() {
		return domain.DatabaseInfo{}, fmt.Errorf("路径是目录而不是数据库文件: %s", abs)
	}

	if err := validateHeader(abs); err != nil {
		return domain.DatabaseInfo{}, err
	}

	// 同路径复用：模式可能不同（如已写模式打开后再请求只读），
	// 以现有连接为准，前端可据 Reused 提示。
	key := pathKey(abs)
	s.mu.Lock()
	if id, ok := s.byPath[key]; ok {
		c := s.conns[id]
		s.mu.Unlock()
		if c == nil {
			return domain.DatabaseInfo{}, ErrNotOpen
		}
		info, err := s.buildInfo(c)
		if err != nil {
			return domain.DatabaseInfo{}, err
		}
		info.Reused = true
		return info, nil
	}
	s.mu.Unlock()

	readOnly := forceReadOnly || !writable(abs)

	dsn, err := buildDSN(abs, readOnly, enableWAL)
	if err != nil {
		return domain.DatabaseInfo{}, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return domain.DatabaseInfo{}, fmt.Errorf("打开数据库失败 %q: %w", abs, err)
	}
	// 文档 10.2：单连接便于控制事务与锁（每连接独立）
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// 连通性检查
	var version string
	if err := db.QueryRow(`SELECT sqlite_version()`).Scan(&version); err != nil {
		db.Close()
		return domain.DatabaseInfo{}, fmt.Errorf("打开数据库失败（文件可能已损坏）: %w", err)
	}

	s.mu.Lock()
	// 双重检查：并发打开同一路径（注册表不一致时 existing 可能为 nil，
	// 走下方全新注册路径，避免对 nil 连接取信息）
	if id, ok := s.byPath[key]; ok {
		if existing := s.conns[id]; existing != nil {
			s.mu.Unlock()
			db.Close()
			info, err := s.buildInfo(existing)
			if err != nil {
				return domain.DatabaseInfo{}, err
			}
			info.Reused = true
			return info, nil
		}
	}
	s.seq++
	c := &Conn{
		id:       fmt.Sprintf("c%d", s.seq),
		db:       db,
		path:     abs,
		readOnly: readOnly,
	}
	c.baseCtx, c.cancel = context.WithCancel(context.Background())
	s.conns[c.id] = c
	s.byPath[key] = c.id
	s.mu.Unlock()

	return s.buildInfo(c)
}

// CloseConn 关闭指定连接并取消其运行中的查询。
func (s *Store) CloseConn(id string) error {
	s.mu.Lock()
	c, ok := s.conns[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotOpen
	}
	delete(s.conns, id)
	if key := pathKey(c.path); s.byPath[key] == id {
		delete(s.byPath, key)
	}
	s.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	return c.db.Close()
}

// Close 关闭全部连接（应用退出）。
func (s *Store) Close() error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	var firstErr error
	for _, id := range ids {
		if err := s.CloseConn(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Count 返回当前打开的连接数。
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// lookup 返回指定连接。
func (s *Store) lookup(id string) (*Conn, error) {
	s.mu.Lock()
	c, ok := s.conns[id]
	s.mu.Unlock()
	if !ok {
		return nil, ErrNotOpen
	}
	return c, nil
}

// DB 返回指定连接的底层 *sql.DB，不存在时报错。调用方不得 Close 或替换连接。
func (s *Store) DB(id string) (*sql.DB, error) {
	c, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return c.db, nil
}

// Context 返回随连接关闭而取消的基础 context（文档 7.5：
// 连接关闭时取消其所有运行中查询）。
func (s *Store) Context(id string) (context.Context, error) {
	c, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return c.baseCtx, nil
}

// Path 返回连接的数据库绝对路径，未打开时为空串。
func (s *Store) Path(id string) string {
	c, err := s.lookup(id)
	if err != nil {
		return ""
	}
	return c.path
}

// ReadOnly 返回连接是否只读，未打开时按只读处理。
func (s *Store) ReadOnly(id string) bool {
	c, err := s.lookup(id)
	if err != nil {
		return true
	}
	return c.readOnly
}

// Info 返回连接信息（重新查询版本与 PRAGMA）。
func (s *Store) Info(id string) (domain.DatabaseInfo, error) {
	c, err := s.lookup(id)
	if err != nil {
		return domain.DatabaseInfo{}, err
	}
	return s.buildInfo(c)
}

// buildInfo 汇总连接的描述信息。
func (s *Store) buildInfo(c *Conn) (domain.DatabaseInfo, error) {
	var version string
	if err := c.db.QueryRow(`SELECT sqlite_version()`).Scan(&version); err != nil {
		return domain.DatabaseInfo{}, fmt.Errorf("读取 SQLite 版本失败: %w", err)
	}

	journal, encoding := "", ""
	pageSize := int64(0)
	_ = c.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal)
	_ = c.db.QueryRow(`PRAGMA encoding`).Scan(&encoding)
	_ = c.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)

	size := int64(0)
	if st, err := os.Stat(c.path); err == nil {
		size = st.Size()
	}

	return domain.DatabaseInfo{
		ConnID:       c.id,
		Path:         c.path,
		FileName:     filepath.Base(c.path),
		Size:         size,
		SQLiteVer:    version,
		ReadOnly:     c.readOnly,
		JournalMode:  journal,
		EncodingMode: encoding,
		PageSize:     pageSize,
	}, nil
}

// Backup 用 VACUUM INTO 把连接的数据库在线备份到 dest（V2.5）。
// 目标文件不能已存在（SQLite 拒绝覆盖）；VACUUM 不能在事务内运行，
// 这里走普通 Exec，单连接串行执行。只读连接也允许备份（只读源）。
func (s *Store) Backup(ctx context.Context, connID string, dest string) (domain.BackupResult, error) {
	db, err := s.DB(connID)
	if err != nil {
		return domain.BackupResult{}, err
	}
	if strings.TrimSpace(dest) == "" {
		return domain.BackupResult{}, fmt.Errorf("缺少备份目标路径")
	}
	if _, err := os.Stat(dest); err == nil {
		return domain.BackupResult{}, fmt.Errorf("目标文件已存在，备份不会覆盖已有文件：%s", dest)
	}

	start := time.Now()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return domain.BackupResult{}, fmt.Errorf("备份数据库失败: %w", err)
	}

	result := domain.BackupResult{Path: dest, ElapsedMs: time.Since(start).Milliseconds()}
	if st, err := os.Stat(dest); err == nil {
		result.Size = st.Size()
	}
	return result, nil
}

func validateHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("无法读取文件: %w", err)
	}
	defer f.Close()

	header := make([]byte, len(sqliteHeader))
	n, readErr := io.ReadFull(f, header)
	if n == 0 {
		return nil // 0 字节文件：SQLite 按新数据库初始化
	}
	if readErr != nil || string(header) != sqliteHeader {
		return fmt.Errorf("不是有效的 SQLite 数据库文件: %s", path)
	}
	return nil
}

func writable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// buildDSN 构造 modernc.org/sqlite 的 file: URI DSN，
// 通过 _pragma 参数保证连接级 PRAGMA 生效（文档 6.1）。
func buildDSN(path string, readOnly bool, enableWAL bool) (string, error) {
	slashPath := "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	u := url.URL{Scheme: "file", Path: slashPath}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	if enableWAL {
		q.Add("_pragma", "journal_mode(WAL)")
	}
	if readOnly {
		q.Add("mode", "ro")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
