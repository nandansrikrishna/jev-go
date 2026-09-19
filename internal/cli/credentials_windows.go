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
		return errInput
	}
	c := wincred.NewGenericCredential("jev-agent-tool/typesafe")
	c.UserName = "typesafe"
	c.CredentialBlob = bytesUTF16(key)
	return c.Write()
}
