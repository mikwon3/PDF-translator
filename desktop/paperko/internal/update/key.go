package update

import (
	"crypto/ed25519"
	"encoding/base64"
)

// publicKey 는 릴리스 manifest 를 확인하는 Ed25519 공개키다(base64). 짝이 되는 개인키는 릴리스를
// 만드는 컴퓨터에만 있다(cmd/releasetool keygen 으로 만들어 ~/Library/Application Support/PaperKo/
// release-key/paperko-release.key 에 둔다). 키를 바꾸면 이미 설치된 앱은 새 판을 받지 못하므로
// 함부로 바꾸지 않는다.
const publicKey = "gFWO5UoQtfzAXskZnHpfoEFuabcLAQgBQY/nuanaI/E="

// PublicKey 는 심은 공개키다.
func PublicKey() ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}
