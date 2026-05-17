// encrypt.go — EncryptedCodec: AES-GCM authenticated-encryption codec wrapper (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares KeyProvider, StaticKey, and EncryptedCodec which
// wraps an inner Codec with AES-GCM so PII/secrets cached in a shared or
// managed backend are not a breach if the store is dumped. The WHY: caching
// sensitive values safely without a backend-specific encryption feature.
// Wire-format invariant: stored layout is [12-byte random nonce][GCM
// ciphertext+tag]; a tampered or wrong-key payload fails Open and surfaces
// as ErrSerialization (never silent garbage).
//
// AI-context: this is a Codec decorator (decorates codec.go's Codec). Key
// rotation is by re-warming the cache, not in place — decryption always
// uses the current key the provider returns.

package cache

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// KeyProvider supplies the 16/24/32-byte AES key used by EncryptedCodec.
// Returning a fresh value each call enables key rotation; decryption always
// uses the current key (rotate by re-warming the cache, not in place).
type KeyProvider func() ([]byte, error)

// StaticKey is a KeyProvider for a fixed key.
func StaticKey(key []byte) KeyProvider {
	return func() ([]byte, error) { return key, nil }
}

// EncryptedCodec wraps another Codec with authenticated encryption
// (AES-GCM). Use it to cache PII/secrets in a shared or managed backend so a
// raw dump of the store is not a data breach. Tampered ciphertext fails to
// decrypt (GCM authentication), surfacing as ErrSerialization.
//
//	codec := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: cache.StaticKey(k)}
//
// Layout: [12-byte nonce][GCM ciphertext+tag]. Nonce is random per Encode.
type EncryptedCodec struct {
	Inner Codec
	Key   KeyProvider
}

// Name returns the codec identifier.
func (e EncryptedCodec) Name() string { return "aesgcm+" + e.inner().Name() }

func (e EncryptedCodec) inner() Codec {
	if e.Inner != nil {
		return e.Inner
	}
	return DefaultCodec
}

func (e EncryptedCodec) gcm() (cipher.AEAD, error) {
	if e.Key == nil {
		return nil, fmt.Errorf("%w: EncryptedCodec.Key is nil", ErrSerialization)
	}
	key, err := e.Key()
	if err != nil {
		return nil, fmt.Errorf("%w: key provider: %v", ErrSerialization, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: aes: %v", ErrSerialization, err)
	}
	return cipher.NewGCM(block)
}

// Encode serializes v with the inner codec, then seals it.
func (e EncryptedCodec) Encode(v any) ([]byte, error) {
	plain, err := e.inner().Encode(v)
	if err != nil {
		return nil, err
	}
	aead, err := e.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: nonce: %v", ErrSerialization, err)
	}
	// Seal appends ciphertext to nonce so the nonce travels with the payload.
	return aead.Seal(nonce, nonce, plain, nil), nil
}

// Decode opens the payload and hands the plaintext to the inner codec.
func (e EncryptedCodec) Decode(data []byte, v any) error {
	aead, err := e.gcm()
	if err != nil {
		return err
	}
	ns := aead.NonceSize()
	if len(data) < ns {
		return fmt.Errorf("%w: ciphertext shorter than nonce", ErrSerialization)
	}
	nonce, ct := data[:ns], data[ns:]
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("%w: gcm open (tampered or wrong key): %v", ErrSerialization, err)
	}
	return e.inner().Decode(plain, v)
}
