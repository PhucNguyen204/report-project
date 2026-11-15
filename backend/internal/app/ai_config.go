package app

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

func ReadOpenAIKey(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	invalid := errors.New("invalid private OpenAI API key file")
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", invalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || (info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600) || info.Size() > 4096 {
		return "", invalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return "", invalid
	}
	key := strings.TrimSpace(string(raw))
	if len(key) < 20 {
		return "", invalid
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return "", invalid
		}
	}
	return key, nil
}
