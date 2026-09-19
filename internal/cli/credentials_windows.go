package cli

import "github.com/danieljoos/wincred"

// Python keyring's Windows backend uses service/username and UTF-16 blobs.
func loadKey() (string, error) {
	c, e := wincred.GetGenericCredential("jev-agent-tool/typesafe")
	if e != nil {
		return "", e
	}
	return stringFromUTF16(c.CredentialBlob), nil
}
func saveKey(key string) error {
	if !validKey(key) {
		return invalid("invalid_credentials", "API key must be nonempty and contain no whitespace")
	}
	c := wincred.NewGenericCredential("jev-agent-tool/typesafe")
	c.UserName = "typesafe"
	c.CredentialBlob = bytesUTF16(key)
	return c.Write()
}
