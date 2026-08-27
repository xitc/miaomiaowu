package handler

import (
	"os"
	"testing"
)

func TestParseAndFormatIP2RegionLocation(t *testing.T) {
	record, ok := parseIP2RegionRecord("中国|江苏省|苏州市|联通|CN")
	if !ok {
		t.Fatal("parseIP2RegionRecord() returned not found")
	}
	if got := formatIP2RegionLocation(record); got != "中国 江苏省 苏州市 联通" {
		t.Fatalf("formatIP2RegionLocation() = %q", got)
	}
	if record.CountryCode != "CN" {
		t.Fatalf("CountryCode = %q", record.CountryCode)
	}
}

func TestParseIP2RegionLocationDropsEmptyPlaceholderFields(t *testing.T) {
	record, ok := parseIP2RegionRecord("中国|0|0|联通|CN")
	if !ok {
		t.Fatal("parseIP2RegionRecord() returned not found")
	}
	if got := formatIP2RegionLocation(record); got != "中国 联通" {
		t.Fatalf("formatIP2RegionLocation() = %q", got)
	}
}

func TestParseIP2RegionLocationRejectsEmptyRecord(t *testing.T) {
	if _, ok := parseIP2RegionRecord("0|0|0|0|"); ok {
		t.Fatal("parseIP2RegionRecord() returned found for empty record")
	}
}

func TestIP2RegionDatabaseIntegration(t *testing.T) {
	path := os.Getenv("IP2REGION_TEST_XDB")
	if path == "" {
		t.Skip("IP2REGION_TEST_XDB is not set")
	}
	t.Setenv("IP2REGION_XDB_PATH", path)
	notificationIP2RegionDB = ip2RegionDatabase{}
	t.Cleanup(func() { notificationIP2RegionDB = ip2RegionDatabase{} })

	got, ok := lookupIP2RegionLocation("112.86.93.17")
	if !ok {
		t.Fatal("lookupIP2RegionLocation() returned not found")
	}
	if got != "中国 江苏省 苏州市 联通" {
		t.Fatalf("lookupIP2RegionLocation() = %q", got)
	}
}
