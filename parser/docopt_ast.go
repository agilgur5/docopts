//go:generate stringer -type=DocoptNodeType
package docopt_language

import (
	"github.com/docopt/docopts/grammar/lexer"
)

type DocoptNodeType int

// ast nodes types
const (
	Unmatched_node DocoptNodeType = -1
	Root           DocoptNodeType = 1 + iota
	Prologue
	Prologue_node
	Usage_section
	Usage
	Usage_line
	Prog_name
	Usage_short_option
	Usage_long_option
	Usage_argument
	Usage_punct
	Usage_indent
)

type DocoptAst struct {
	Type     DocoptNodeType
	Token    *lexer.Token
	Children []*DocoptAst
	parent   *DocoptAst
}

func (n *DocoptAst) AddNode(node_type DocoptNodeType, t *lexer.Token) *DocoptAst {
	new_node := &DocoptAst{
		Type:   node_type,
		Token:  t,
		parent: n,
	}
	n.Children = append(n.Children, new_node)
	return new_node
}
