package handler

import (
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestDefaultTrafficStartForExpire(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

	// Expire in 7 days → start = expire - 30d (23 days ago)
	expire7 := now.Add(7 * 24 * time.Hour)
	start := defaultTrafficStartForExpire(expire7, now)
	want := expire7.Add(-30 * 24 * time.Hour)
	if !start.Equal(want) {
		t.Fatalf("7d left: start=%v want %v", start, want)
	}

	// Expire in 40 days → start clamped to now (full remaining window from now)
	expire40 := now.Add(40 * 24 * time.Hour)
	start = defaultTrafficStartForExpire(expire40, now)
	if !start.Equal(now) {
		t.Fatalf("40d left: start=%v want now", start)
	}
}

func TestSimulateTrafficUsedByRemainingTime(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) // 30 days
	total := int64(300 * bytesPerGB)

	t.Run("midpoint half used", func(t *testing.T) {
		now := start.Add(15 * 24 * time.Hour)
		used := simulateTrafficUsedByRemainingTime(total, start, expire, now)
		ratio := float64(used) / float64(total)
		if ratio < 0.49 || ratio > 0.51 {
			t.Fatalf("ratio=%v used=%d", ratio, used)
		}
	})

	t.Run("seven days left of thirty day cycle", func(t *testing.T) {
		// Matches No.003 shape: 30d package, 7d remaining → used ~76.7%
		now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
		expire := now.Add(7 * 24 * time.Hour)
		start := defaultTrafficStartForExpire(expire, now)
		limit := int64(500 * bytesPerGB)
		used := simulateTrafficUsedByRemainingTime(limit, start, expire, now)
		ratio := float64(used) / float64(limit)
		if ratio < 0.75 || ratio > 0.78 {
			t.Fatalf("ratio=%v used_gb=%v start=%v", ratio, float64(used)/float64(bytesPerGB), start)
		}
	})

	t.Run("at expire full", func(t *testing.T) {
		if simulateTrafficUsedByRemainingTime(total, start, expire, expire) != total {
			t.Fatal("expected full at expire")
		}
	})
}

func TestResolveCustomWithDerivedStart(t *testing.T) {
	limit := 500.0
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)
	expire := now.Add(7 * 24 * time.Hour)
	// No TrafficStartAt — should derive expire-30d
	file := storage.SubscribeFile{
		TrafficLimit: &limit,
		ExpireAt:     &expire,
	}
	gotLimit, gotUsed, ok := resolveCustomSimulatedTraffic(file, now)
	if !ok || gotLimit != 500*bytesPerGB {
		t.Fatalf("ok=%v limit=%d", ok, gotLimit)
	}
	ratio := float64(gotUsed) / float64(gotLimit)
	if ratio < 0.75 || ratio > 0.78 {
		t.Fatalf("expected ~76%% used for 7d left of 30d cycle, ratio=%v used_gb=%v", ratio, float64(gotUsed)/float64(bytesPerGB))
	}
}

func TestEnsureTrafficStartAtUsesExpireCycle(t *testing.T) {
	limit := 10.0
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	expire := now.Add(3 * 24 * time.Hour)
	file := storage.SubscribeFile{TrafficLimit: &limit, ExpireAt: &expire}
	if !ensureTrafficStartAt(&file, now) || file.TrafficStartAt == nil {
		t.Fatal("expected set")
	}
	want := defaultTrafficStartForExpire(expire, now)
	if !file.TrafficStartAt.Equal(want) {
		t.Fatalf("start=%v want %v", file.TrafficStartAt, want)
	}
	// already set — no change
	if ensureTrafficStartAt(&file, now.Add(time.Hour)) {
		t.Fatal("should not reset")
	}
}
