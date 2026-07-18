package storage

import (
	"fmt"
	"strings"
)

// Subscribe output modes for dual-link subscriptions.
const (
	OutputModeNormal   = "normal"
	OutputModeProvider = "provider"
)

// NormalizeDefaultOutputMode returns a valid default mode.
func NormalizeDefaultOutputMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OutputModeProvider:
		return OutputModeProvider
	default:
		return OutputModeNormal
	}
}

// ParseOutputMode validates an explicitly supplied output mode.
func ParseOutputMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case OutputModeNormal, OutputModeProvider:
		return mode, nil
	default:
		return "", fmt.Errorf("非法输出模式 %q，仅支持 normal 或 provider", mode)
	}
}

// ApplySubscribeLinkDefaults fills missing dual-mode flags for new/legacy rows.
// Raw-only files (RawOutput without template) stay normal-only.
func ApplySubscribeLinkDefaults(file *SubscribeFile) {
	if file == nil {
		return
	}
	// Zero-value Go struct: both false means "unset" for create paths.
	// DB COALESCE defaults handle legacy reads.
	if !file.NormalLinkEnabled && !file.ProviderLinkEnabled {
		file.NormalLinkEnabled = true
	}
	file.DefaultOutputMode = NormalizeDefaultOutputMode(file.DefaultOutputMode)
	if file.DefaultOutputMode == OutputModeProvider && !file.ProviderLinkEnabled {
		if file.NormalLinkEnabled {
			file.DefaultOutputMode = OutputModeNormal
		} else {
			file.ProviderLinkEnabled = true
		}
	}
	if file.DefaultOutputMode == OutputModeNormal && !file.NormalLinkEnabled {
		if file.ProviderLinkEnabled {
			file.DefaultOutputMode = OutputModeProvider
		} else {
			file.NormalLinkEnabled = true
		}
	}
}

// ValidateSubscribeOutputModes checks dual-mode consistency before save.
// providerClientCount is the number of available process_mode=client providers (admin side).
func ValidateSubscribeOutputModes(file SubscribeFile, providerClientCount int) error {
	if !file.NormalLinkEnabled && !file.ProviderLinkEnabled {
		return fmt.Errorf("至少启用一种输出模式（普通链接或 Provider 链接）")
	}
	mode := NormalizeDefaultOutputMode(file.DefaultOutputMode)
	if mode == OutputModeNormal && !file.NormalLinkEnabled {
		return fmt.Errorf("默认输出模式为普通链接，但未启用普通链接")
	}
	if mode == OutputModeProvider && !file.ProviderLinkEnabled {
		return fmt.Errorf("默认输出模式为 Provider 链接，但未启用 Provider 链接")
	}
	if file.ProviderLinkEnabled {
		if strings.TrimSpace(file.TemplateFilename) == "" {
			return fmt.Errorf("启用 Provider 链接时必须绑定 v3 模板")
		}
		if providerClientCount <= 0 {
			return fmt.Errorf("启用 Provider 链接时需要至少一个可用的 client provider")
		}
	}
	// True raw file output is mutually exclusive with provider mode generation.
	if file.RawOutput && file.ProviderLinkEnabled {
		return fmt.Errorf("原始文件输出与 Provider 链接不能同时启用")
	}
	if file.RawOutput && strings.TrimSpace(file.TemplateFilename) != "" {
		return fmt.Errorf("原始文件输出不能绑定模板")
	}
	return nil
}

// ResolveOutputMode picks the effective generation mode for a subscription request.
// requested may be empty, "normal", or "provider".
func (f SubscribeFile) ResolveOutputMode(requested string) (string, error) {
	req := strings.ToLower(strings.TrimSpace(requested))
	switch req {
	case "":
		mode := NormalizeDefaultOutputMode(f.DefaultOutputMode)
		if mode == OutputModeProvider && f.ProviderLinkEnabled {
			return OutputModeProvider, nil
		}
		if mode == OutputModeNormal && f.NormalLinkEnabled {
			return OutputModeNormal, nil
		}
		// Fall back to any enabled mode if default is inconsistent (should not happen after validation).
		if f.ProviderLinkEnabled {
			return OutputModeProvider, nil
		}
		if f.NormalLinkEnabled {
			return OutputModeNormal, nil
		}
		return "", fmt.Errorf("订阅未启用任何输出模式")
	case OutputModeNormal:
		if !f.NormalLinkEnabled {
			return "", fmt.Errorf("该订阅未启用普通链接模式")
		}
		return OutputModeNormal, nil
	case OutputModeProvider:
		if !f.ProviderLinkEnabled {
			return "", fmt.Errorf("该订阅未启用 Provider 链接模式")
		}
		return OutputModeProvider, nil
	default:
		return "", fmt.Errorf("非法输出模式 %q，仅支持 normal 或 provider", requested)
	}
}

// IsClashCompatibleClient reports whether client type t is allowed for Provider mode.
// Empty / clash / clashmeta are accepted.
func IsClashCompatibleClient(clientType string) bool {
	t := strings.ToLower(strings.TrimSpace(clientType))
	switch t {
	case "", "clash", "clashmeta":
		return true
	default:
		return false
	}
}
