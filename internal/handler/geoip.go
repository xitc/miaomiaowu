package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"miaomiaowu/internal/logger"
)

const (
	geoIPSuccessTTL = 24 * time.Hour
	geoIPFailureTTL = 10 * time.Minute
)

var (
	geoIPAPIBase = "https://api.ipinfo.io/lite"
	geoIPClient  = &http.Client{Timeout: 5 * time.Second}
	geoIPCache   sync.Map // map[string]geoIPCacheEntry
)

type geoIPInfo struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	ASN         string `json:"asn"`
	ASName      string `json:"as_name"`
}

type geoIPCacheEntry struct {
	info      geoIPInfo
	expiresAt time.Time
}

// getGeoIPCountryCode preserves the existing provider-filter contract while
// sharing the richer lookup and bounded cache used by subscription notices.
func getGeoIPCountryCode(ipOrHost string) string {
	return lookupGeoIPInfo(context.Background(), ipOrHost).CountryCode
}

func lookupGeoIPInfo(ctx context.Context, ipOrHost string) geoIPInfo {
	ipOrHost = strings.TrimSpace(ipOrHost)
	if ipOrHost == "" {
		return geoIPInfo{}
	}

	ip := net.ParseIP(ipOrHost)
	if ip == nil {
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, ipOrHost)
		if err != nil || len(addresses) == 0 {
			logger.Info("[GeoIP] 域名解析失败", "domain", ipOrHost, "error", err)
			return geoIPInfo{}
		}
		ip = addresses[0].IP
	}
	ipString := ip.String()

	if cached, ok := geoIPCache.Load(ipString); ok {
		entry := cached.(geoIPCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.info
		}
		geoIPCache.Delete(ipString)
	}

	endpoint := fmt.Sprintf("%s/%s?token=%s", strings.TrimRight(geoIPAPIBase, "/"), url.PathEscape(ipString), url.QueryEscape(ipInfoToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		cacheGeoIPResult(ipString, geoIPInfo{}, geoIPFailureTTL)
		return geoIPInfo{}
	}

	resp, err := geoIPClient.Do(req)
	if err != nil {
		logger.Info("[GeoIP] IP查询失败", "ip", ipString, "error", err)
		cacheGeoIPResult(ipString, geoIPInfo{}, geoIPFailureTTL)
		return geoIPInfo{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Info("[GeoIP] API返回错误状态", "ip", ipString, "status", resp.StatusCode)
		cacheGeoIPResult(ipString, geoIPInfo{}, geoIPFailureTTL)
		return geoIPInfo{}
	}

	var result geoIPInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Info("[GeoIP] 响应解析失败", "ip", ipString, "error", err)
		cacheGeoIPResult(ipString, geoIPInfo{}, geoIPFailureTTL)
		return geoIPInfo{}
	}

	result.IP = ipString
	result.CountryCode = strings.ToUpper(strings.TrimSpace(result.CountryCode))
	result.Country = strings.TrimSpace(result.Country)
	result.ASN = strings.TrimSpace(result.ASN)
	result.ASName = strings.TrimSpace(result.ASName)
	cacheGeoIPResult(ipString, result, geoIPSuccessTTL)
	logger.Info("[GeoIP] IP地理位置查询成功", "ip", ipString, "country", result.CountryCode, "asn", result.ASN)
	return result
}

func cacheGeoIPResult(ip string, info geoIPInfo, ttl time.Duration) {
	geoIPCache.Store(ip, geoIPCacheEntry{info: info, expiresAt: time.Now().Add(ttl)})
}
