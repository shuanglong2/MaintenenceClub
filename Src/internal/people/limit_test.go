package people

import (
	"testing"
	"time"
)

func TestLimiterBackoff(t *testing.T) {
	lim := NewLimiter()
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	if lim.Rejected("a", now) != 0 {
		t.Fatal("一开始不应限制")
	}
	lim.Fail("a", now)
	if lim.Rejected("a", now.Add(time.Second)) != 0 {
		t.Fatal("第一次失败等待 1 秒, 到点后应放行")
	}
	if got := lim.Rejected("a", now.Add(500*time.Millisecond)); got < 400*time.Millisecond {
		t.Fatalf("等待过短: %s", got)
	}
	lim.Fail("a", now)
	if got := lim.Rejected("a", now); got < 1500*time.Millisecond {
		t.Fatalf("第二次应等待约 2 秒, 得到 %s", got)
	}
	lim.Success("a")
	if lim.Rejected("a", now) != 0 {
		t.Fatal("成功后应清除")
	}
}
