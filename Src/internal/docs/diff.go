package docs

import "strings"

// DiffText 用行比较两段文字, 给冲突页看.
func DiffText(from, to string) string {
	a := splitLines(from)
	b := splitLines(to)
	match := pair(a, b)
	var out []string
	j := 0
	for i := 0; i < len(a); i++ {
		if match[i] < 0 {
			out = append(out, "-"+a[i])
			continue
		}
		for j < match[i] {
			out = append(out, "+"+b[j])
			j++
		}
		out = append(out, " "+a[i])
		j = match[i] + 1
	}
	for j < len(b) {
		out = append(out, "+"+b[j])
		j++
	}
	return strings.Join(out, "\n")
}

// Merge 做一次三方合并. 两边改了同一处时写入冲突标记.
func Merge(base, mine, theirs string) string {
	b := splitLines(base)
	m := splitLines(mine)
	t := splitLines(theirs)
	bm := pair(b, m)
	bt := pair(b, t)
	type point struct{ b, m, t int }
	points := []point{{-1, -1, -1}}
	for i := range b {
		if bm[i] >= 0 && bt[i] >= 0 {
			points = append(points, point{i, bm[i], bt[i]})
		}
	}
	points = append(points, point{len(b), len(m), len(t)})
	var out []string
	for k := 0; k < len(points)-1; k++ {
		prev := points[k]
		next := points[k+1]
		baseChunk := b[prev.b+1 : next.b]
		mineChunk := m[prev.m+1 : next.m]
		theirChunk := t[prev.t+1 : next.t]
		switch {
		case sameLines(baseChunk, mineChunk) && sameLines(baseChunk, theirChunk):
			out = append(out, baseChunk...)
		case sameLines(baseChunk, mineChunk):
			out = append(out, theirChunk...)
		case sameLines(baseChunk, theirChunk):
			out = append(out, mineChunk...)
		case sameLines(mineChunk, theirChunk):
			out = append(out, mineChunk...)
		default:
			out = append(out, "<<<<<<< 我的修改")
			out = append(out, mineChunk...)
			out = append(out, "=======")
			out = append(out, theirChunk...)
			out = append(out, ">>>>>>> 对方的修改")
		}
		if next.b < len(b) {
			out = append(out, b[next.b])
		}
	}
	return strings.Join(out, "\n")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pair(base, side []string) []int {
	n, m := len(base), len(side)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if base[i] == side[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	i, j := 0, 0
	for i < n && j < m {
		if base[i] == side[j] && dp[i][j] == dp[i+1][j+1]+1 {
			out[i] = j
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return out
}
