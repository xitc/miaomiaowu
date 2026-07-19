package storage

import "testing"

func TestNormalizeDefaultOutputMode(t *testing.T) {
	if NormalizeDefaultOutputMode("") != OutputModeNormal {
		t.Fatal("empty -> normal")
	}
	if NormalizeDefaultOutputMode("PROVIDER") != OutputModeProvider {
		t.Fatal("PROVIDER -> provider")
	}
	if NormalizeDefaultOutputMode("weird") != OutputModeNormal {
		t.Fatal("invalid -> normal")
	}
}

func TestParseOutputMode(t *testing.T) {
	mode, err := ParseOutputMode(" PROVIDER ")
	if err != nil || mode != OutputModeProvider {
		t.Fatalf("parse provider: %q %v", mode, err)
	}
	if _, err := ParseOutputMode("other"); err == nil {
		t.Fatal("expected invalid explicit mode")
	}
}

func TestResolveOutputMode(t *testing.T) {
	both := SubscribeFile{NormalLinkEnabled: true, ProviderLinkEnabled: true, DefaultOutputMode: OutputModeProvider}
	mode, err := both.ResolveOutputMode("")
	if err != nil || mode != OutputModeProvider {
		t.Fatalf("default provider: %v %v", mode, err)
	}
	mode, err = both.ResolveOutputMode("normal")
	if err != nil || mode != OutputModeNormal {
		t.Fatalf("explicit normal: %v %v", mode, err)
	}
	onlyProvider := SubscribeFile{ProviderLinkEnabled: true, DefaultOutputMode: OutputModeProvider}
	if _, err := onlyProvider.ResolveOutputMode("normal"); err == nil {
		t.Fatal("expected error for disabled normal")
	}
	if _, err := both.ResolveOutputMode("other"); err == nil {
		t.Fatal("expected illegal mode error")
	}
}

func TestValidateSubscribeOutputModes(t *testing.T) {
	err := ValidateSubscribeOutputModes(SubscribeFile{}, 0)
	if err == nil {
		t.Fatal("expected at least one mode")
	}
	err = ValidateSubscribeOutputModes(SubscribeFile{
		NormalLinkEnabled: false, ProviderLinkEnabled: true, DefaultOutputMode: OutputModeProvider,
		ProviderTemplateFilename: "x.yaml",
	}, 0)
	if err == nil {
		t.Fatal("expected client provider required")
	}
	err = ValidateSubscribeOutputModes(SubscribeFile{
		NormalLinkEnabled: false, ProviderLinkEnabled: true, DefaultOutputMode: OutputModeProvider,
		ProviderTemplateFilename: "x.yaml",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateSubscribeOutputModes(SubscribeFile{
		RawOutput: true, ProviderLinkEnabled: true, NormalLinkEnabled: true,
		DefaultOutputMode: OutputModeNormal, NormalTemplateFilename: "normal.yaml", ProviderTemplateFilename: "provider.yaml",
	}, 1)
	if err == nil {
		t.Fatal("raw+provider should fail")
	}
}

func TestValidateSubscribeOutputModesRequiresDistinctDualTemplates(t *testing.T) {
	file := SubscribeFile{
		NormalLinkEnabled: true, ProviderLinkEnabled: true, DefaultOutputMode: OutputModeNormal,
		NormalTemplateFilename: "shared.yaml", ProviderTemplateFilename: "shared.yaml",
	}
	if err := ValidateSubscribeOutputModes(file, 1); err == nil {
		t.Fatal("dual mode should reject a shared template")
	}
	file.ProviderTemplateFilename = "provider.yaml"
	if err := ValidateSubscribeOutputModes(file, 1); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateFilenameForMode(t *testing.T) {
	file := SubscribeFile{
		NormalTemplateFilename:   "normal.yaml",
		ProviderTemplateFilename: "provider.yaml",
	}
	if got := file.TemplateFilenameForMode(OutputModeNormal); got != "normal.yaml" {
		t.Fatalf("normal template = %q", got)
	}
	if got := file.TemplateFilenameForMode(OutputModeProvider); got != "provider.yaml" {
		t.Fatalf("provider template = %q", got)
	}
}

func TestIsClashCompatibleClient(t *testing.T) {
	if !IsClashCompatibleClient("") || !IsClashCompatibleClient("clash") {
		t.Fatal("clash ok")
	}
	if IsClashCompatibleClient("sing-box") {
		t.Fatal("sing-box not ok for provider")
	}
	if IsClashCompatibleClient("clash-to-shadowrocket") {
		t.Fatal("Shadowrocket must not receive Mihomo provider YAML")
	}
}
