package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const policyPath = ".controlproof.yaml"

const validPolicy = `schema_version: 1
repo_profile:
  provider: github
  repository_default_token_permissions: write
  fork_pr_workflow_approval: all-outside-collaborators
`

func TestDefault(t *testing.T) {
	policy := Default()
	if policy.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", policy.SchemaVersion)
	}
	if policy.RepoProfile.Provider != "github" {
		t.Fatalf("Provider = %q, want github", policy.RepoProfile.Provider)
	}
	if policy.RepoProfile.RepositoryDefaultTokenPermissions != Unknown {
		t.Fatalf("RepositoryDefaultTokenPermissions = %q, want %q", policy.RepoProfile.RepositoryDefaultTokenPermissions, Unknown)
	}
	if policy.RepoProfile.ForkPRWorkflowApproval != Unknown {
		t.Fatalf("ForkPRWorkflowApproval = %q, want %q", policy.RepoProfile.ForkPRWorkflowApproval, Unknown)
	}
	if policy.SHA256 != "" {
		t.Fatalf("SHA256 = %q, want empty", policy.SHA256)
	}
}

func TestParseValid(t *testing.T) {
	policy, err := Parse(policyPath, []byte(validPolicy))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if policy.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", policy.SchemaVersion)
	}
	if policy.RepoProfile.Provider != "github" {
		t.Errorf("Provider = %q, want github", policy.RepoProfile.Provider)
	}
	if policy.RepoProfile.RepositoryDefaultTokenPermissions != TokenWrite {
		t.Errorf("RepositoryDefaultTokenPermissions = %q, want %q", policy.RepoProfile.RepositoryDefaultTokenPermissions, TokenWrite)
	}
	if policy.RepoProfile.ForkPRWorkflowApproval != ApprovalAllOutsideCollaborators {
		t.Errorf("ForkPRWorkflowApproval = %q, want %q", policy.RepoProfile.ForkPRWorkflowApproval, ApprovalAllOutsideCollaborators)
	}
	sum := sha256.Sum256([]byte(validPolicy))
	if want := hex.EncodeToString(sum[:]); policy.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", policy.SHA256, want)
	}
}

func TestParseSHA256UsesRawBytes(t *testing.T) {
	content := []byte("# comment\r\nschema_version: 1\r\n")
	policy, err := Parse(policyPath, content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sum := sha256.Sum256(content)
	if want := hex.EncodeToString(sum[:]); policy.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", policy.SHA256, want)
	}
	if strings.ToLower(policy.SHA256) != policy.SHA256 {
		t.Errorf("SHA256 = %q, want lowercase hex", policy.SHA256)
	}
}

func TestParseDefaults(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		provider string
		token    string
		approval string
	}{
		{name: "repo_profile omitted", content: "schema_version: 1\n", provider: "github", token: Unknown, approval: Unknown},
		{name: "repo_profile empty", content: "schema_version: 1\nrepo_profile: {}\n", provider: "github", token: Unknown, approval: Unknown},
		{name: "repo_profile null", content: "schema_version: 1\nrepo_profile:\n", provider: "github", token: Unknown, approval: Unknown},
		{
			name:     "provider omitted",
			content:  "schema_version: 1\nrepo_profile:\n  repository_default_token_permissions: read\n  fork_pr_workflow_approval: none\n",
			provider: "github",
			token:    TokenRead,
			approval: ApprovalNone,
		},
		{
			name:     "token permissions omitted",
			content:  "schema_version: 1\nrepo_profile:\n  provider: github\n  fork_pr_workflow_approval: first-time-contributors\n",
			provider: "github",
			token:    Unknown,
			approval: ApprovalFirstTimeContributors,
		},
		{
			name:     "approval omitted",
			content:  "schema_version: 1\nrepo_profile:\n  provider: github\n  repository_default_token_permissions: write\n",
			provider: "github",
			token:    TokenWrite,
			approval: Unknown,
		},
		{
			name:     "field null",
			content:  "schema_version: 1\nrepo_profile:\n  provider:\n  repository_default_token_permissions:\n",
			provider: "github",
			token:    Unknown,
			approval: Unknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := Parse(policyPath, []byte(test.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if policy.RepoProfile.Provider != test.provider {
				t.Errorf("Provider = %q, want %q", policy.RepoProfile.Provider, test.provider)
			}
			if policy.RepoProfile.RepositoryDefaultTokenPermissions != test.token {
				t.Errorf("RepositoryDefaultTokenPermissions = %q, want %q", policy.RepoProfile.RepositoryDefaultTokenPermissions, test.token)
			}
			if policy.RepoProfile.ForkPRWorkflowApproval != test.approval {
				t.Errorf("ForkPRWorkflowApproval = %q, want %q", policy.RepoProfile.ForkPRWorkflowApproval, test.approval)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		want    []string
	}{
		{
			name:    "schema_version missing",
			content: "repo_profile:\n  provider: github\n",
			want:    []string{"schema_version"},
		},
		{
			name:    "schema_version zero",
			content: "schema_version: 0\n",
			line:    1,
			want:    []string{"schema_version", "0"},
		},
		{
			name:    "schema_version two",
			content: "schema_version: 2\n",
			line:    1,
			want:    []string{"schema_version", "2"},
		},
		{
			name:    "schema_version quoted",
			content: "schema_version: \"1\"\n",
			line:    1,
			want:    []string{"schema_version", "integer"},
		},
		{
			name:    "schema_version float",
			content: "schema_version: 1.0\n",
			line:    1,
			want:    []string{"schema_version", "integer"},
		},
		{
			name:    "schema_version boolean",
			content: "schema_version: true\n",
			line:    1,
			want:    []string{"schema_version", "integer"},
		},
		{
			name:    "schema_version sequence",
			content: "schema_version: [1]\n",
			line:    1,
			want:    []string{"schema_version"},
		},
		{
			name:    "unknown top-level key",
			content: "schema_version: 1\nowners:\n  - fingerprint: cp_3f9a\n",
			line:    2,
			want:    []string{`unknown key "owners"`},
		},
		{
			name:    "unknown repo_profile key",
			content: "schema_version: 1\nrepo_profile:\n  provider: github\n  foo: bar\n",
			line:    4,
			want:    []string{`unknown key "foo"`},
		},
		{
			name:    "duplicate top-level key",
			content: "schema_version: 1\nschema_version: 1\n",
			line:    2,
			want:    []string{"duplicate key", "schema_version"},
		},
		{
			name:    "duplicate repo_profile key",
			content: "schema_version: 1\nrepo_profile:\n  provider: github\n  provider: github\n",
			line:    4,
			want:    []string{"duplicate key", "provider"},
		},
		{
			name:    "non-string key",
			content: "schema_version: 1\n1: true\n",
			line:    2,
			want:    []string{"key must be a string"},
		},
		{
			name:    "provider not github",
			content: "schema_version: 1\nrepo_profile:\n  provider: gitlab\n",
			line:    3,
			want:    []string{"provider", "gitlab"},
		},
		{
			name:    "provider non-string",
			content: "schema_version: 1\nrepo_profile:\n  provider: 1\n",
			line:    3,
			want:    []string{"provider", "string"},
		},
		{
			name:    "token permissions invalid",
			content: "schema_version: 1\nrepo_profile:\n  repository_default_token_permissions: admin\n",
			line:    3,
			want:    []string{"repository_default_token_permissions", "admin"},
		},
		{
			name:    "token permissions non-string",
			content: "schema_version: 1\nrepo_profile:\n  repository_default_token_permissions: true\n",
			line:    3,
			want:    []string{"repository_default_token_permissions", "string"},
		},
		{
			name:    "approval invalid",
			content: "schema_version: 1\nrepo_profile:\n  fork_pr_workflow_approval: always\n",
			line:    3,
			want:    []string{"fork_pr_workflow_approval", "always"},
		},
		{
			name:    "approval non-string",
			content: "schema_version: 1\nrepo_profile:\n  fork_pr_workflow_approval: [none]\n",
			line:    3,
			want:    []string{"fork_pr_workflow_approval", "string"},
		},
		{
			name:    "repo_profile sequence",
			content: "schema_version: 1\nrepo_profile: []\n",
			line:    2,
			want:    []string{"repo_profile", "mapping"},
		},
		{
			name:    "repo_profile scalar",
			content: "schema_version: 1\nrepo_profile: github\n",
			line:    2,
			want:    []string{"repo_profile", "mapping"},
		},
		{
			name:    "root sequence",
			content: "- schema_version: 1\n",
			line:    1,
			want:    []string{"root must be a mapping"},
		},
		{
			name:    "root scalar",
			content: "schema_version\n",
			line:    1,
			want:    []string{"root must be a mapping"},
		},
		{
			name:    "root null",
			content: "null\n",
			line:    1,
			want:    []string{"root must be a mapping"},
		},
		{
			name:    "document marker only",
			content: "---\n",
			line:    2,
			want:    []string{"root must be a mapping"},
		},
		{
			name:    "empty file",
			content: "",
			want:    []string{"empty"},
		},
		{
			name:    "whitespace only",
			content: "   \n\n\n",
			want:    []string{"empty"},
		},
		{
			name:    "comments only",
			content: "# nothing here\n",
			want:    []string{"empty"},
		},
		{
			name:    "anchor",
			content: "schema_version: 1\nrepo_profile: &profile\n  provider: github\n",
			line:    2,
			want:    []string{"anchor"},
		},
		{
			name:    "alias",
			content: "schema_version: 1\nrepo_profile:\n  provider: &provider github\n  repository_default_token_permissions: *provider\n",
			line:    3,
			want:    []string{"anchor"},
		},
		{
			name:    "multiple documents",
			content: "schema_version: 1\n---\nschema_version: 1\n",
			line:    2,
			want:    []string{"multiple YAML documents"},
		},
		{
			name:    "syntax error",
			content: "schema_version: [1\n",
			line:    1,
			want:    []string{"parse YAML"},
		},
		{
			name:    "too large",
			content: strings.Repeat("a", MaxPolicyBytes+1),
			want:    []string{"limit"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := Parse(policyPath, []byte(test.content))
			if err == nil {
				t.Fatalf("Parse() error = nil, policy = %+v", policy)
			}
			if policy != nil {
				t.Errorf("Parse() policy = %+v, want nil", policy)
			}
			if !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("Parse() error = %v, want ErrInvalidPolicy", err)
			}
			message := err.Error()
			if !strings.HasPrefix(message, policyPath) {
				t.Errorf("error = %q, want prefix %q", message, policyPath)
			}
			if test.line > 0 {
				if prefix := fmt.Sprintf("%s:%d:", policyPath, test.line); !strings.HasPrefix(message, prefix) {
					t.Errorf("error = %q, want prefix %q", message, prefix)
				}
			}
			for _, want := range test.want {
				if !strings.Contains(message, want) {
					t.Errorf("error = %q, want substring %q", message, want)
				}
			}
		})
	}
}

func TestParseErrorLineUnknown(t *testing.T) {
	_, err := Parse(policyPath, []byte("repo_profile:\n  provider: github\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
	if strings.HasPrefix(err.Error(), policyPath+":1:") {
		t.Errorf("error = %q, want no line for a missing key", err)
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".controlproof.yaml")
	if err := os.WriteFile(path, []byte(validPolicy), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	policy, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if policy.RepoProfile.RepositoryDefaultTokenPermissions != TokenWrite {
		t.Errorf("RepositoryDefaultTokenPermissions = %q, want %q", policy.RepoProfile.RepositoryDefaultTokenPermissions, TokenWrite)
	}
	sum := sha256.Sum256([]byte(validPolicy))
	if want := hex.EncodeToString(sum[:]); policy.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", policy.SHA256, want)
	}
}

func TestLoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	policy, err := Load(path)
	if err == nil {
		t.Fatalf("Load() error = nil, policy = %+v", policy)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() error = %v, want os.ErrNotExist", err)
	}
	if !strings.HasPrefix(err.Error(), path) {
		t.Errorf("error = %q, want prefix %q", err.Error(), path)
	}
}

func TestLoadDirectory(t *testing.T) {
	path := t.TempDir()
	policy, err := Load(path)
	if err == nil {
		t.Fatalf("Load() error = nil, policy = %+v", policy)
	}
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Load() error = %v, want ErrInvalidPolicy", err)
	}
	if !strings.HasPrefix(err.Error(), path) {
		t.Errorf("error = %q, want prefix %q", err.Error(), path)
	}
}

func TestLoadNotRegularFile(t *testing.T) {
	const path = "/dev/null"
	info, err := os.Stat(path)
	if err != nil || info.Mode().IsRegular() || info.IsDir() {
		t.Skipf("%s is not a usable special file: %v", path, err)
	}
	policy, err := Load(path)
	if err == nil {
		t.Fatalf("Load() error = nil, policy = %+v", policy)
	}
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Load() error = %v, want ErrInvalidPolicy", err)
	}
	if !strings.HasPrefix(err.Error(), path) {
		t.Errorf("error = %q, want prefix %q", err.Error(), path)
	}
}

func TestLoadTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.yaml")
	content := make([]byte, MaxPolicyBytes+1)
	for i := range content {
		content[i] = ' '
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	policy, err := Load(path)
	if err == nil {
		t.Fatalf("Load() error = nil, policy = %+v", policy)
	}
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Load() error = %v, want ErrInvalidPolicy", err)
	}
	if !strings.HasPrefix(err.Error(), path) {
		t.Errorf("error = %q, want prefix %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "larger") {
		t.Errorf("error = %q, want size limit message", err)
	}
}

func TestLoadInvalidPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".controlproof.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 2\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Load() error = %v, want ErrInvalidPolicy", err)
	}
	if prefix := fmt.Sprintf("%s:1:", path); !strings.HasPrefix(err.Error(), prefix) {
		t.Errorf("error = %q, want prefix %q", err.Error(), prefix)
	}
}
