package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

func sha256x3(b []byte) []byte {
	h := sha256.Sum256(b)
	h2 := sha256.Sum256(h[:])
	h3 := sha256.Sum256(h2[:])
	return h3[:]
}

// DeriveAES128Key returns the AES-128 key: SHA256^3(encRand)[:16].
func DeriveAES128Key(encRand []byte) ([]byte, error) {
	if len(encRand) != 16 {
		return nil, fmt.Errorf("encRand must be 16 bytes, got %d", len(encRand))
	}
	return sha256x3(encRand)[:16], nil
}

// DeriveNonce returns the 12-byte GCM nonce: SHA256^3(le16(tag)||le16(seq)||encRand)[-12:].
func DeriveNonce(encRand []byte, tag, seq uint16) ([]byte, error) {
	if len(encRand) != 16 {
		return nil, fmt.Errorf("encRand must be 16 bytes, got %d", len(encRand))
	}
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint16(buf[0:2], tag)
	binary.LittleEndian.PutUint16(buf[2:4], seq)
	copy(buf[4:], encRand)
	d := sha256x3(buf)
	return d[20:32], nil
}

// Crypt encrypts (encrypt=true) or decrypts (encrypt=false) with AES-128-GCM.
// aad = le16(tag)||le16(seq). Output of encrypt includes the 16-byte GCM tag.
func Crypt(encrypt bool, encRand []byte, tag, seq uint16, in []byte) ([]byte, error) {
	key, err := DeriveAES128Key(encRand)
	if err != nil {
		return nil, err
	}
	nonce, err := DeriveNonce(encRand, tag, seq)
	if err != nil {
		return nil, err
	}
	aad := make([]byte, 4)
	binary.LittleEndian.PutUint16(aad[0:2], tag)
	binary.LittleEndian.PutUint16(aad[2:4], seq)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if encrypt {
		return gcm.Seal(nil, nonce, in, aad), nil
	}
	return gcm.Open(nil, nonce, in, aad)
}
