package handler

import (
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestCalendarDaysCeil(t *testing.T) {
	if calendarDaysCeil(0) != 0 {
		t.Fatal("zero")
	}
	if calendarDaysCeil(time.Hour) != 1 {
		t.Fatal("partial day")
	}
	if calendarDaysCeil(24*time.Hour) != 1 {
		t.Fatal("exact day")
	}
	if calendarDaysCeil(24*time.Hour+time.Second) != 2 {
		t.Fatal("day+eps")
	}
}

func TestSimulateTrafficUsedByRemainingDays(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) // 30 days
	total := int64(300 * bytesPerGB)

	t.Run("midpoint half remaining", func(t *testing.T) {
		// 15 days left of 30 => remaining 50% => used 50%
		now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
		used := simulateTrafficUsedByRemainingDays(total, start, expire, now)
		ratio := float64(used) / float64(total)
		if ratio < 0.48 || ratio > 0.52 {
			t.Fatalf("ratio=%v used=%d", ratio, used)
		}
	})

	t.Run("just started nearly full remaining", func(t *testing.T) {
		now := start.Add(time.Hour)
		used := simulateTrafficUsedByRemainingDays(total, start, expire, now)
		// remaining days ~30/30 => used ~0
		if used > total/10 {
			t.Fatalf("used too high at start: %d", used)
		}
	})

	t.Run("one day left", func(t *testing.T) {
		now := expire.Add(-12 * time.Hour)
		used := simulateTrafficUsedByRemainingDays(total, start, expire, now)
		// remainingDays=1, totalDays=30 => remaining=10GB, used=290GB
		remaining := total - used
		wantRem := int64(mathRound(float64(total) * 1 / 30))
		if remaining != wantRem {
			t.Fatalf("remaining=%d want %d used=%d", remaining, wantRem, used)
		}
	})

	t.Run("at expire full", func(t *testing.T) {
		if simulateTrafficUsedByRemainingDays(total, start, expire, expire) != total {
			t.Fatal("expected full at expire")
		}
	})

	t.Run("no expire zero used", func(t *testing.T) {
		if simulateTrafficUsedByRemainingDays(total, start, time.Time{}, start.Add(10*24*time.Hour)) != 0 {
			t.Fatal("expected 0")
		}
	})
}

func mathRound(v float64) int64 {
	return int64(v + 0.5)
}

func TestResolveCustomSimulatedTrafficUsesTrafficStartAt(t *testing.T) {
	limit := 200.0
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	createdLongAgo := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	file := storage.SubscribeFile{
		TrafficLimit:   &limit,
		TrafficStartAt: &start,
		CreatedAt:      createdLongAgo,
		ExpireAt:       &expire,
	}
	now := start.Add(24 * time.Hour)
	gotLimit, gotUsed, ok := resolveCustomSimulatedTraffic(file, now)
	if !ok || gotLimit != 200*bytesPerGB {
		t.Fatalf("ok=%v limit=%d", ok, gotLimit)
	}
	// Should NOT use createdLongAgo (which would burn most traffic)
	if float64(gotUsed)/float64(gotLimit) > 0.2 {
		t.Fatalf("used ratio too high with traffic_start_at: %d/%d", gotUsed, gotLimit)
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
