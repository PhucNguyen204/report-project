package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
)

type EventCipher struct{ aead cipher.AEAD }

func NewEventCipher(key []byte) (*EventCipher, error) {
	if len(key) != 32 {
		return nil, errors.New("queue encryption key must contain32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &EventCipher{aead}, nil
}
func eventAAD(e Event) []byte { b, _ := json.Marshal([]string{e.Event, e.Key}); return b }
func (c *EventCipher) Seal(e Event) (Event, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Event{}, err
	}
	sealed := c.aead.Seal(nonce, nonce, e.Payload, eventAAD(e))
	encoded, err := json.Marshal(base64.StdEncoding.EncodeToString(sealed))
	if err != nil {
		return Event{}, err
	}
	e.Payload = encoded
	e.Encrypted = true
	return e, nil
}
func (c *EventCipher) Open(e Event) (Event, error) {
	if !e.Encrypted {
		return Event{}, errors.New("unencrypted queue event rejected")
	}
	var encoded string
	if err := json.Unmarshal(e.Payload, &encoded); err != nil {
		return Event{}, errors.New("invalid queue ciphertext")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < c.aead.NonceSize() {
		return Event{}, errors.New("invalid queue ciphertext")
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, data[:n], data[n:], eventAAD(e))
	if err != nil {
		return Event{}, errors.New("queue payload authentication failed")
	}
	e.Payload = plain
	e.Encrypted = false
	return e, nil
}
