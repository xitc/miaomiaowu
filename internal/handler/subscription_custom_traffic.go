package handler

import (
	"math"
	"time"

	"miaomiaowu/internal/storage"
)

const bytesPerGB = int64(1024 * 1024 * 1024)

// customTrafficPeriodStart picks the cycle start for day-percentage simulation.
// Prefer CreatedAt; fall back to UpdatedAt when CreatedAt is zero.
func customTrafficPeriodStart(file storage.SubscribeFile) time.Time {
	if !file.CreatedAt.IsZero() {
		return file.CreatedAt
	}
	return file.UpdatedAt
}

// simulateTrafficUsedByDays linearly deducts custom traffic by elapsed days.
//
//	used = total * clamp((now - start) / (expire - start), 0, 1)
//
// Rules:
//   - totalBytes <= 0  → 0
//   - no expire        → 0 (only show quota, no time-based burn)
//   - now <= start     → 0
//   - now >= expire    → totalBytes
//   - invalid period   → 0
func simulateTrafficUsedByDays(totalBytes int64, start, expire time.Time, now time.Time) int64 {
	if totalBytes <= 0 {
		return 0
	}
	if expire.IsZero() {
		return 0
	}
	if !start.IsZero() && !now.Before(start) && !expire.After(start) {
		// expire == start or expire before start: treat as fully used once past expire
		if !now.Before(expire) {
			return totalBytes
		}
		return 0
	}
	if start.IsZero() {
		// Without a start, only clamp at expire boundary.
		if !now.Before(expire) {
			return totalBytes
		}
		return 0
	}
	if now.Before(start) {
		return 0
	}
	if !now.Before(expire) {
		return totalBytes
	}

	period := expire.Sub(start).Seconds()
	if period <= 0 {
		return 0
	}
	elapsed := now.Sub(start).Seconds()
	if elapsed <= 0 {
		return 0
	}
	ratio := elapsed / period
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	used := int64(math.Round(float64(totalBytes) * ratio))
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

// resolveCustomSimulatedTraffic returns limit/used for pure custom day simulation.
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
	used = simulateTrafficUsedByDays(limit, start, expire, now)
	return limit, used, true
}
