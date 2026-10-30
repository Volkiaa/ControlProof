package workflow

import (
	"errors"
	"fmt"

	"github.com/Volkiaa/ControlProof/internal/model"
	"go.yaml.in/yaml/v4"
)

type yamlSyntaxError struct {
	err error
}

func (e *yamlSyntaxError) Error() string {
	return e.err.Error()
}

func (e *yamlSyntaxError) Unwrap() error {
	return e.err
}

type nodeSize struct {
	nodes int
	depth int
}

type nodeState uint8

const (
	nodeUnvisited nodeState = iota
	nodeVisiting
	nodeVisited
)

type complexityChecker struct {
	limits Limits
	states map[*yaml.Node]nodeState
	sizes  map[*yaml.Node]nodeSize
}

func checkComplexity(content []byte, limits Limits) error {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return &yamlSyntaxError{err: err}
	}

	checker := complexityChecker{
		limits: limits,
		states: map[*yaml.Node]nodeState{},
		sizes:  map[*yaml.Node]nodeSize{},
	}
	size, err := checker.visit(&root)
	if err != nil {
		return err
	}
	if size.nodes > limits.MaxNodes {
		return fmt.Errorf("%w: expanded YAML has %d nodes, limit is %d", ErrTooComplex, size.nodes, limits.MaxNodes)
	}
	if size.depth > limits.MaxDepth {
		return fmt.Errorf("%w: YAML nesting depth is %d, limit is %d", ErrTooComplex, size.depth, limits.MaxDepth)
	}
	return nil
}

func (c *complexityChecker) visit(node *yaml.Node) (nodeSize, error) {
	if node == nil {
		return nodeSize{}, nil
	}
	switch c.states[node] {
	case nodeVisiting:
		return nodeSize{}, fmt.Errorf("%w: recursive YAML alias", ErrTooComplex)
	case nodeVisited:
		return c.sizes[node], nil
	}

	c.states[node] = nodeVisiting

	var size nodeSize
	if node.Kind == yaml.AliasNode {
		target, err := c.visit(node.Alias)
		if err != nil {
			return nodeSize{}, err
		}
		size = target
	} else {
		size.nodes = 1
		for _, child := range node.Content {
			childSize, err := c.visit(child)
			if err != nil {
				return nodeSize{}, err
			}
			size.nodes += childSize.nodes
			if childSize.depth+1 > size.depth {
				size.depth = childSize.depth + 1
			}
			if size.nodes > c.limits.MaxNodes {
				return nodeSize{}, fmt.Errorf("%w: expanded YAML exceeds %d nodes", ErrTooComplex, c.limits.MaxNodes)
			}
		}
		if size.depth == 0 {
			size.depth = 1
		}
	}

	c.states[node] = nodeVisited
	c.sizes[node] = size
	return size, nil
}

func yamlDiagnostics(path string, err error) []model.Diag {
	var parserErr *yaml.ParserError
	if errors.As(err, &parserErr) {
		return []model.Diag{{
			Pos:     model.Pos{File: path, Line: parserErr.Line, Col: parserErr.Column},
			Kind:    "syntax-check",
			Message: parserErr.Message,
		}}
	}

	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		diags := make([]model.Diag, 0, len(typeErr.Errors))
		for _, entry := range typeErr.Errors {
			diags = append(diags, model.Diag{
				Pos:     model.Pos{File: path, Line: entry.Line, Col: entry.Column},
				Kind:    "syntax-check",
				Message: entry.Err.Error(),
			})
		}
		return diags
	}

	return []model.Diag{{
		Pos:     model.Pos{File: path},
		Kind:    "syntax-check",
		Message: err.Error(),
	}}
}
