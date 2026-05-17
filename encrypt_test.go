// encrypt_test.go — tests for the AES-GCM EncryptedCodec (encrypt.go).

package cache_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ubgo/cache"
)

func TestEncryptedCodecRoundtrip(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	ec := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: cache.StaticKey(key)}

	type secret struct {
		SSN string
	}
	enc, err := ec.Encode(secret{SSN: "123-45-6789"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte("123-45-6789")) {
		t.Fatal("plaintext leaked into ciphertext")
	}
	var out secret
	if err := ec.Decode(enc, &out); err != nil {
		t.Fatal(err)
	}
	if out.SSN != "123-45-6789" {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestEncryptedCodecNonceUnique(t *testing.T) {
	ec := cache.EncryptedCodec{Key: cache.StaticKey(bytes.Repeat([]byte("x"), 16))}
	a, _ := ec.Encode("same")
	b, _ := ec.Encode("same")
	if bytes.Equal(a, b) {
		t.Fatal("identical plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestEncryptedCodecTamperDetected(t *testing.T) {
	ec := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: cache.StaticKey(bytes.Repeat([]byte("z"), 32))}
	enc, _ := ec.Encode(map[string]int{"n": 1})
	enc[len(enc)-1] ^= 0xff // flip a ciphertext bit
	var out map[string]int
	err := ec.Decode(enc, &out)
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("tamper must fail with ErrSerialization, got %v", err)
	}
}

func TestEncryptedCodecWrongKeyFails(t *testing.T) {
	enc, _ := cache.EncryptedCodec{Key: cache.StaticKey(bytes.Repeat([]byte("a"), 32))}.Encode("hi")
	var s string
	err := cache.EncryptedCodec{Key: cache.StaticKey(bytes.Repeat([]byte("b"), 32))}.Decode(enc, &s)
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("wrong key must fail, got %v", err)
	}
}
