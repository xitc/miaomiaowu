package handler

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Immutable patterns are safe to reuse across concurrent subscription requests.
var (
	nameserverPolicyRe = regexp.MustCompile(`(?ms)^(nameserver-policy:\s*\n)((?:[ \t]+.+\n?)*)`)
	quotedUnicodeRe    = regexp.MustCompile(`"([^"]*\\[Uu][0-9A-Fa-f]{4,8}[^"]*)"`)
	escapeRe           = regexp.MustCompile(`\\U([0-9A-Fa-f]{8})|\\u([0-9A-Fa-f]{4})`)
	numericQuotesRe    = regexp.MustCompile(`(?m)^(\s*)(port|socks-port|redir-port|tproxy-port|mixed-port|dns-port|interval|timeout|geo-update-interval|update-interval|size-limit|size_limit|health-check-interval|health-check-timeout):\s+"(\d+)"`)
)

// MarshalYAMLWithIndent marshals a YAML node with 2-space indentation
func MarshalYAMLWithIndent(node *yaml.Node) ([]byte, error) {
	// Sanitize explicit string tags before encoding to prevent !!str from appearing in output
	sanitizeExplicitStringTags(node)

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalWithIndent marshals any value to YAML with 2-space indentation
func MarshalWithIndent(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RemoveUnicodeEscapeQuotes removes quotes from strings that contain Unicode escape sequences
// and converts the escape sequences back to actual Unicode characters (like emoji).
// For known numeric fields (port, interval, etc.), removes quotes to ensure proper numeric type.
// Note: nameserver-policy values are preserved with quotes to maintain DNS policy format.
func RemoveUnicodeEscapeQuotes(yamlContent string) string {
	// 使用占位符保留原始nameserver-policy配置
	var nameserverPolicyBlock string
	yamlContent = nameserverPolicyRe.ReplaceAllStringFunc(yamlContent, func(match string) string {
		nameserverPolicyBlock = match
		return "___NAMESERVER_POLICY_PLACEHOLDER___\n"
	})

	// Step 1: Remove quotes from strings that contain Unicode escape sequences
	// Pattern: "...\U000XXXXX..." or "...\uXXXX..."
	// But keep quotes if the unquoted string would start with YAML special characters
	result := quotedUnicodeRe.ReplaceAllStringFunc(yamlContent, func(match string) string {
		// Get the content without quotes
		content := strings.Trim(match, `"`)

		// First convert Unicode escapes to actual characters to check the real first character
		tempContent := convertUnicodeEscapes(content)

		// Check if the unquoted string would start with YAML special characters that need quoting
		// These characters have special meaning in YAML and need to be quoted
		if len(tempContent) > 0 {
			firstChar := tempContent[0]
			// Characters that need quoting at the start: [ ] { } * & ! | > ' " % @ ` # , ? : -
			if firstChar == '[' || firstChar == ']' || firstChar == '{' || firstChar == '}' ||
				firstChar == '*' || firstChar == '&' || firstChar == '!' || firstChar == '|' ||
				firstChar == '>' || firstChar == '\'' || firstChar == '"' || firstChar == '%' ||
				firstChar == '@' || firstChar == '`' || firstChar == '#' || firstChar == ',' ||
				firstChar == '?' || firstChar == ':' || firstChar == '-' {
				// Keep the quotes but still convert Unicode escapes inside
				return `"` + convertUnicodeEscapes(content) + `"`
			}
		}

		// Safe to remove quotes
		return content
	})

	// Step 2: Convert ALL Unicode escapes back to actual characters (quoted or not)
	// \U0001F4B0 -> 💰, \u4E2D -> 中, \U0001F1ED\U0001F1F0 -> 🇭🇰
	result = convertUnicodeEscapes(result)

	// Step 3: Remove quotes from numeric values for known numeric fields
	// Only unquote fields that are expected to be numbers to avoid changing string-typed fields like name/server.
	result = numericQuotesRe.ReplaceAllString(result, `$1$2: $3`)

	// 回复nameserver-policy配置
	if nameserverPolicyBlock != "" {
		result = strings.Replace(result, "___NAMESERVER_POLICY_PLACEHOLDER___\n", nameserverPolicyBlock, 1)
	}

	return result
}

// convertUnicodeEscapes converts Unicode escape sequences to actual characters
func convertUnicodeEscapes(s string) string {
	if !strings.Contains(s, `\u`) && !strings.Contains(s, `\U`) {
		return s
	}
	return escapeRe.ReplaceAllStringFunc(s, func(escapeSeq string) string {
		var codepoint int
		if strings.HasPrefix(escapeSeq, `\U`) {
			fmt.Sscanf(escapeSeq, `\U%X`, &codepoint)
		} else {
			fmt.Sscanf(escapeSeq, `\u%X`, &codepoint)
		}
		return string(rune(codepoint))
	})
}

// yaml.Unmarshal 方法会丢失数字前面的0, 先转为yaml.Node, 再转为map
func yamlNodeToMap(node *yaml.Node) (map[string]interface{}, error) {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) > 0 {
			return yamlNodeToMap(node.Content[0])
		}
		return nil, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got %v", node.Kind)
	}

	result := make(map[string]interface{})
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 >= len(node.Content) {
			break
		}
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		key := keyNode.Value
		value, err := yamlNodeToValue(valueNode)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

// 转换为对应的 Go 类型值, 0开头的值保留其原始字符串格式
func yamlNodeToValue(node *yaml.Node) (interface{}, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		// 对于带引号的字符串，保持字符串格式
		if node.Tag == "!!str" || node.Style == yaml.DoubleQuotedStyle || node.Style == yaml.SingleQuotedStyle {
			return node.Value, nil
		}
		if looksLikeNumericStringWithLeadingZero(node.Value) {
			return node.Value, nil
		}
		// 对于其他标量，使用标准解析
		var value interface{}
		if err := node.Decode(&value); err != nil {
			return node.Value, nil // 解码失败时返回原始字符串
		}
		return value, nil

	case yaml.SequenceNode:
		var result []interface{}
		for _, child := range node.Content {
			value, err := yamlNodeToValue(child)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil

	case yaml.MappingNode:
		return yamlNodeToMap(node)

	case yaml.AliasNode:
		return yamlNodeToValue(node.Alias)

	default:
		return nil, nil
	}
}

// 判断yaml节点的value是否是0开头的数字
func looksLikeNumericStringWithLeadingZero(s string) bool {
	if len(s) < 2 {
		return false
	}
	// 以 0 开头且后续都是数字的字符串（如 "045678"）
	if s[0] == '0' && len(s) > 1 && s[1] >= '0' && s[1] <= '9' {
		for _, c := range s[1:] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// sanitizeExplicitStringTags removes explicit !!str tags from scalar nodes by clearing
// the TaggedStyle bit. This prevents the YAML encoder from emitting literal !!str tags
// in the output, which can cause parsing errors in some YAML clients.
//
// The function recursively walks the entire node tree and normalizes any scalar nodes
// that have explicit string tags (!!str or tag:yaml.org,2002:str). After clearing the
// TaggedStyle bit, the encoder will use implicit typing and add quotes automatically
// when needed, maintaining semantic correctness while improving compatibility.
func sanitizeExplicitStringTags(node *yaml.Node) {
	if node == nil {
		return
	}

	// Clear TaggedStyle for scalar nodes with explicit string tags
	if node.Kind == yaml.ScalarNode && isExplicitStringTag(node.Tag) {
		node.Style &^= yaml.TaggedStyle
	}

	// Recursively process all child nodes
	for _, child := range node.Content {
		sanitizeExplicitStringTags(child)
	}
}

// isExplicitStringTag checks if the given YAML tag represents an explicit string type
func isExplicitStringTag(tag string) bool {
	return tag == "!!str" || tag == "tag:yaml.org,2002:str"
}
