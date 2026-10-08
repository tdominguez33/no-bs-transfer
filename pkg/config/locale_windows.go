//go:build windows

package config

import (
	"strings"
	"syscall"
	"unsafe"
)

// DetectSystemLanguage detects whether the host OS uses Spanish or defaults to English.
func DetectSystemLanguage() string {
	mod := syscall.NewLazyDLL("kernel32.dll")
	proc := mod.NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, 85)
	r1, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r1 > 0 {
		locale := strings.ToLower(syscall.UTF16ToString(buf))
		if strings.HasPrefix(locale, "es") {
			return LanguageES
		}
	}
	return LanguageEN
}
