package tabular

import "testing"

func TestDedupeColumns(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"a", "b"}, []string{"a", "b"}},
		{[]string{"a", "a", "a"}, []string{"a", "a_2", "a_3"}},
		// 生成名 a_2 撞上真实列名 a_2：必须继续避让，不得输出重复名
		{[]string{"a", "a", "a_2"}, []string{"a", "a_2", "a_2_2"}},
		// 真实列名先占坑，后到的重复名绕开它
		{[]string{"name_2", "name", "name"}, []string{"name_2", "name", "name_3"}},
		// 空列名补 c{i}，补出的名字同样参与去重
		{[]string{"", "x", ""}, []string{"c1", "x", "c3"}},
		{[]string{"c2", "", ""}, []string{"c2", "c2_2", "c3"}},
		{[]string{"  a  ", "a"}, []string{"a", "a_2"}},
	}
	for _, c := range cases {
		got := DedupeColumns(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("DedupeColumns(%v) = %v, want %v", c.in, got, c.want)
		}
		seen := map[string]bool{}
		for i, name := range got {
			if name != c.want[i] {
				t.Errorf("DedupeColumns(%v)[%d] = %q, want %q", c.in, i, name, c.want[i])
			}
			if seen[name] {
				t.Errorf("DedupeColumns(%v) 输出重复名 %q", c.in, name)
			}
			seen[name] = true
		}
	}
}
