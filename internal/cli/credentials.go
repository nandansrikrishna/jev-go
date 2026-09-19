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
			return "", errInput
		}
		key = strings.TrimSpace(key)
	}
	if !validKey(key) {
		return "", errInput
	}
	return key, nil
}
