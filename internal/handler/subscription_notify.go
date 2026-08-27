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
)

const (
	subscriptionNotifyTimeout    = 10 * time.Second
	subscriptionGeoLookupTimeout = 3 * time.Second
)

type subscriptionFetchNotice struct {
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
		Message: fmt.Sprintf(
			"用户: %s\n订阅: %s\n客户端: %s\nUA: %s\nIP: %s\nIP属地: %s",
			notice.Username,
			notice.Subscription,
			notice.ClientType,
			notice.UserAgent,
			notice.ClientIP,
			location,
		),
	}
	if err := n.Send(ctx, event); err != nil {
		logger.Warn("[Notify] 订阅获取通知发送失败", "error", err)
	}
}

func describeIPLocation(ctx context.Context, ipString string) string {
	ip := net.ParseIP(strings.TrimSpace(ipString))
	if ip == nil {
		return "未知"
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "内网"
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
