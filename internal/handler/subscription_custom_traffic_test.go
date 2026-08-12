package handler

import (
	"math"
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

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

	t.Run("a few hours into first day is non-zero", func(t *testing.T) {
		now := start.Add(6 * time.Hour)
		used := simulateTrafficUsedByRemainingTime(total, start, expire, now)
		if used <= 0 {
			t.Fatalf("expected non-zero used within first day, got %d", used)
		}
		// ~6h / 30d ≈ 0.83%
		ratio := float64(used) / float64(total)
		if ratio < 0.005 || ratio > 0.02 {
			t.Fatalf("unexpected early ratio %v", ratio)
		}
	})

	t.Run("just started tiny used", func(t *testing.T) {
		now := start.Add(time.Minute)
		used := simulateTrafficUsedByRemainingTime(total, start, expire, now)
		if used < 0 || used > total/1000 {
			t.Fatalf("used=%d", used)
		}
	})

	t.Run("one day left", func(t *testing.T) {
		now := expire.Add(-24 * time.Hour)
		used := simulateTrafficUsedByRemainingTime(total, start, expire, now)
		// remaining 1/30 => used 29/30
		want := int64(math.Round(float64(total) * 29 / 30))
		if used != want {
			t.Fatalf("used=%d want %d", used, want)
		}
	})

	t.Run("at expire full", func(t *testing.T) {
		if simulateTrafficUsedByRemainingTime(total, start, expire, expire) != total {
			t.Fatal("expected full at expire")
		}
	})

	t.Run("no expire zero used", func(t *testing.T) {
		if simulateTrafficUsedByRemainingTime(total, start, time.Time{}, start.Add(10*24*time.Hour)) != 0 {
			t.Fatal("expected 0")
		}
	})
}

func TestResolveCustomSimulatedTrafficUsesTrafficStartAt(t *testing.T) {
	limit := 200.0
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	createdLongAgo := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	file := storage.SubscribeFile{
		TrafficLimit:   &limit,
		TrafficStartAt: &start,
		CreatedAt:      createdLongAgo,
		ExpireAt:       &expire,
	}
	now := start.Add(15 * 24 * time.Hour)
	gotLimit, gotUsed, ok := resolveCustomSimulatedTraffic(file, now)
	if !ok || gotLimit != 200*bytesPerGB {
		t.Fatalf("ok=%v limit=%d", ok, gotLimit)
	}
	ratio := float64(gotUsed) / float64(gotLimit)
	if ratio < 0.45 || ratio > 0.55 {
		t.Fatalf("expected ~half used with traffic_start_at mid cycle, ratio=%v", ratio)
	}
}

func TestEnsureTrafficStartAt(t *testing.T) {
	limit := 10.0
	file := storage.SubscribeFile{TrafficLimit: &limit}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if !ensureTrafficStartAt(&file, now) || file.TrafficStartAt == nil {
		t.Fatal("expected set")
	}
	if ensureTrafficStartAt(&file, now.Add(time.Hour)) {
		t.Fatal("should not reset existing start")
	}
}
