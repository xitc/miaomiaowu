package handler

import (
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestSimulateTrafficUsedByDays(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) // 31 days
	total := int64(100 * bytesPerGB)

	t.Run("midpoint roughly half", func(t *testing.T) {
		now := start.Add(15 * 24 * time.Hour)
		used := simulateTrafficUsedByDays(total, start, expire, now)
		// 15/31 ≈ 48.4%
		ratio := float64(used) / float64(total)
		if ratio < 0.47 || ratio > 0.50 {
			t.Fatalf("mid ratio = %v, used=%d", ratio, used)
		}
	})

	t.Run("before start is zero", func(t *testing.T) {
		used := simulateTrafficUsedByDays(total, start, expire, start.Add(-time.Hour))
		if used != 0 {
			t.Fatalf("used=%d", used)
		}
	})

	t.Run("at expire is full", func(t *testing.T) {
		used := simulateTrafficUsedByDays(total, start, expire, expire)
		if used != total {
			t.Fatalf("used=%d want %d", used, total)
		}
	})

	t.Run("after expire is full", func(t *testing.T) {
		used := simulateTrafficUsedByDays(total, start, expire, expire.Add(time.Hour))
		if used != total {
			t.Fatalf("used=%d want %d", used, total)
		}
	})

	t.Run("no expire yields zero used", func(t *testing.T) {
		used := simulateTrafficUsedByDays(total, start, time.Time{}, start.Add(10*24*time.Hour))
		if used != 0 {
			t.Fatalf("used=%d", used)
		}
	})

	t.Run("zero total", func(t *testing.T) {
		if simulateTrafficUsedByDays(0, start, expire, start.Add(time.Hour)) != 0 {
			t.Fatal("expected 0")
		}
	})
}

func TestResolveCustomSimulatedTraffic(t *testing.T) {
	limit := 200.0
	created := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	file := storage.SubscribeFile{
		TrafficLimit:  &limit,
		CreatedAt:     created,
		ExpireAt:      &expire,
		StatsServerIDs: "",
	}
	now := created.Add(15 * 24 * time.Hour)
	gotLimit, gotUsed, ok := resolveCustomSimulatedTraffic(file, now)
	if !ok {
		t.Fatal("expected ok")
	}
	if gotLimit != 200*bytesPerGB {
		t.Fatalf("limit=%d", gotLimit)
	}
	if gotUsed <= 0 || gotUsed >= gotLimit {
		t.Fatalf("used=%d not between 0 and limit", gotUsed)
	}

	// stats servers disable pure custom simulation
	file.StatsServerIDs = "s1"
	if _, _, ok := resolveCustomSimulatedTraffic(file, now); ok {
		t.Fatal("expected disabled when stats servers set")
	}
}

func TestUsesCustomTrafficSimulation(t *testing.T) {
	limit := 10.0
	zero := 0.0
	if !usesCustomTrafficSimulation(storage.SubscribeFile{TrafficLimit: &limit}) {
		t.Fatal("expected true")
	}
	if usesCustomTrafficSimulation(storage.SubscribeFile{TrafficLimit: &zero}) {
		t.Fatal("expected false for zero limit")
	}
	if usesCustomTrafficSimulation(storage.SubscribeFile{TrafficLimit: &limit, StatsServerIDs: "a"}) {
		t.Fatal("expected false with stats servers")
	}
}
