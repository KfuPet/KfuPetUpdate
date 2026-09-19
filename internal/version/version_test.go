package version

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.0.9", "0.0.9"},
		{"v0.0.9", "0.0.9"},
		{"V0.0.9", "0.0.9"},
		{"  v1.2  ", "1.2"},
		{"", ""},
		{"v", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// 数字比较，不能按字符串比
		{"0.0.10", "0.0.9", 1},
		{"0.0.9", "0.0.10", -1},
		// v 前缀归一后可比
		{"v0.0.10", "0.0.9", 1},
		{"0.0.9", "v0.0.9", 0},
		// 段数不等：缺段按 0 补
		{"0.1", "0.1.0", 0},
		{"0.1.1", "0.1", 1},
		// 预发布标记：数字相同时正式版更新
		{"0.0.9", "0.0.9-beta", 1},
		{"0.0.9-beta", "0.0.9", -1},
		{"0.0.9-beta", "0.0.9-beta", 0},
		{"0.0.9-beta", "0.0.8", 1},
		// 本地比线上新（远端 < 本地）
		{"0.0.10", "0.0.9", 1},
		// 主版本差异
		{"1.0.0", "0.9.9", 1},
		{"0.9.9", "1.0.0", -1},
		// 空值按 0.0.0 处理
		{"", "0.0.1", -1},
		{"0.0.0", "", 0},
		{"未知", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d，期望 %d（对称性）", c.b, c.a, got, -c.want)
		}
	}
}

func TestMajor(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"8.0.31", 8},
		{"v8.0.31", 8},
		{"", 0},
		{"未知", 0},
	}
	for _, c := range cases {
		if got := Major(c.in); got != c.want {
			t.Errorf("Major(%q) = %d，期望 %d", c.in, got, c.want)
		}
	}
}
