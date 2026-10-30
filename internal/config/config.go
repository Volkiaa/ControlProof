package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"go.yaml.in/yaml/v4"
)

var ErrInvalidPolicy = errors.New("invalid policy")

const MaxPolicyBytes = 64 << 10

const (
	TokenRead                       = "read"
	TokenWrite                      = "write"
	Unknown                         = "unknown"
	ApprovalAllOutsideCollaborators = "all-outside-collaborators"
	ApprovalFirstTimeContributors   = "first-time-contributors"
	ApprovalNone                    = "none"
)

const (
	providerGitHub  = "github"
	schemaVersionV1 = 1
)

type RepoProfile struct {
	Provider                          string `json:"provider"`
	RepositoryDefaultTokenPermissions string `json:"repository_default_token_permissions"`
	ForkPRWorkflowApproval            string `json:"fork_pr_workflow_approval"`
}

type Policy struct {
	SchemaVersion int         `json:"schema_version"`
	RepoProfile   RepoProfile `json:"repo_profile"`
	SHA256        string      `json:"sha256,omitempty"`
}

func Default() *Policy {
	return &Policy{
		SchemaVersion: schemaVersionV1,
		RepoProfile: RepoProfile{
			Provider:                          providerGitHub,
			RepositoryDefaultTokenPermissions: Unknown,
			ForkPRWorkflowApproval:            Unknown,
		},
	}
}

func Parse(path string, content []byte) (*Policy, error) {
	if len(content) > MaxPolicyBytes {
		return nil, invalidf(path, 0, "policy content is %d bytes, limit is %d", len(content), MaxPolicyBytes)
	}
	root, err := decodeDocument(path, content)
	if err != nil {
		return nil, err
	}
	if err := checkAnchors(path, root); err != nil {
		return nil, err
	}
	policy, err := parseRoot(path, root)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	policy.SHA256 = hex.EncodeToString(sum[:])
	return policy, nil
}

func Load(path string) (*Policy, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if info.IsDir() {
		return nil, invalidf(path, 0, "is a directory")
	}
	if !info.Mode().IsRegular() {
		return nil, invalidf(path, 0, "is not a regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !opened.Mode().IsRegular() {
		return nil, invalidf(path, 0, "is not a regular file")
	}

	content, err := io.ReadAll(io.LimitReader(file, MaxPolicyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(content) > MaxPolicyBytes {
		return nil, invalidf(path, 0, "policy file is larger than %d bytes", MaxPolicyBytes)
	}
	return Parse(path, content)
}

func decodeDocument(path string, content []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, invalidf(path, 0, "policy is empty")
		}
		return nil, invalidf(path, yamlErrorLine(err), "parse YAML: %s", yamlErrorMessage(err))
	}

	var second yaml.Node
	switch err := decoder.Decode(&second); {
	case err == nil:
		return nil, invalidf(path, second.Line, "multiple YAML documents")
	case !errors.Is(err, io.EOF):
		return nil, invalidf(path, yamlErrorLine(err), "parse YAML: %s", yamlErrorMessage(err))
	}

	if document.Kind != yaml.DocumentNode {
		return &document, nil
	}
	if len(document.Content) != 1 {
		return nil, invalidf(path, document.Line, "document has no root node")
	}
	return document.Content[0], nil
}

func yamlErrorLine(err error) int {
	var parserErr *yaml.ParserError
	if errors.As(err, &parserErr) {
		return parserErr.Line
	}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) && len(typeErr.Errors) > 0 {
		return typeErr.Errors[0].Line
	}
	return 0
}

func yamlErrorMessage(err error) string {
	var parserErr *yaml.ParserError
	if errors.As(err, &parserErr) {
		return parserErr.Message
	}
	return err.Error()
}

func checkAnchors(path string, node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return invalidf(path, node.Line, "YAML alias %q is not allowed", node.Value)
	}
	if node.Anchor != "" {
		return invalidf(path, node.Line, "YAML anchor %q is not allowed", node.Anchor)
	}
	for _, child := range node.Content {
		if err := checkAnchors(path, child); err != nil {
			return err
		}
	}
	return nil
}

func parseRoot(path string, root *yaml.Node) (*Policy, error) {
	if root.Kind != yaml.MappingNode {
		return nil, invalidf(path, root.Line, "root must be a mapping, got %s", describe(root))
	}

	policy := Default()
	hasVersion := false
	seen := make(map[string]int)
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		name, err := keyName(path, key)
		if err != nil {
			return nil, err
		}
		if first, ok := seen[name]; ok {
			return nil, invalidf(path, key.Line, "duplicate key %q, first defined on line %d", name, first)
		}
		seen[name] = key.Line

		switch name {
		case "schema_version":
			version, err := parseSchemaVersion(path, value)
			if err != nil {
				return nil, err
			}
			policy.SchemaVersion = version
			hasVersion = true
		case "repo_profile":
			if err := parseRepoProfile(path, value, &policy.RepoProfile); err != nil {
				return nil, err
			}
		default:
			return nil, invalidf(path, key.Line, "unknown key %q", name)
		}
	}

	if !hasVersion {
		return nil, invalidf(path, 0, "schema_version is required")
	}
	return policy, nil
}

func parseSchemaVersion(path string, node *yaml.Node) (int, error) {
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
		return 0, invalidf(path, node.Line, "schema_version must be an integer, got %s", describe(node))
	}
	version, err := strconv.ParseInt(node.Value, 0, 64)
	if err != nil {
		return 0, invalidf(path, node.Line, "schema_version %q is not an integer", node.Value)
	}
	if version != schemaVersionV1 {
		return 0, invalidf(path, node.Line, "schema_version %d is not supported, want %d", version, schemaVersionV1)
	}
	return int(version), nil
}

func parseRepoProfile(path string, node *yaml.Node, profile *RepoProfile) error {
	if isNull(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return invalidf(path, node.Line, "repo_profile must be a mapping, got %s", describe(node))
	}

	seen := make(map[string]int)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		name, err := keyName(path, key)
		if err != nil {
			return err
		}
		if first, ok := seen[name]; ok {
			return invalidf(path, key.Line, "duplicate key %q, first defined on line %d", name, first)
		}
		seen[name] = key.Line

		switch name {
		case "provider":
			if err := parseEnum(path, "provider", value, &profile.Provider, []string{providerGitHub}); err != nil {
				return err
			}
		case "repository_default_token_permissions":
			if err := parseEnum(path, "repository_default_token_permissions", value, &profile.RepositoryDefaultTokenPermissions, []string{TokenRead, TokenWrite, Unknown}); err != nil {
				return err
			}
		case "fork_pr_workflow_approval":
			if err := parseEnum(path, "fork_pr_workflow_approval", value, &profile.ForkPRWorkflowApproval, []string{ApprovalAllOutsideCollaborators, ApprovalFirstTimeContributors, ApprovalNone, Unknown}); err != nil {
				return err
			}
		default:
			return invalidf(path, key.Line, "unknown key %q", name)
		}
	}
	return nil
}

func parseEnum(path, field string, node *yaml.Node, target *string, allowed []string) error {
	if isNull(node) {
		return nil
	}
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
		return invalidf(path, node.Line, "%s must be a string, got %s", field, describe(node))
	}
	for _, candidate := range allowed {
		if node.Value == candidate {
			*target = node.Value
			return nil
		}
	}
	return invalidf(path, node.Line, "%s %q must be one of %v", field, node.Value, allowed)
}

func keyName(path string, node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
		return "", invalidf(path, node.Line, "mapping key must be a string, got %s", describe(node))
	}
	return node.Value, nil
}

func isNull(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null"
}

func describe(node *yaml.Node) string {
	switch node.Kind {
	case yaml.AliasNode:
		return "alias"
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return node.ShortTag()
	}
	return "unknown"
}

func invalidf(path string, line int, format string, args ...any) error {
	location := path
	if line > 0 {
		location = fmt.Sprintf("%s:%d", path, line)
	}
	return fmt.Errorf("%s: %s: %w", location, fmt.Sprintf(format, args...), ErrInvalidPolicy)
}
