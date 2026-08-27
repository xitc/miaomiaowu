package handler

import (
	"context"
	"strings"
	"testing"
)

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
