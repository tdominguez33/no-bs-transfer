//go:build !windows

package config

import (
	"os"
	"strings"
)

// DetectSystemLanguage detects whether the host OS uses Spanish or defaults to English.
func DetectSystemLanguage() string {
	lang := strings.ToLower(os.Getenv("LC_ALL"))
	if lang == "" {
		lang = strings.ToLower(os.Getenv("LANG"))
	}
	if strings.HasPrefix(lang, "es") {
		return LanguageES
	}
	return LanguageEN
}
