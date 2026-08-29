// Package cell 负责数据库单元格值与 JSON 传输值之间的转换：
// - 服务端 -> 前端：BLOB 与超长文本转为带标记的对象（文档 13.3）
// - 前端 -> 服务端：编辑值还原为 database/sql 驱动参数
package cell

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// 展示限制（文档 13.3）。
const (
	MaxTextDisplayBytes = 1 << 20 // 单个单元格文本最大展示 1 MB
	MaxBlobPreviewBytes = 1 << 16 // BLOB 预览最多返回 64 KB 原始数据
)

// MarkerKind 标记对象的类型字段。
const (
	KindBlob      = "blob"
	KindTruncated = "text-truncated"
	// KindBigint V3.0：超出 JS 安全整数范围（±2^53-1）的 int64，
	// 以十进制字符串传输，避免 JSON 数字在 JS 侧丢精度。
	KindBigint = "bigint"
)

// MaxSafeInteger JS Number.MAX_SAFE_INTEGER（2^53-1），超出该范围的
// int64 无法被 JS 精确表示，Encode 时转为 bigint 标记。
const MaxSafeInteger = int64(1)<<53 - 1

// Marker 前端可识别的特殊值标记。
type Marker struct {
	Kind      string `json:"__kind"`
	Size      int    `json:"size,omitempty"`
	B64       string `json:"b64,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Value     string `json:"value,omitempty"`
	Str       string `json:"str,omitempty"` // bigint 标记的十进制字符串
}

// Encode 把驱动返回的单元格值转为 JSON 安全的展示值。
func Encode(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		n := len(t)
		preview := t
		if n > MaxBlobPreviewBytes {
			preview = t[:MaxBlobPreviewBytes]
		}
		return Marker{
			Kind: KindBlob,
			Size: n,
			B64:  base64.StdEncoding.EncodeToString(preview),
		}
	case string:
		if len(t) > MaxTextDisplayBytes {
			return Marker{
				Kind:      KindTruncated,
				Size:      len(t),
				Truncated: true,
				Value:     t[:MaxTextDisplayBytes],
			}
		}
		return t
	case bool:
		return t
	case int64:
		// 超出 JS 安全整数范围的整数以十进制字符串传输（V3.0 精度修复）
		if t > MaxSafeInteger || t < -MaxSafeInteger {
			return Marker{Kind: KindBigint, Str: strconv.FormatInt(t, 10)}
		}
		return t
	case int:
		return Encode(int64(t))
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return int64(t)
		}
		return t
	case float32:
		return float64(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ErrInvalidValue 前端传入的值无法转换为数据库参数。
var ErrInvalidValue = errors.New("无效的单元格值")

// Decode 把前端 JSON 反序列化出的编辑值还原为驱动参数。
// 支持 nil、布尔、整数、浮点、字符串以及 BLOB 标记对象。
func Decode(v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case bool:
		return t, nil
	case float64:
		// JSON 数字统一为 float64；整数值转 int64 保持整数列类型
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return int64(t), nil
		}
		return t, nil
	case string:
		return t, nil
	case map[string]any:
		kind, _ := t["__kind"].(string)
		if kind == KindBlob {
			b64, _ := t["b64"].(string)
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return nil, fmt.Errorf("解码 BLOB 数据失败: %w", err)
			}
			return data, nil
		}
		if kind == KindBigint {
			str, _ := t["str"].(string)
			n, err := strconv.ParseInt(strings.TrimSpace(str), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%w: 大整数值无效 %q", ErrInvalidValue, str)
			}
			return n, nil
		}
		return nil, ErrInvalidValue
	default:
		return nil, ErrInvalidValue
	}
}
