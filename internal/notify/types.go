package notify

// EventType enumerates notification categories.
type EventType string

const (
	EventSubscribeFetch EventType = "subscribe_fetch"
	EventLogin          EventType = "login"
	EventIPBan          EventType = "ip_ban"
	EventSilentMode     EventType = "silent_mode"
	EventDailyTraffic   EventType = "daily_traffic"
	EventExpiry         EventType = "expiry"
)

// Config holds Telegram notification configuration.
// Designed to be loaded/stored externally; this package does not import storage.
type Config struct {
	Enabled              bool
	BotToken             string
	ChatID               string
	NotifySubscribeFetch bool
	NotifyLogin          bool
	NotifyIPBan          bool
	NotifySilentMode     bool
	NotifyDailyTraffic   bool
	NotifyExpiry         bool
	DailyTrafficTime     string // "HH:MM" format, e.g. "08:00"
}

// Event holds data for a notification to be sent.
type Event struct {
	Type      EventType
	Title     string
	Message   string
	PlainText bool // 用户可控内容使用纯文本，避免 Telegram Markdown 注入或解析失败
}
