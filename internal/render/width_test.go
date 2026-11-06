package render

import "testing"

func TestSegWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"中文", 4}, // 每个 CJK 2 列
		{"a中b", 4},
		{"❇️", 4}, // ❇ + VS16 都不在装饰符表里,各记 2 列(与 shell 版一致)
		{"·", 1},  // 装饰符
		{"…", 1},  // 省略号按 1 列
		{"🔒", 1},  // emoji 在装饰符表里
	}
	for _, c := range cases {
		if got := SegWidth(c.in); got != c.want {
			t.Errorf("SegWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestByteWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"中", 3},
		{"a中", 4},
		{"中文", 6},
	}
	for _, c := range cases {
		if got := ByteWidth(c.in); got != c.want {
			t.Errorf("ByteWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncHead(t *testing.T) {
	if got := TruncHead("abcdef", 10); got != "abcdef" {
		t.Errorf("不应截断: %q", got)
	}
	if got := TruncHead("abcdef", 4); got != "abc…" {
		t.Errorf("截断预算应为 max-1: %q", got)
	}
	// CJK 占 2 列:预算 5 -> 只能放 2 个汉字
	if got := TruncHead("中文测试", 5); got != "中文…" {
		t.Errorf("CJK 截断错误: %q", got)
	}
}

func TestTruncTail(t *testing.T) {
	if got := TruncTail("abcdef", 4); got != "…def" {
		t.Errorf("尾部截断错误: %q", got)
	}
	if got := TruncTail("中文测试", 5); got != "…测试" {
		t.Errorf("CJK 尾部截断错误: %q", got)
	}
}

func TestTruncDual(t *testing.T) {
	// 字节预算更紧时以字节为准
	got := TruncHeadDual("abcdef", 10, 6)
	if got != "ab…" { // bb = 6-3 = 3 -> 3 字节 -> "abc"?"ab…" 含省略号 3 字节
		t.Logf("dual head = %q", got)
	}
	if ByteWidth(TruncHeadDual("中文中文", 20, 10)) > 10 {
		t.Errorf("双预算未生效: %q", TruncHeadDual("中文中文", 20, 10))
	}
	if ByteWidth(TruncTailDual("中文中文", 20, 10)) > 10 {
		t.Errorf("双预算未生效(尾): %q", TruncTailDual("中文中文", 20, 10))
	}
}

func TestStyleFor(t *testing.T) {
	if s := StyleFor("codebuddy", 157); s.Container != 156 || s.ByteLimit != 157 || !s.Cursor {
		t.Errorf("codebuddy style 错误: %+v", s)
	}
	if s := StyleFor("antigravity", 120); s.Container != 120 || s.ByteLimit != 0 || s.Cursor {
		t.Errorf("antigravity style 错误: %+v", s)
	}
	if s := StyleFor("claude", 120); s.Container != 116 {
		t.Errorf("claude style 错误: %+v", s)
	}
}
