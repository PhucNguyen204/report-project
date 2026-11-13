package app

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/mail"
	"os"
	"strings"
	"syscall"

	"golang.org/x/oauth2/google"
)

const FCMMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"

func ReadFCMCredentials(ctx context.Context, path string) (*google.Credentials, error) {
	invalid := errors.New("invalid FCM service account credentials")
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, invalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0400 == 0 || info.Size() > 65536 {
		return nil, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, invalid
	}
	var in struct {
		Type           string `json:"type"`
		ClientEmail    string `json:"client_email"`
		PrivateKey     string `json:"private_key"`
		TokenURI       string `json:"token_uri"`
		UniverseDomain string `json:"universe_domain"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Type != string(google.ServiceAccount) || (in.TokenURI != "" && in.TokenURI != google.JWTTokenURL) || (in.UniverseDomain != "" && in.UniverseDomain != "googleapis.com") {
		return nil, invalid
	}
	address, err := mail.ParseAddress(in.ClientEmail)
	if err != nil || address.Address != in.ClientEmail || address.Name != "" || !strings.HasSuffix(strings.ToLower(in.ClientEmail), ".gserviceaccount.com") {
		return nil, invalid
	}
	block, rest := pem.Decode([]byte(in.PrivateKey))
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, invalid
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, invalid
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, invalid
	}
	creds, err := google.CredentialsFromJSONWithType(ctx, raw, google.ServiceAccount, FCMMessagingScope)
	if err != nil {
		return nil, invalid
	}
	return creds, nil
}
