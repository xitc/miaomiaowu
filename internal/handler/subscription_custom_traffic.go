package handler

import (
	"math"
	"time"

	"miaomiaowu/internal/storage"
)

const bytesPerGB = int64(1024 * 1024 * 1024)

// defaultCustomTrafficCycleDays is used when first enabling custom traffic with an
// expire date: billing start defaults to expire - N days (clamped not after now),
// so a sub with only a few days left shows most quota already consumed instead of ~0 used.
const defaultCustomTrafficCycleDays = 30

// defaultTrafficStartForExpire returns a billing-cycle start for an existing expire.
// start = expire - defaultCustomTrafficCycleDays; if that is still in the future, use now
// (new long-lived sub: burn across the full remaining window from now).
func defaultTrafficStartForExpire(expire, now time.Time) time.Time {
	expire = expire.UTC()
	now = now.UTC()
	start := expire.Add(-time.Duration(defaultCustomTrafficCycleDays) * 24 * time.Hour)
	if start.After(now) {
		return now
	}
	return start
}

// customTrafficPeriodStart picks the billing-cycle start for remaining-time simulation.
// Prefer TrafficStartAt; else if expire is set, derive from default cycle; else CreatedAt.
func customTrafficPeriodStart(file storage.SubscribeFile, now time.Time) time.Time {
	if file.TrafficStartAt != nil && !file.TrafficStartAt.IsZero() {
		return file.TrafficStartAt.UTC()
	}
	if file.ExpireAt != nil && !file.ExpireAt.IsZero() {
		return defaultTrafficStartForExpire(*file.ExpireAt, now)
	}
	if !file.CreatedAt.IsZero() {
		return file.CreatedAt.UTC()
	}
	return file.UpdatedAt.UTC()
}

// simulateTrafficUsedByRemainingTime burns custom traffic by remaining time fraction:
//
//	remaining = total * (expire - now) / (expire - start)
//	used      = total - remaining
//
// Rules:
//   - totalBytes <= 0 → 0
//   - no expire       → 0 used (only expose quota)
//   - now >= expire   → full totalBytes used
//   - now <= start    → 0 used
//   - invalid period  → 0
func simulateTrafficUsedByRemainingTime(totalBytes int64, start, expire time.Time, now time.Time) int64 {
	if totalBytes <= 0 {
		return 0
	}
	start = start.UTC()
	expire = expire.UTC()
	now = now.UTC()

	if expire.IsZero() {
		return 0
	}
	if !now.Before(expire) {
		return totalBytes
	}
	if start.IsZero() {
		return 0
	}
	if now.Before(start) {
		return 0
	}
	period := expire.Sub(start)
	if period <= 0 {
		return totalBytes
	}

	remainingDur := expire.Sub(now)
	if remainingDur < 0 {
		remainingDur = 0
	}
	if remainingDur > period {
		remainingDur = period
	}

	ratioRemaining := float64(remainingDur) / float64(period)
	if ratioRemaining < 0 {
		ratioRemaining = 0
	}
	if ratioRemaining > 1 {
		ratioRemaining = 1
	}

	remaining := int64(math.Round(float64(totalBytes) * ratioRemaining))
	if remaining < 0 {
		remaining = 0
	}
	if remaining > totalBytes {
		remaining = totalBytes
	}
	used := totalBytes - remaining
	if used < 0 {
		return 0
	}
	if used > totalBytes {
		return totalBytes
	}
	return used
}

// trafficLimitBytes converts a subscribe-file traffic_limit (GB) to bytes.
func trafficLimitBytes(limitGB *float64) int64 {
	if limitGB == nil || *limitGB <= 0 {
		return 0
	}
	return int64(math.Round(*limitGB * float64(bytesPerGB)))
}

// usesCustomTrafficSimulation reports whether this file should burn traffic by time %
// instead of probe/external used counters. Pure custom limit without stats servers.
func usesCustomTrafficSimulation(file storage.SubscribeFile) bool {
	return file.TrafficLimit != nil && *file.TrafficLimit > 0 && file.StatsServerIDs == ""
}

// ensureTrafficStartAt sets TrafficStartAt when enabling custom traffic without a start.
// With expire: start = expire - 30d (or now if that is in the future).
// Without expire: start = now.
func ensureTrafficStartAt(file *storage.SubscribeFile, now time.Time) bool {
	if file == nil || !usesCustomTrafficSimulation(*file) {
		return false
	}
	if file.TrafficStartAt != nil && !file.TrafficStartAt.IsZero() {
		return false
	}
	now = now.UTC()
	var start time.Time
	if file.ExpireAt != nil && !file.ExpireAt.IsZero() {
		start = defaultTrafficStartForExpire(*file.ExpireAt, now)
	} else {
		start = now
	}
	file.TrafficStartAt = &start
	return true
}

// resolveCustomSimulatedTraffic returns limit/used for pure custom remaining-time simulation.
func resolveCustomSimulatedTraffic(file storage.SubscribeFile, now time.Time) (limit, used int64, ok bool) {
	if !usesCustomTrafficSimulation(file) {
		return 0, 0, false
	}
	limit = trafficLimitBytes(file.TrafficLimit)
	if limit <= 0 {
		return 0, 0, false
	}
	now = now.UTC()
	start := customTrafficPeriodStart(file, now)
	var expire time.Time
	if file.ExpireAt != nil {
		expire = file.ExpireAt.UTC()
	}
	used = simulateTrafficUsedByRemainingTime(limit, start, expire, now)
	return limit, used, true
}
