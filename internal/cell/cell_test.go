package cell

import (
	"encoding/json"
	"testing"
)

// TestEncodeBigint 验证超出 JS 安全整数范围的 int64 转为 bigint 标记，
// 安全范围内保持数字（V3.0 精度修复）。
func TestEncodeBigint(t *testing.T) {
	cases := []struct {
		name string
		in   int64
	}{
		{"上界内", MaxSafeInteger},
		{"下界内", -MaxSafeInteger},
		{"常规值", 42},
		{"上界外", MaxSafeInteger + 1},
		{"下界外", -MaxSafeInteger - 1},
		{"int64 最大", 1<<63 - 1},
		{"int64 最小", -1 << 63},
	}
	for _, c := range cases {
		out := Encode(c.in)
		if c.in >= -MaxSafeInteger && c.in <= MaxSafeInteger {
			if v, ok := out.(int64); !ok || v != c.in {
				t.Errorf("%s: 期望原样 int64 %d, 得到 %#v", c.name, c.in, out)
			}
			continue
		}
		m, ok := out.(Marker)
		if !ok {
			t.Errorf("%s: 期望 Marker, 得到 %#v", c.name, out)
			continue
		}
		if m.Kind != KindBigint {
			t.Errorf("%s: Kind = %q, 期望 %q", c.name, m.Kind, KindBigint)
		}
		if want := jsonInt64(c.in); m.Str != want {
			t.Errorf("%s: Str = %q, 期望 %q", c.name, m.Str, want)
		}
	}
}

func jsonInt64(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestEncodeBigintJSONRoundtrip 标记对象经 JSON 序列化后字段名正确。
func TestEncodeBigintJSONRoundtrip(t *testing.T) {
	const big = int64(9007199254740993) // 2^53 + 1
	data, err := json.Marshal(Encode(big))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if m["__kind"] != KindBigint {
		t.Fatalf("__kind = %v, 期望 %q", m["__kind"], KindBigint)
	}
	if m["str"] != "9007199254740993" {
		t.Fatalf("str = %v", m["str"])
	}
	// 模拟前端原样回传编辑值：Decode 还原为 int64
	v, err := Decode(m)
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if v != big {
		t.Fatalf("Decode 得到 %#v, 期望 %d", v, big)
	}
}

// TestDecodeBigintInvalid 非法 bigint 字符串必须报错。
func TestDecodeBigintInvalid(t *testing.T) {
	cases := []map[string]any{
		{"__kind": KindBigint, "str": "not-a-number"},
		{"__kind": KindBigint, "str": "9223372036854775808"}, // int64 溢出
		{"__kind": KindBigint},
	}
	for _, m := range cases {
		if _, err := Decode(m); err == nil {
			t.Errorf("期望报错: %#v", m)
		}
	}
}

// TestDecodeBasic 其余类型解码不受影响。
func TestDecodeBasic(t *testing.T) {
	if v, err := Decode(nil); err != nil || v != nil {
		t.Errorf("nil 解码错误: %v %v", v, err)
	}
	if v, err := Decode(float64(3)); err != nil || v != int64(3) {
		t.Errorf("整数浮点解码错误: %v %v", v, err)
	}
	if v, err := Decode("文本"); err != nil || v != "文本" {
		t.Errorf("字符串解码错误: %v %v", v, err)
	}
	if _, err := Decode(map[string]any{"__kind": "unknown"}); err == nil {
		t.Error("未知标记应报错")
	}
}
