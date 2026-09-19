package cli

import (
	"os"
	"strings"
	"unicode"
)

func validKey(key string) bool { return key != "" && strings.IndexFunc(key, unicode.IsSpace) < 0 }
func apiKey() (string, error) {
	key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	if key == "" {
		var err error
		key, err = loadKey()
		if err != nil {
			return "", invalid("missing_credentials", "No API key found; set TYPESAFE_API_KEY or run jev auth")
		}
		key = strings.TrimSpace(key)
	}
	if !validKey(key) {
		return "", invalid("invalid_credentials", "API key must be nonempty and contain no whitespace")
	}
	return key, nil
}
