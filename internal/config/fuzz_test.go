package config

import (
	"errors"
	"testing"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(validPolicy))
	f.Add([]byte("schema_version: 1\n"))
	f.Add([]byte("schema_version: 2\n"))
	f.Add([]byte("schema_version: 1\nrepo_profile: &p\n  provider: github\n"))
	f.Add([]byte("schema_version: 1\n---\nschema_version: 1\n"))
	f.Add([]byte("- schema_version: 1\n"))

	f.Fuzz(func(t *testing.T, content []byte) {
		policy, err := Parse(policyPath, content)
		if err != nil {
			if !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("Parse() error = %v, want ErrInvalidPolicy", err)
			}
			return
		}
		if policy == nil {
			t.Fatal("Parse() policy = nil with nil error")
		}
		if policy.SchemaVersion != 1 {
			t.Fatalf("Parse() SchemaVersion = %d, want 1", policy.SchemaVersion)
		}
	})
}
