// codec.go — the Codec interface + the three built-in codecs (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares Codec (Encode/Decode/Name) plus JSONCodec (default,
// portable), GobCodec (Go-only, faster), RawCodec (passthrough for
// already-serialized []byte/string), and the DefaultCodec var. The WHY:
// the generics layer (Remember/Typed/GetT) serializes through a Codec so
// the bytes-level Cache stays type-agnostic. Invariant: every encode/decode
// failure is wrapped so errors.Is(err, ErrSerialization) holds; RawCodec
// rejects non-[]byte/string loudly rather than silently corrupting data.
//
// AI-context: contrib modules (codec-msgpack/zstd/protobuf) implement this
// same interface from sibling modules and wrap/replace DefaultCodec usage.

package cache

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// Codec serializes typed values to and from the bytes the Cache stores.
type Codec interface {
	Encode(v any) ([]byte, error)
	Decode(data []byte, v any) error
	Name() string
}

// JSONCodec is the default codec: portable and debuggable.
type JSONCodec struct{}

// Name returns the codec identifier.
func (JSONCodec) Name() string { return "json" }

// Encode marshals v to JSON.
func (JSONCodec) Encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: json encode: %v", ErrSerialization, err)
	}
	return b, nil
}

// Decode unmarshals JSON into v.
func (JSONCodec) Decode(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: json decode: %v", ErrSerialization, err)
	}
	return nil
}

// GobCodec is faster for Go-only struct round-trips. Types with unexported
// fields must register with encoding/gob as usual.
type GobCodec struct{}

// Name returns the codec identifier.
func (GobCodec) Name() string { return "gob" }

// Encode gob-encodes v.
func (GobCodec) Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, fmt.Errorf("%w: gob encode: %v", ErrSerialization, err)
	}
	return buf.Bytes(), nil
}

// Decode gob-decodes data into v.
func (GobCodec) Decode(data []byte, v any) error {
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(v); err != nil {
		return fmt.Errorf("%w: gob decode: %v", ErrSerialization, err)
	}
	return nil
}

// RawCodec is a passthrough for already-serialized payloads. It only accepts
// []byte / *[]byte / string / *string — anything else is a serialization error
// so misuse fails loudly instead of silently corrupting data.
type RawCodec struct{}

// Name returns the codec identifier.
func (RawCodec) Name() string { return "raw" }

// Encode passes through []byte/string values unchanged.
func (RawCodec) Encode(v any) ([]byte, error) {
	switch t := v.(type) {
	case []byte:
		return t, nil
	case *[]byte:
		return *t, nil
	case string:
		return []byte(t), nil
	case *string:
		return []byte(*t), nil
	default:
		return nil, fmt.Errorf("%w: raw codec only supports []byte/string, got %T", ErrSerialization, v)
	}
}

// Decode writes data into a *[]byte or *string target.
func (RawCodec) Decode(data []byte, v any) error {
	switch t := v.(type) {
	case *[]byte:
		*t = data
		return nil
	case *string:
		*t = string(data)
		return nil
	default:
		return fmt.Errorf("%w: raw codec only supports *[]byte/*string, got %T", ErrSerialization, v)
	}
}

// DefaultCodec is used by the generics layer when no codec is configured.
var DefaultCodec Codec = JSONCodec{}
