package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// Sealer cifra e decifra i token Spotify con AES-256-GCM.
type Sealer struct{ aead cipher.AEAD }

func NewSealer(key []byte) (*Sealer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal restituisce nonce || ciphertext. additional lega il dato a un contesto
// (es. l'id utente), così un token non può essere spostato su un altro utente.
func (s *Sealer) Seal(plain, additional []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plain)+s.aead.Overhead())
	_, _ = rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, plain, additional)
}

func (s *Sealer) Open(sealed, additional []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("dato cifrato troppo corto")
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], additional)
}
