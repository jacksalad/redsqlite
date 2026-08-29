package domain

// BinaryContent 二进制内容（Base64 编码）：BLOB 单元格查看与文件读写共用（V2.5）。
type BinaryContent struct {
	Size int64  `json:"size"`
	B64  string `json:"b64"`
}
