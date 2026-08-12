package handler

import (
	"math"
	"time"

	"miaomiaowu/internal/storage"
)

const bytesPerGB = int64(1024 * 1024 * 1024)

// calendarDaysCeil returns whole calendar days covering d, rounding partial days up.
// Zero or negative duration yields 0.
func calendarDaysCeil(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	// 1ns .. 24h => 1 day
	const day = 24 * time.Hour
	days := int(d / day)
	if d%day != 0 {
		days++
	}
	return days
}

// customTrafficPeriodStart picks the billing-cycle start for remaining-day simulation.
// Prefer TrafficStartAt (set when custom traffic is enabled); fall back to CreatedAt.
func customTrafficPeriodStart(file storage.SubscribeFile) time.Time {
	if file.TrafficStartAt != nil && !file.TrafficStartAt.IsZero() {
		return *file.TrafficStartAt
	}
	if !file.CreatedAt.IsZero() {
		return file.CreatedAt
	}
	return file.UpdatedAt
}

// simulateTrafficUsedByRemainingDays burns custom traffic by remaining calendar days:
//
//	remaining = total * remainingDays / totalDays
//	used      = total - remaining
//
// totalDays  = ceil((expire - start) / 1d)
// remainingDays = ceil((expire - now) / 1d) while not expired
//
// Rules:
//   - totalBytes <= 0        → 0
//   - no expire              → 0 used (only expose quota)
//   - now >= expire          → full totalBytes used
//   - now <= start           → 0 used
//   - invalid period         → 0
func simulateTrafficUsedByRemainingDays(totalBytes int64, start, expire time.Time, now time.Time) int64 {
	if totalBytes <= 0 {
		return 0
	}
	if expire.IsZero() {
		return 0
	}
	if !now.Before(expire) {
		return totalBytes
	}
	if start.IsZero() {
		// Without a start we cannot form a period; keep full remaining until expire.
		return 0
	}
	if now.Before(start) {
		return 0
	}
	if !expire.After(start) {
		return totalBytes
	}

	totalDays := calendarDaysCeil(expire.Sub(start))
	if totalDays <= 0 {
		return 0
	}
	remainingDays := calendarDaysCeil(expire.Sub(now))
	if remainingDays < 0 {
		remainingDays = 0
	}
	if remainingDays > totalDays {
		remainingDays = totalDays
	}

	remaining := int64(math.Round(float64(totalBytes) * float64(remainingDays) / float64(totalDays)))
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

// usesCustomTrafficSimulation reports whether this file should burn traffic by day %
// instead of probe/external used counters. Pure custom limit without stats servers.
func usesCustomTrafficSimulation(file storage.SubscribeFile) bool {
	return file.TrafficLimit != nil && *file.TrafficLimit > 0 && file.StatsServerIDs == ""
}

// ensureTrafficStartAt sets TrafficStartAt to now when enabling custom traffic without a start.
// Returns true if the file was modified and should be persisted.
func ensureTrafficStartAt(file *storage.SubscribeFile, now time.Time) bool {
	if file == nil || !usesCustomTrafficSimulation(*file) {
		return false
	}
	if file.TrafficStartAt != nil && !file.TrafficStartAt.IsZero() {
		return false
	}
	t := now.UTC()
	file.TrafficStartAt = &t
	return true
}

// resolveCustomSimulatedTraffic returns limit/used for pure custom remaining-day simulation.
// ok is true when the file has a custom traffic_limit suitable for simulation.
func resolveCustomSimulatedTraffic(file storage.SubscribeFile, now time.Time) (limit, used int64, ok bool) {
	if !usesCustomTrafficSimulation(file) {
		return 0, 0, false
	}
	limit = trafficLimitBytes(file.TrafficLimit)
	if limit <= 0 {
		return 0, 0, false
	}
	start := customTrafficPeriodStart(file)
	var expire time.Time
	if file.ExpireAt != nil {
		expire = *file.ExpireAt
	}
	used = simulateTrafficUsedByRemainingDays(limit, start, expire, now)
	return limit, used, true
}
