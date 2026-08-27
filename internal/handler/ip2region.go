package handler

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"miaomiaowu/internal/logger"
)

const defaultIP2RegionV4Path = "/app/provider-geoip/ip2region_v4.xdb"

type ip2RegionRecord struct {
	Country     string
	Province    string
	City        string
	ISP         string
	CountryCode string
}

type ip2RegionDatabase struct {
	mu       sync.Mutex
	path     string
	size     int64
	modTime  time.Time
	searcher *xdb.Searcher
}

var notificationIP2RegionDB ip2RegionDatabase

func lookupIP2RegionLocation(ipString string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(ipString))
	if ip == nil || ip.To4() == nil {
		return "", false
	}

	raw, err := notificationIP2RegionDB.search(ip.String())
	if err != nil {
		logger.Warn("[IP2Region] 查询失败，降级到在线 GeoIP", "ip", ip.String(), "error", err)
		return "", false
	}
	record, ok := parseIP2RegionRecord(raw)
	if !ok {
		return "", false
	}
	location := formatIP2RegionLocation(record)
	return location, location != ""
}

func (db *ip2RegionDatabase) search(ip string) (string, error) {
	path := strings.TrimSpace(os.Getenv("IP2REGION_XDB_PATH"))
	if path == "" {
		path = defaultIP2RegionV4Path
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if db.searcher == nil || db.path != path || db.size != info.Size() || !db.modTime.Equal(info.ModTime()) {
		searcher, loadedInfo, loadErr := loadIP2RegionDatabase(path)
		if loadErr != nil {
			return "", loadErr
		}
		db.path = path
		db.size = loadedInfo.Size()
		db.modTime = loadedInfo.ModTime()
		db.searcher = searcher
		logger.Info("[IP2Region] IPv4 数据库已加载", "path", path, "size", loadedInfo.Size(), "modified_at", loadedInfo.ModTime())
	}

	return db.searcher.Search(ip)
}

func loadIP2RegionDatabase(path string) (*xdb.Searcher, os.FileInfo, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open xdb: %w", err)
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat xdb: %w", err)
	}
	if err := xdb.Verify(handle); err != nil {
		return nil, nil, fmt.Errorf("verify xdb: %w", err)
	}
	content, err := xdb.LoadContent(handle)
	if err != nil {
		return nil, nil, fmt.Errorf("load xdb: %w", err)
	}
	searcher, err := xdb.NewWithBuffer(xdb.IPv4, content)
	if err != nil {
		return nil, nil, fmt.Errorf("create xdb searcher: %w", err)
	}
	return searcher, info, nil
}

func parseIP2RegionRecord(raw string) (ip2RegionRecord, bool) {
	parts := strings.Split(strings.TrimSpace(raw), "|")
	for len(parts) < 5 {
		parts = append(parts, "")
	}
	record := ip2RegionRecord{
		Country:     cleanIP2RegionField(parts[0]),
		Province:    cleanIP2RegionField(parts[1]),
		City:        cleanIP2RegionField(parts[2]),
		ISP:         cleanIP2RegionField(parts[3]),
		CountryCode: strings.ToUpper(cleanIP2RegionField(parts[4])),
	}
	return record, record.Country != "" || record.Province != "" || record.City != "" || record.ISP != ""
}

func cleanIP2RegionField(value string) string {
	value = strings.TrimSpace(value)
	if value == "0" {
		return ""
	}
	return value
}

func formatIP2RegionLocation(record ip2RegionRecord) string {
	fields := []string{record.Country, record.Province, record.City, record.ISP}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			result = append(result, field)
		}
	}
	return strings.Join(result, " ")
}
