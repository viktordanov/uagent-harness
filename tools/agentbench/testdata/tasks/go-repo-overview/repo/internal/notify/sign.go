package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// sign is the hex HMAC-SHA256 of body with secret, prefixed "sha256=".
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
