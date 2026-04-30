package worker

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func parsePID(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid pid %q: %w", s, err)
	}
	return pid, nil
}

func localMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
