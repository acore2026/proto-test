package nas

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func LoadTemplate(path string, isHex bool) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !isHex {
		return raw, nil
	}
	s := strings.TrimSpace(string(raw))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	out, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex template: %w", err)
	}
	return out, nil
}
