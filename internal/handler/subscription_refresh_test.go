package handler

import "testing"

func TestRefreshSubscriptionDataAfterExternalSyncPreservesTemplateMode(t *testing.T) {
	generated := 0
	read := 0
	data, err := refreshSubscriptionDataAfterExternalSync(
		true,
		func() ([]byte, error) {
			generated++
			return []byte("proxy-providers: {}\n"), nil
		},
		func() ([]byte, error) {
			read++
			return []byte("proxies: []\n"), nil
		},
	)
	if err != nil {
		t.Fatalf("refresh template output: %v", err)
	}
	if string(data) != "proxy-providers: {}\n" {
		t.Fatalf("template output replaced: %q", data)
	}
	if generated != 1 || read != 0 {
		t.Fatalf("generated=%d read=%d, want generated=1 read=0", generated, read)
	}
}

func TestRefreshSubscriptionDataAfterExternalSyncReadsNonTemplateFile(t *testing.T) {
	generated := 0
	read := 0
	data, err := refreshSubscriptionDataAfterExternalSync(
		false,
		func() ([]byte, error) {
			generated++
			return []byte("proxy-providers: {}\n"), nil
		},
		func() ([]byte, error) {
			read++
			return []byte("proxies: []\n"), nil
		},
	)
	if err != nil {
		t.Fatalf("refresh file output: %v", err)
	}
	if string(data) != "proxies: []\n" {
		t.Fatalf("non-template output = %q", data)
	}
	if generated != 0 || read != 1 {
		t.Fatalf("generated=%d read=%d, want generated=0 read=1", generated, read)
	}
}
