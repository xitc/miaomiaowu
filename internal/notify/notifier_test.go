package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSendSupportsPlainTextAndExistingMarkdown(t *testing.T) {
	queries := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	oldBase, oldClient := telegramAPIBase, httpClient
	telegramAPIBase = server.URL + "/bot"
	httpClient = server.Client()
	t.Cleanup(func() {
		telegramAPIBase, httpClient = oldBase, oldClient
	})

	n := New(Config{Enabled: true, BotToken: "test-token", ChatID: "test-chat", NotifySubscribeFetch: true})
	err := n.Send(context.Background(), Event{
		Type:      EventSubscribeFetch,
		Title:     "订阅获取",
		Message:   "UA: client_[unsafe]",
		PlainText: true,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	query := <-queries
	if got := query.Get("parse_mode"); got != "" {
		t.Fatalf("parse_mode = %q, want omitted", got)
	}
	if got := query.Get("text"); got != "订阅获取\nUA: client_[unsafe]" {
		t.Fatalf("text = %q", got)
	}

	err = n.Send(context.Background(), Event{
		Type:    EventSubscribeFetch,
		Title:   "订阅获取",
		Message: "客户端: clash",
	})
	if err != nil {
		t.Fatalf("Send() markdown error = %v", err)
	}
	query = <-queries
	if got := query.Get("parse_mode"); got != "Markdown" {
		t.Fatalf("parse_mode = %q, want Markdown", got)
	}
	if got := query.Get("text"); got != "*订阅获取*\n客户端: clash" {
		t.Fatalf("markdown text = %q", got)
	}
}
