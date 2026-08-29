package services

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/redjack/redsqlite/domain"
)

// maxFileTransferBytes 文件读写的大小上限（与 BLOB 在线查看一致，V2.5）。
const maxFileTransferBytes = int64(32 << 20)

// FileService BLOB 增强的文件读写与对话框（V2.5）：
// 前端 WebView 无本地文件系统权限，二进制内容经 Base64 走绑定传输。
type FileService struct{}

// NewFileService 创建文件服务。
func NewFileService() *FileService {
	return &FileService{}
}

// PickBlobOpenPath 弹出文件选择对话框（图片优先）；
// 用户取消时返回空字符串。
func (s *FileService) PickBlobOpenPath() (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	path, err := app.Dialog.OpenFile().
		SetTitle("选择要写入单元格的文件").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("图片", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp;*.ico").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// PickBlobSavePath 弹出保存对话框。defaultName 建议带扩展名；
// 用户取消时返回空字符串。
func (s *FileService) PickBlobSavePath(defaultName string) (string, error) {
	app := applicationGet()
	if app == nil {
		return "", errApplicationNotReady
	}
	path, err := app.Dialog.SaveFile().
		SetMessage("保存 BLOB 数据").
		SetFilename(defaultName).
		AddFilter("所有文件", "*.*").
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", wrapDialogError(err)
	}
	return path, nil
}

// ReadFileBase64 读取文件内容并 Base64 编码返回（供「从文件替换」BLOB）。
func (s *FileService) ReadFileBase64(path string) (domain.BinaryContent, error) {
	if strings.TrimSpace(path) == "" {
		return domain.BinaryContent{}, fmt.Errorf("缺少文件路径")
	}
	st, err := os.Stat(path)
	if err != nil {
		return domain.BinaryContent{}, fmt.Errorf("无法读取文件: %w", err)
	}
	if st.IsDir() {
		return domain.BinaryContent{}, fmt.Errorf("路径是目录，不是文件")
	}
	if st.Size() > maxFileTransferBytes {
		return domain.BinaryContent{}, fmt.Errorf("文件超过 %d MB，不支持写入单元格", maxFileTransferBytes/(1<<20))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.BinaryContent{}, fmt.Errorf("读取文件失败: %w", err)
	}
	slog.Info("已读取文件", "path", path, "size", st.Size())
	return domain.BinaryContent{Size: int64(len(data)), B64: base64.StdEncoding.EncodeToString(data)}, nil
}

// WriteFileBase64 把 Base64 内容解码写入文件（供「另存为」）。
// 目标已存在时直接覆盖（保存对话框已由用户确认）。
func (s *FileService) WriteFileBase64(path string, b64 string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("缺少保存路径")
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("解码数据失败: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建保存目录失败: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	slog.Info("已写入文件", "path", path, "size", len(data))
	return nil
}
