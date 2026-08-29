// Package logging 配置全局 slog 落盘日志（V1.1）。
// 日志写入 %APPDATA%\RedSQLite\logs\redsqlite.log（同时输出到 stdout，
// 便于开发模式查看）。用户报告问题时可直接附上该文件作为诊断材料。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/redjack/redsqlite/internal/appdata"
)

const (
	logFileName   = "redsqlite.log"
	maxLogBytes   = 5 << 20 // 单文件超过 5MB 时在启动阶段轮转
	keepRotations = 3       // 最多保留 3 个轮转文件
)

// Setup 初始化全局默认 logger 并打印启动横幅，返回关闭函数（应用退出时调用）。
// 文件不可写时降级为仅 stdout，不阻塞启动。
func Setup(appVersion string) (func(), error) {
	logDir, err := logDir()
	if err != nil {
		slog.SetDefault(stdoutLogger())
		return func() {}, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.SetDefault(stdoutLogger())
		return func() {}, fmt.Errorf("创建日志目录失败: %w", err)
	}

	path := filepath.Join(logDir, logFileName)
	rotateIfLarge(path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.SetDefault(stdoutLogger())
		return func() {}, fmt.Errorf("打开日志文件失败: %w", err)
	}

	handler := slog.NewTextHandler(io.MultiWriter(os.Stdout, f), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))

	slog.Info("RedSQLite 启动",
		"version", appVersion,
		"go", runtime.Version(),
		"os", runtime.GOOS+"/"+runtime.GOARCH,
	)
	slog.Info("日志文件", "path", path)

	return func() {
		slog.Info("RedSQLite 退出")
		_ = f.Close()
	}, nil
}

func stdoutLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func logDir() (string, error) {
	base, err := appdata.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "logs"), nil
}

// rotateIfLarge 启动时把超限日志轮转为 redsqlite-<时间戳>.log，仅保留最近几个。
// 运行期不再轮转：本应用日志量小，5MB 上限 + 启动轮转已够用。
func rotateIfLarge(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < maxLogBytes {
		return
	}
	dir := filepath.Dir(path)
	// 毫秒粒度：同秒内二次启动不至于撞名（Rename 失败会静默丢轮转）
	stamp := time.Now().Format("20060102-150405.000")
	_ = os.Rename(path, filepath.Join(dir, fmt.Sprintf("%s-%s.log", strings.TrimSuffix(logFileName, ".log"), stamp)))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var rotated []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, strings.TrimSuffix(logFileName, ".log")+"-") && strings.HasSuffix(name, ".log") {
			rotated = append(rotated, name)
		}
	}
	sort.Strings(rotated)
	for len(rotated) > keepRotations {
		_ = os.Remove(filepath.Join(dir, rotated[0]))
		rotated = rotated[1:]
	}
}
