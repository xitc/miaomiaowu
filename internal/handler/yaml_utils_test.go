package handler

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSubscriptionYAMLTextNormalization(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"plain", "name: 香港\nport: 443\n", "name: 香港\nport: 443\n"},
		{"unicode", `name: "\U0001F1ED\U0001F1F0 \u9999\u6E2F"` + "\n", "name: 🇭🇰 香港\n"},
		{"quoted_special", `name: "[\u9999\u6E2F]"` + "\n", "name: \"[香港]\"\n"},
		{"numeric_types", "port: \"443\"\ninterval: \"300\"\nname: \"001\"\nserver: \"123\"\n", "port: 443\ninterval: 300\nname: \"001\"\nserver: \"123\"\n"},
		{"dns_policy", "nameserver-policy:\n  example.invalid: \"1.1.1.1\"\n", "nameserver-policy:\n  example.invalid: \"1.1.1.1\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RemoveUnicodeEscapeQuotes(tc.input)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			var doc any
			if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkSubscriptionYAMLNormalization(b *testing.B) {
	input := strings.Repeat(`- name: "\U0001F1ED\U0001F1F0 \u9999\u6E2F"`+"\n  port: \"443\"\n", 233)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		RemoveUnicodeEscapeQuotes(input)
	}
}
