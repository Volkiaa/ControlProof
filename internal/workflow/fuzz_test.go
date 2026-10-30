package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/workspace"
)

func FuzzParse(f *testing.F) {
	seeds, err := filepath.Glob(filepath.Join("testdata", "golden", "*.yml"))
	if err != nil {
		f.Fatalf("glob fuzz seeds: %v", err)
	}
	for _, seed := range seeds {
		content, err := os.ReadFile(seed)
		if err != nil {
			f.Fatalf("read fuzz seed %s: %v", seed, err)
		}
		f.Add(content)
	}

	f.Fuzz(func(t *testing.T, content []byte) {
		file := workspace.File{Path: ".github/workflows/fuzz.yml", Content: content}
		first, firstErr := Parse(file, DefaultLimits)
		if errors.Is(firstErr, ErrInternal) {
			t.Fatalf("Parse() internal error: %v", firstErr)
		}

		second, secondErr := Parse(file, DefaultLimits)
		if errors.Is(secondErr, ErrInternal) {
			t.Fatalf("second Parse() internal error: %v", secondErr)
		}

		firstJSON, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal first IR: %v", err)
		}
		secondJSON, err := json.Marshal(second)
		if err != nil {
			t.Fatalf("marshal second IR: %v", err)
		}
		if !bytes.Equal(firstJSON, secondJSON) {
			t.Fatalf("Parse() is not deterministic\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
		}
	})
}
