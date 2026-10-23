// Package secrets, diskte duran gizli bilgileri (API anahtarları) AES-256-GCM ile şifreler.
// Anahtar, şifreli dosyanın yanında 0600 izinli ayrı bir dosyada tutulur.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const keyBytes = 32

func keyPath(path string) string { return path + ".key" }

func loadOrCreateKey(path string) ([]byte, error) {
	kp := keyPath(path)
	if b, err := os.ReadFile(kp); err == nil {
		if k, derr := hex.DecodeString(string(b)); derr == nil && len(k) == keyBytes {
			return k, nil
		}
		return nil, fmt.Errorf("şifreleme anahtarı bozuk: %s", kp)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k := make([]byte, keyBytes)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(kp), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(kp, []byte(hex.EncodeToString(k)), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal düz metni nonce||şifreli metin||etiket biçiminde şifreler.
func Seal(path string, plaintext []byte) ([]byte, error) {
	key, err := loadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plaintext, nil), nil
}

// Open Seal ile şifrelenmiş veriyi çözer. Anahtar yoksa ya da veri bozuksa hata döner.
func Open(path string, data []byte) ([]byte, error) {
	if _, err := os.Stat(keyPath(path)); err != nil {
		return nil, errors.New("şifreleme anahtarı bulunamadı")
	}
	key, err := loadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize() {
		return nil, errors.New("şifreli veri çok kısa")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
}
