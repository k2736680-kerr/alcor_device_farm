package sensitive

import (
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var (
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{6,}`)
	secretPattern        = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|cookie|credential|api[_-]?key)\s*[:=]\s*[^\s,;]+`)
	credentialURLPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://[^\s/@:]+:[^@\s]+@`)
)

func Contains(value string) bool {
	return bearerPattern.MatchString(value) || secretPattern.MatchString(value) || credentialURLPattern.MatchString(value)
}

func RedactText(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer "+Redacted)
	value = secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		separator := strings.IndexAny(match, ":=")
		if separator < 0 {
			return Redacted
		}
		return strings.TrimSpace(match[:separator]) + "=" + Redacted
	})
	value = credentialURLPattern.ReplaceAllString(value, "$1://"+Redacted+"@")
	return value
}

func ContainsMap(source map[string]any) bool {
	for key, value := range source {
		if sensitiveKey(key) || containsValue(value) {
			return true
		}
	}
	return false
}

func RedactMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		if sensitiveKey(key) {
			result[key] = Redacted
			continue
		}
		result[key] = redactValue(value)
	}
	return result
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case string:
		return RedactText(typed)
	case map[string]any:
		return RedactMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactValue(item)
		}
		return result
	default:
		return value
	}
}

func containsValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return Contains(typed)
	case map[string]any:
		return ContainsMap(typed)
	case []any:
		for _, item := range typed {
			if containsValue(item) {
				return true
			}
		}
	}
	return false
}

func sensitiveKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(strings.ToLower(key))
	for _, fragment := range []string{"authorization", "password", "passwd", "token", "secret", "cookie", "credential", "apikey", "dsn"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
