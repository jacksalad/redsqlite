package identifier

import (
	"errors"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", `"users"`},
		{`a"b`, `"a""b"`},
		{"中文表", `"中文表"`},
		{"order", `"order"`},
	}
	for _, c := range cases {
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := []string{"users", "order", "user_1", "中文表", "tbl-name", "a b", "t.x", "col$"}
	for _, name := range ok {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) 不应报错: %v", name, err)
		}
	}

	bad := []string{"", "   ", `a"b`, "a;b", "x'y", "a\nb", "col(", "col)", "a\tb"}
	for _, name := range bad {
		if err := Validate(name); !errors.Is(err, ErrInvalidCharacter) && !errors.Is(err, ErrEmpty) {
			t.Errorf("Validate(%q) 应报错，实际: %v", name, err)
		}
	}
}

func TestValidateAndQuote(t *testing.T) {
	quoted, err := ValidateAndQuote("users")
	if err != nil || quoted != `"users"` {
		t.Fatalf("ValidateAndQuote(users) = %q, %v", quoted, err)
	}
	if _, err := ValidateAndQuote(`u"m`); err == nil {
		t.Fatal("含双引号的标识符应被拒绝")
	}
}
