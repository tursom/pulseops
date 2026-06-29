package pluginconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"pulseops/internal/pluginmodel"
)

const secretEncryptionAlg = "aes-256-gcm-local-v1"

func EncryptSecret(plaintext string) (string, map[string]any, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", nil, fmt.Errorf("generate plugin secret data key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, fmt.Errorf("create plugin secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, fmt.Errorf("create plugin secret gcm: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", nil, fmt.Errorf("generate plugin secret nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, []byte(plaintext), nil)
	meta := map[string]any{
		"alg":      secretEncryptionAlg,
		"nonce":    base64.StdEncoding.EncodeToString(nonce),
		"data_key": base64.StdEncoding.EncodeToString(key),
	}
	return base64.StdEncoding.EncodeToString(sealed), meta, nil
}

func DecryptSecret(value pluginmodel.SecretValueRecord) (string, error) {
	if value.Ciphertext == "" {
		return "", errors.New("secret ciphertext is empty")
	}
	alg, _ := value.EncryptionMeta["alg"].(string)
	if alg != secretEncryptionAlg {
		return "", fmt.Errorf("unsupported plugin secret encryption alg %q", alg)
	}
	nonceText, _ := value.EncryptionMeta["nonce"].(string)
	keyText, _ := value.EncryptionMeta["data_key"].(string)
	if nonceText == "" || keyText == "" {
		return "", errors.New("plugin secret encryption metadata is incomplete")
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceText)
	if err != nil {
		return "", fmt.Errorf("decode plugin secret nonce: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil {
		return "", fmt.Errorf("decode plugin secret data key: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(value.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode plugin secret ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create plugin secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create plugin secret gcm: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt plugin secret: %w", err)
	}
	return string(plaintext), nil
}

func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	return "********"
}
