package handler

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"miaomiaowu/internal/logger"
	"miaomiaowu/internal/notify"
	"miaomiaowu/internal/storage"
)

const (
	subscriptionNotifyTimeout    = 10 * time.Second
	subscriptionGeoLookupTimeout = 3 * time.Second
)

type subscriptionFetchNotice struct {
	RequestedAt  time.Time
	Duration     time.Duration
	ProviderName string
	OutputMode   string
	Username     string
	Subscription string
	ClientType   string
	UserAgent    string
	ClientIP     string
}

func queueSubscriptionFetchNotification(notice subscriptionFetchNotice) {
	n := GetNotifier()
	if n == nil || !n.IsEnabled(notify.EventSubscribeFetch) {
		return
	}

	notice.Username = sanitizeNotificationValue(notice.Username, 100)
	notice.ProviderName = sanitizeNotificationValue(notice.ProviderName, 160)
	notice.Subscription = sanitizeNotificationValue(notice.Subscription, 160)
	notice.ClientType = sanitizeNotificationValue(notice.ClientType, 80)
	notice.UserAgent = sanitizeNotificationValue(notice.UserAgent, 240)
	notice.ClientIP = sanitizeNotificationValue(notice.ClientIP, 64)
	if notice.Username == "" {
		notice.Username = "未知"
	}
	if notice.Subscription == "" {
		notice.Subscription = "未知"
	}
	if notice.ClientType == "" {
		notice.ClientType = detectClientTypeFromUA(notice.UserAgent)
	}
	if notice.ClientType == "" {
		notice.ClientType = "未知"
	}
	if notice.UserAgent == "" {
		notice.UserAgent = "未提供"
	}
	if notice.ClientIP == "" {
		notice.ClientIP = "未知"
	}

	go sendSubscriptionFetchNotification(n, notice)
}

func sendSubscriptionFetchNotification(n *notify.Notifier, notice subscriptionFetchNotice) {
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionNotifyTimeout)
	defer cancel()

	geoCtx, stopGeoLookup := context.WithTimeout(ctx, subscriptionGeoLookupTimeout)
	location := describeIPLocation(geoCtx, notice.ClientIP)
	stopGeoLookup()
	event := notify.Event{
		Type:      notify.EventSubscribeFetch,
		Title:     "订阅获取",
		PlainText: true,
		Message:   formatSubscriptionFetchMessage(notice, location),
	}
	if err := n.Send(ctx, event); err != nil {
		logger.Warn("[Notify] 订阅获取通知发送失败", "error", err)
	}
}

func formatSubscriptionFetchMessage(notice subscriptionFetchNotice, location string) string {
	mode := "未知"
	switch notice.OutputMode {
	case storage.OutputModeNormal:
		mode = "普通订阅"
	case storage.OutputModeProvider:
		mode = "Provider"
	case providerSourceOutputMode:
		mode = "提供者节点"
	case "raw":
		mode = "原始输出"
	}
	requestedAt := "未知"
	if !notice.RequestedAt.IsZero() {
		requestedAt = notice.RequestedAt.In(time.FixedZone("北京时间", 8*60*60)).Format("2006-01-02 15:04:05") + "（北京时间）"
	}
	provider := ""
	if notice.OutputMode == providerSourceOutputMode {
		provider = "\n提供者: " + notice.ProviderName
	}
	return fmt.Sprintf("用户: %s\n订阅: %s\n请求时间: %s\n输出模式: %s%s\n服务端耗时: %d ms\n客户端: %s\nUA: %s\nIP: %s\nIP属地: %s",
		notice.Username, notice.Subscription, requestedAt, mode, provider, notice.Duration.Milliseconds(),
		notice.ClientType, notice.UserAgent, notice.ClientIP, location)
}

func describeIPLocation(ctx context.Context, ipString string) string {
	ip := net.ParseIP(strings.TrimSpace(ipString))
	if ip == nil {
		return "未知"
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "内网"
	}
	if location, ok := lookupIP2RegionLocation(ip.String()); ok {
		return location
	}

	info := lookupGeoIPInfo(ctx, ip.String())
	location := info.Country
	if location == "" {
		location = info.CountryCode
	} else if info.CountryCode != "" {
		location += " (" + info.CountryCode + ")"
	}
	if location == "" {
		location = "未知"
	}

	network := strings.TrimSpace(strings.Join([]string{info.ASN, info.ASName}, " "))
	if network != "" {
		location += " · " + network
	}
	return location
}

func sanitizeNotificationValue(value string, maxRunes int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return value
}
