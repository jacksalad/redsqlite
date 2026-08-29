package services

import (
	"path/filepath"
	"testing"
)

func TestFileServiceRoundtrip(t *testing.T) {
	svc := NewFileService()
	dir := t.TempDir()

	// 写入 -> 读回
	path := filepath.Join(dir, "blob.bin")
	if err := svc.WriteFileBase64(path, "M2EyK3c9PQ=="); err != nil { // "3a2+w==" 的 Base64 再编码
		t.Fatalf("写入失败: %v", err)
	}
	content, err := svc.ReadFileBase64(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if content.Size != 7 {
		t.Errorf("大小错误: %d", content.Size)
	}

	// 目录自动创建
	nested := filepath.Join(dir, "a", "b", "c.bin")
	if err := svc.WriteFileBase64(nested, ""); err != nil {
		t.Fatalf("嵌套目录写入失败: %v", err)
	}
	if content, err := svc.ReadFileBase64(nested); err != nil || content.Size != 0 {
		t.Errorf("空文件读取错误: %+v %v", content, err)
	}

	// 空路径 / 目录路径 / 不存在的文件
	if err := svc.WriteFileBase64("  ", "QUJD"); err == nil {
		t.Error("空保存路径应报错")
	}
	if _, err := svc.ReadFileBase64("  "); err == nil {
		t.Error("空读取路径应报错")
	}
	if _, err := svc.ReadFileBase64(dir); err == nil {
		t.Error("目录路径应报错")
	}
	if _, err := svc.ReadFileBase64(filepath.Join(dir, "nope.bin")); err == nil {
		t.Error("不存在的文件应报错")
	}

	// 非法 Base64
	if err := svc.WriteFileBase64(filepath.Join(dir, "bad.bin"), "!!!"); err == nil {
		t.Error("非法 Base64 应报错")
	}

}
