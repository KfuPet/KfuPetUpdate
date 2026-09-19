// Package version 负责版本号的归一与比较。
//
// KfuPet 的版本号在几处流通：GitHub 发布版的 tag（可能带 v 前缀）、注册表里
// 记录的本地版本（统一存归一后的形式）、安装包文件名。比较必须在同一套规则下
// 进行，否则会出现「0.0.10 比 0.0.9 旧」这类字符串比较的错判。
package version

import (
	"strconv"
	"strings"
)

// Normalize 归一化版本号：去掉首尾空白与 v/V 前缀。
// 注册表与比较前都先过这一手，避免「带前缀」与「不带前缀」被当成两个版本。
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		return strings.TrimSpace(s[1:])
	}
	return s
}

// Compare 比较两个版本号：a 旧于 b 返回 -1，相同返回 0，a 新于 b 返回 1。
//
// 规则：
//   - 先归一化（去 v 前缀、去空白）；
//   - 按 "." 分段后逐段做数字比较，不能按字符串比（0.0.10 > 0.0.9）；
//   - 段数不等时缺段按 0 补（0.1 与 0.1.0 视为相同）；
//   - 段内数字之后还有内容（如 "0.0.9-beta"）视为预发布标记：数字部分相同时，
//     带标记的版本更小（预发布早于正式版）。
func Compare(a, b string) int {
	pa, preA := parse(a)
	pb, preB := parse(b)

	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}

	switch {
	case preA && !preB:
		return -1
	case !preA && preB:
		return 1
	default:
		return 0
	}
}

// Major 返回版本号的主版本段；解析不出数字时返回 0。
func Major(s string) int {
	parts, _ := parse(s)
	if len(parts) == 0 {
		return 0
	}
	return parts[0]
}

// parse 把版本号拆成数字段，并报告是否存在预发布标记。
// 遇到没有前导数字的段（如 "x"）即停止解析，其后的内容不再参与比较。
func parse(s string) (parts []int, prerelease bool) {
	for _, seg := range strings.Split(Normalize(s), ".") {
		seg = strings.TrimSpace(seg)
		digits := 0
		for digits < len(seg) && seg[digits] >= '0' && seg[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			break // 该段没有数字，后续无法按数字比较
		}
		n, err := strconv.Atoi(seg[:digits])
		if err != nil {
			break // 数字长到超出 int，按无法解析处理
		}
		parts = append(parts, n)
		if digits < len(seg) {
			prerelease = true // 数字之后还有内容（如 "-beta"）
			break
		}
	}
	return parts, prerelease
}
