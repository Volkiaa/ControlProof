package workflow

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/rhysd/actionlint"
)

func (c *converter) exprs(value string, base *actionlint.Pos) []model.Expr {
	var out []model.Expr
	for offset := 0; offset < len(value); {
		start := strings.Index(value[offset:], "${{")
		if start < 0 {
			break
		}
		start += offset
		inner := start + len("${{")

		raw, next := expressionSpan(value, inner)

		expr := model.Expr{
			Raw: strings.TrimSpace(raw),
			Pos: c.exprPos(base, value[:start]),
		}
		expr.Refs, expr.Err = parseExpression(expr.Raw)
		out = append(out, expr)

		offset = next
	}
	return out
}

func expressionSpan(value string, inner int) (string, int) {
	if _, offset, err := actionlint.LexExpression(value[inner:]); err == nil {
		return value[inner : inner+offset-len("}}")], inner + offset
	}
	end := strings.Index(value[inner:], "}}")
	if end < 0 {
		return value[inner:], len(value)
	}
	return value[inner : inner+end], inner + end + len("}}")
}

func (c *converter) exprPos(base *actionlint.Pos, prefix string) model.Pos {
	pos := c.pos(base)
	if base == nil {
		return pos
	}
	newlines := strings.Count(prefix, "\n")
	pos.Line += newlines
	if newlines == 0 {
		pos.Col += utf8.RuneCountInString(prefix)
	} else {
		tail := prefix[strings.LastIndexByte(prefix, '\n')+1:]
		pos.Col = utf8.RuneCountInString(tail) + 1
	}
	return pos
}

func parseExpression(raw string) ([]string, string) {
	lexer := actionlint.NewExprLexer(raw + "}}")
	node, err := actionlint.NewExprParser().Parse(lexer)
	if err != nil {
		return nil, err.Message
	}

	refs := expressionRefs(node)
	return refs, ""
}

func expressionRefs(node actionlint.ExprNode) []string {
	var refs []string
	var chain func(actionlint.ExprNode) (string, bool)
	var walk func(actionlint.ExprNode)

	chain = func(n actionlint.ExprNode) (string, bool) {
		switch n := n.(type) {
		case *actionlint.VariableNode:
			return n.Name, true
		case *actionlint.ObjectDerefNode:
			if base, ok := chain(n.Receiver); ok {
				return base + "." + n.Property, true
			}
		case *actionlint.IndexAccessNode:
			if base, ok := chain(n.Operand); ok {
				if index, ok := n.Index.(*actionlint.StringNode); ok {
					return base + "." + index.Value, true
				}
				return base + ".*", true
			}
		case *actionlint.ArrayDerefNode:
			if base, ok := chain(n.Receiver); ok {
				return base + ".*", true
			}
		}
		return "", false
	}

	var walkIndexes func(actionlint.ExprNode)
	walkIndexes = func(n actionlint.ExprNode) {
		switch n := n.(type) {
		case *actionlint.ObjectDerefNode:
			walkIndexes(n.Receiver)
		case *actionlint.ArrayDerefNode:
			walkIndexes(n.Receiver)
		case *actionlint.IndexAccessNode:
			walkIndexes(n.Operand)
			if _, literal := n.Index.(*actionlint.StringNode); !literal {
				walk(n.Index)
			}
		}
	}

	walk = func(n actionlint.ExprNode) {
		if path, ok := chain(n); ok {
			refs = append(refs, path)
			walkIndexes(n)
			return
		}
		switch n := n.(type) {
		case *actionlint.FuncCallNode:
			for _, arg := range n.Args {
				walk(arg)
			}
		case *actionlint.NotOpNode:
			walk(n.Operand)
		case *actionlint.CompareOpNode:
			walk(n.Left)
			walk(n.Right)
		case *actionlint.LogicalOpNode:
			walk(n.Left)
			walk(n.Right)
		case *actionlint.IndexAccessNode:
			walk(n.Operand)
			walk(n.Index)
		case *actionlint.ObjectDerefNode:
			walk(n.Receiver)
		case *actionlint.ArrayDerefNode:
			walk(n.Receiver)
		}
	}

	walk(node)

	sort.Strings(refs)
	unique := refs[:0]
	for _, ref := range refs {
		if len(unique) == 0 || unique[len(unique)-1] != ref {
			unique = append(unique, ref)
		}
	}
	return unique
}
