package nas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTemplateHex(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "nas.hex")
	if err := os.WriteFile(p, []byte("01 02 0A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadTemplate(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 3 || b[2] != 0x0A {
		t.Fatalf("unexpected bytes: %v", b)
	}
}
