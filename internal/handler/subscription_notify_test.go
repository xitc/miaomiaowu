package handler

import (
	"context"
	"io"
	"miaomiaowu/internal/notify"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionFetchMessageRequestTimeAndOutputMode(t *testing.T) {
	for _, tc := range []struct{ mode, label string }{
		{"normal", "普通订阅"}, {"provider", "Provider"}, {"raw", "原始输出"}, {providerSourceOutputMode, "提供者节点"}, {"", "未知"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			notice := subscriptionFetchNotice{
				RequestedAt: time.Date(2026, 9, 11, 18, 0, 56, 0, time.UTC),
				OutputMode:  tc.mode,
				Duration:    278 * time.Millisecond,
			}
			got := formatSubscriptionFetchMessage(notice, "内网")
			for _, want := range []string{"请求时间: 2026-09-12 02:00:56（北京时间）", "输出模式: " + tc.label + "\n", "服务端耗时: 278 ms"} {
				if !strings.Contains(got, want) {
					t.Fatalf("message = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSanitizeNotificationValueRemovesInjectionAndBoundsLength(t *testing.T) {
	input := "Mihomo/1.0\r\n伪造字段: yes\t" + strings.Repeat("x", 20)
	got := sanitizeNotificationValue(input, 18)
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("sanitizeNotificationValue() retained control characters: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("sanitizeNotificationValue() = %q, want truncation suffix", got)
	}
	if len([]rune(strings.TrimSuffix(got, "…"))) != 18 {
		t.Fatalf("sanitizeNotificationValue() = %q, want 18 runes before suffix", got)
	}
}

func TestDescribeIPLocationLabelsPrivateAddressesWithoutLookup(t *testing.T) {
	if got := describeIPLocation(context.Background(), "10.0.0.25"); got != "内网" {
		t.Fatalf("describeIPLocation() = %q, want 内网", got)
	}
	if got := describeIPLocation(context.Background(), "not-an-ip"); got != "未知" {
		t.Fatalf("describeIPLocation() = %q, want 未知", got)
	}
}

// Intercept Telegram locally: this test must never send a real notification.
type subscriptionNoticeTransport struct {
	original http.RoundTripper
	messages chan string
}

func (tr subscriptionNoticeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.telegram.org" {
		return tr.original.RoundTrip(r)
	}
	tr.messages <- r.URL.Query().Get("text")
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
}
func TestProviderSourceSendsOneNoticeWithTimingAndSubscriber(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), "")
	messages := make(chan string, 8)
	oldTransport, oldNotifier := http.DefaultTransport, globalNotifier
	http.DefaultTransport = subscriptionNoticeTransport{oldTransport, messages}
	InitNotifier(notify.Config{Enabled: true, BotToken: "test-only", ChatID: "test-only", NotifySubscribeFetch: true})
	t.Cleanup(func() { http.DefaultTransport = oldTransport; globalNotifier = oldNotifier })
	req := h.request(h.subscriber, h.providerID)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("User-Agent", "clash.meta")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	select {
	case msg := <-messages:
		for _, want := range []string{"用户: " + h.subscriber, "订阅: provider-source-test", "输出模式: 提供者节点", "提供者: source", "客户端: clash", "UA: clash.meta", "IP属地: 内网", "（北京时间）"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("missing %q in %q", want, msg)
			}
		}
		if !regexp.MustCompile(`服务端耗时: [0-9]+ ms`).MatchString(msg) {
			t.Fatalf("missing duration: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("notification missing")
	}
	bad := httptest.NewRecorder()
	h.handler.ServeHTTP(bad, h.request(h.subscriber, h.unselectedID))
	if bad.Code != http.StatusNotFound {
		t.Fatalf("rejected provider status=%d", bad.Code)
	}
	select {
	case msg := <-messages:
		t.Fatalf("unexpected extra or failure notification: %q", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
