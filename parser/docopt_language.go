package docopt_language

import (
	"container/list"
	"fmt"
	"github.com/docopt/docopts/grammar/lexer"
	"github.com/docopt/docopts/grammar/lexer_state"
	"github.com/docopt/docopts/grammar/token_docopt"
	"strings"
)

type DocoptParser struct {
	s             *lexer_state.StateLexer
	Prog_name     string
	current_token *lexer.Token
	next_token    *lexer.Token
	tokens        *list.List

	// map symbols <=> name
	symbols_name map[rune]string
	all_symbols  map[string]rune

	Error_count  int
	max_error    int
	Errors       []error
	ast          *DocoptAst
	current_node *DocoptAst
	options_node *DocoptAst

	lexer_state_changed bool
	run                 bool
}

// Reason for a consumer to leave
type Reason_Value int

const (
	Reason_Error       Reason_Value = -1
	Reason_TWO_NEWLINE              = 1 + iota
	Reason_PROG_NAME_sequence
	Reason_EOF_reached
	Reason_Continue
)

type Reason struct {
	Value   Reason_Value
	Leaving bool
}

// golang doesn't have const complex type
var (
	Error              = Reason{Reason_Error, true}
	TWO_NEWLINE        = Reason{Reason_TWO_NEWLINE, true}
	PROG_NAME_sequence = Reason{Reason_EOF_reached, true}
	EOF_reached        = Reason{Reason_PROG_NAME_sequence, true}
	Continue           = Reason{Reason_Continue, false}
)

type Consume_method func() error
type Consume_func struct {
	name    string
	consume Consume_method
}

var (
	NEWLINE    rune
	SECTION    rune
	PROG_NAME  rune
	USAGE      rune
	SHORT      rune
	LONG       rune
	ARGUMENT   rune
	PUNCT      rune
	IDENT      rune
	LONG_BLANK rune
	DEFAULT    rune
)

func ParserInit(source []byte) (*DocoptParser, error) {
	states, err := lexer_state.CreateStateLexer(token_docopt.All_states, "state_Prologue")
	if err != nil {
		return nil, fmt.Errorf("ParserInit: fail to init lexer_state: %v", err)
	}

	// initialize the Lexer with the source
	states.State_auto_change = false
	states.InitSource(source)

	p := &DocoptParser{
		s: states,

		Prog_name:     "",
		current_token: nil,
		next_token:    nil,
		options_node:  nil,
		tokens:        list.New(),

		symbols_name: lexer.SymbolsByRune(states),
		all_symbols:  states.Symbols(),

		Error_count:         0,
		max_error:           10,
		lexer_state_changed: false,
		run:                 true,
	}

	// TODO: initialize token in token_docopt
	NEWLINE = p.all_symbols["NEWLINE"]
	SECTION = p.all_symbols["SECTION"]
	PROG_NAME = p.all_symbols["PROG_NAME"]
	USAGE = p.all_symbols["USAGE"]
	SHORT = p.all_symbols["SHORT"]
	LONG = p.all_symbols["LONG"]
	ARGUMENT = p.all_symbols["ARGUMENT"]
	PUNCT = p.all_symbols["PUNCT"]
	IDENT = p.all_symbols["IDENT"]
	LONG_BLANK = p.all_symbols["LONG_BLANK"]
	DEFAULT = p.all_symbols["DEFAULT"]

	return p, nil
}

func (p *DocoptParser) NextToken() {
	if p.next_token != nil && p.lexer_state_changed {
		p.s.Reject(p.next_token)
		p.lexer_state_changed = false
		p.next_token = nil
		p.tokens.Remove(p.tokens.Back())
	}

	if p.next_token != nil {
		p.current_token = p.next_token
	} else {
		p.try_get_NextToken(&p.current_token)
	}

	p.try_get_NextToken(&p.next_token)

	if p.Error_count >= p.max_error {
		p.FatalError("too many error leaving")
	}

	p.tokens.PushBack(p.current_token)
}

func (p *DocoptParser) reject_current_token() {
	p.s.Reject(p.current_token)
	p.next_token = nil
	p.current_token = nil
	p.tokens.Remove(p.tokens.Back())
}

func (p *DocoptParser) try_get_NextToken(token_to_store **lexer.Token) error {
	if p.Error_count >= p.max_error {
		return fmt.Errorf("try_get_NextToken: already too many errors")
	}

	t, err := p.s.Next()
	if err == nil {
		*token_to_store = &t
	} else {
		// error collector
		p.AddError(err)

		if p.Error_count >= p.max_error {
			return fmt.Errorf("try_get_NextToken: too many errors")
		}

		p.s.Discard(1)
		return p.try_get_NextToken(token_to_store)
	}

	return nil
}

func (p *DocoptParser) Eat(f Consume_method) error {
	if err := f(); err != nil {
		return err
	}
	return nil
}

func (p *DocoptParser) FatalError(msg string) {
	for _, e := range p.Errors {
		fmt.Println(e)
	}
	p.run = false
}

func (p *DocoptParser) AddError(e error) error {
	p.Errors = append(p.Errors, e)
	p.Error_count++
	return e
}

// create a Consume_method from a strind a a method name
// TODO: what is the benefit of this call?
func consumer(name string, method Consume_method) Consume_func {
	return Consume_func{
		name:    name,
		consume: method,
	}
}

// Parse() main parser entry point, for parsing docopt syntax
// pre-condition: lexer must be initialized with the []byte of the
// text to parse, See: ParserInit()
func (p *DocoptParser) Parse() *DocoptAst {
	p.CreateNode(Root, nil)

	// list parsing_step
	parse := []Consume_func{
		consumer("Consume_Prologue", p.Consume_Prologue),
		consumer("Consume_Usage", p.Consume_Usage),
		consumer("Consume_Free_Section", p.Consume_Free_section),
		consumer("Consume_Options", p.Consume_Options),
		consumer("Consume_Free_Section", p.Consume_Free_section),
	}

	for _, step := range parse {
		if err := step.consume(); err != nil {
			fmt.Printf("error: %s: %v\n", step.name, err)
			p.Error_count++
		}
	}

	return p.ast
}

// same as Opts in legacy docopt-go
type DocoptOpts map[string]interface{}

// algorithm derive from docopt.ParseArgs() docopt-go/docopt.go
func (p *DocoptParser) MatchArgs(argv []string) (args DocoptOpts, err error) {
	if p.ast == nil {
		err = fmt.Errorf("error: ast is nil")
		return
	}

	//if len(usageSections) == 0 {
	//	err = newLanguageError("\"usage:\" (case-insensitive) not found.")
	//	return
	//}
	//if len(usageSections) > 1 {
	//	err = newLanguageError("More than one \"usage:\" (case-insensitive).")
	//	return
	//}

	// options := parseDefaults(doc)
	// formal, err := FormalUsage(usage)
	// pat, err := ParsePattern(formal, &options)

	return
}

// simple call to our tokenizer for testing debuging purpose
func (p *DocoptParser) Print_all_token() error {
	for p.run {
		p.NextToken()
		fmt.Printf("%s:%q\n", p.symbols_name[p.current_token.Type], p.current_token.Value)
		if p.current_token.Type == lexer.EOF {
			break
		}
	}

	if p.run {
		return nil
	} else {
		return fmt.Errorf("Print_all_token: parser stoped")
	}
}

func (p *DocoptParser) CreateNode(node_type DocoptNodeType, token *lexer.Token) {
	if p.current_node == nil {
		p.current_node = &DocoptAst{
			Type:  node_type,
			Token: token,
		}
	} else {
		p.current_node = p.current_node.AddNode(node_type, token)
	}

	if p.ast == nil {
		p.ast = p.current_node
	}
}

type Consume_token_method func(*DocoptParser) (Reason, error)
type Consumer_Definition struct {
	create_node        bool
	toplevel           *DocoptAst
	toplevel_node      DocoptNodeType
	save_current_node  bool
	reject_first_token bool
	consume_token      Consume_token_method
}

// generic Consume loop method with token
func (p *DocoptParser) Consume_loop(c *Consumer_Definition) (Reason, error) {
	if c.create_node {
		c.toplevel = p.current_node.AddNode(c.toplevel_node, nil)
		// CREATE toplevel node: p.CreateNode(Prologue, nil)
	}

	var saved_current_node *DocoptAst = nil
	if c.save_current_node {
		saved_current_node = p.current_node
	}

	if c.reject_first_token {
		p.reject_current_token()
	}

	var reason Reason
	var err error = nil

	// loop
	for p.run {
		p.NextToken()

		reason, err = c.consume_token(p)
		if err != nil || reason.Leaving {
			break
		}
	}

	if p.run {
		// RESTORE SAVED NODE
		if c.save_current_node {
			p.current_node = saved_current_node
		}
		return reason, err
	} else {
		return Error, fmt.Errorf("%s: parser stoped: %s", p.current_node.Type, err)
	}
}

func (p *DocoptParser) Consume_Prologue() error {
	// we start parsing we are at Root node
	p.CreateNode(Prologue, nil)

	for p.run {
		p.NextToken()

		if p.current_token.Type == USAGE {
			// leaving Prologue
			usage_node := p.ast.AddNode(Usage_section, nil)
			usage_node.AddNode(Usage, p.current_token)
			p.current_node = usage_node
			return nil
		}

		p.current_node.AddNode(Prologue_node, p.current_token)

		if p.current_token.Type == lexer.EOF {
			// Prologue must leave on an Usage token
			return fmt.Errorf("EOF encountered will parsing Prologue, without 'Usage:' found")
		}
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

func (p *DocoptParser) Consume_Usage() error {
	// Usage   = USAGE , First_Program_Usage | { Program_Usage } ;
	// First_Program_Usage = PROG_NAME , [ Expr ] ;
	// (*
	//  PROG_NAME is catched at first definition and stay the same literal for all the parsing
	//  Program_Usage can be break multi-line: Indent + PROG_NAME will start a new Program_Usage
	//
	//  Usage: ./my_program.sh [-h] [--lovely-option] FILENAME
	//         ./my_program.sh another LINE OF --usage
	//         my_program      will continue [the] [--above] <usage-definition>
	//
	//  PROG_NAME  on first usage parsing it becomes: "./my_program.sh"
	// *)
	// PROG_NAME = ? any non space characters ? ;
	// Program_Usage = Indent , PROG_NAME  [ Expr ] ;

	if err := p.Consume_First_Program_Usage(); err != nil {
		return err
	}

	if err := p.Consume_Usage_line(); err != nil {
		return err
	}

	return nil
}

// Consume_Usage_line: take all Usage_line after we matched Consume_First_Program_Usage
// the current node is Usage_line with one Children: Prog_name
// (the PROG_NAME token has been dynamically changeg)
// Every time we match again a sequence: NEWLINE LONG_BLANK PROG_NAME
// we start a new Usage_line
func (p *DocoptParser) Consume_Usage_line() error {
	p.Change_lexer_state("state_Usage_Line")
	var reason Reason
	var err error

	// ensure we got the correct initial condition for adding Usage_line nodes
	usage_section := p.current_node.Parent
	if usage_section.Type != Usage_section {
		return fmt.Errorf("wrong node Type: '%s' expected Usage_section", usage_section.Type)
	}

	consume_Expr := Consumer_Definition{
		create_node:        false,
		toplevel:           nil,
		toplevel_node:      NONE_node,
		save_current_node:  true,
		reject_first_token: true,
		consume_token:      Consume_Usage_Expr,
	}

	for p.run {
		p.NextToken()
		if p.has_reach_EOF(&reason) {
			// assert leaving condition are met
			return nil
		}

		// wrong PROG_NAME token matching
		if p.current_token.Type == PROG_NAME {
			if p.Prog_name != p.current_token.Value {
				return fmt.Errorf(
					"Consume_Usage_line:(%s) PROG_NAME encountered with a distinct value:%s, invalid Token: '%v' extracted with: %s",
					p.s.Current_state.State_name,
					p.Prog_name,
					p.current_token,
					p.current_token.State_name)
			}
			continue
		}

		if p.current_token.Type == USAGE {
			return fmt.Errorf("Consume_Usage_line: USAGE invalid Token: %v", p.current_token)
		}

		// eat a single Usage_line starting with an Usage_Expr
		// current_token is already pointing to the next item the lexer got, following PROG_NAME
		if reason, err = p.Consume_loop(&consume_Expr); err != nil {
			return err
		}

		switch reason {
		case TWO_NEWLINE, EOF_reached:
			// normal exit condition
			return nil
		case PROG_NAME_sequence:
			// start parsing a new Usage_line
			usage_line := usage_section.AddNode(Usage_line, nil)
			usage_line.AddNode(Prog_name, p.current_token)
			p.current_node = usage_line.AddNode(Usage_Expr, nil)
			continue
		default:
			p.FatalError("switch default not supposed to be reached")
		}
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

// following PROG_NAME detection Expr is optional
// Expr could be multiline if Prog_name don't repeat (TODO: ref docopt-go/)
func Consume_Usage_Expr(p *DocoptParser) (Reason, error) {
	var err error = nil
	var n DocoptNodeType
	var reason Reason

	if p.has_reach_EOF(&reason) || p.has_reach_two_NEWLINE(&reason, true) || p.has_reach_PROG_NAME(&reason) {
		// TODO: assert leaving condition are met
		return reason, err
	}

	// assign a token
	switch p.current_token.Type {
	case NEWLINE, LONG_BLANK:
		// skip
		return Continue, nil
	case SHORT:
		n = Usage_short_option
	case LONG:
		n = Usage_long_option
	case ARGUMENT:
		n = Usage_argument
	case PUNCT:
		switch p.current_token.Value {
		case "[":
			n = Usage_optional_group
		case "(":
			n = Usage_required_group
		case "...":
			p.ensure_node(Usage_Expr)
			if err := p.Consume_ellipsis(); err != nil {
				return Error, err
			}
			return Continue, nil
		case "=":
			p.ensure_node(Usage_Expr)
			if err := p.Consume_assign(p.next_token); err != nil {
				return Error, err
			}
			// consume ARGUMENT assigned
			p.NextToken()
			return Continue, nil
		case "|":
			if p.current_node.Type != Usage_Expr {
				err = fmt.Errorf("%s: current node error: %v", p.current_node.Type, p.current_token)
				return Error, err
			}

			parent := p.current_node.Parent
			if parent.Type == Usage_line {
				// first node is Prog_name, it wont goes to the Group_alternative
				group_node := &DocoptAst{
					Type:     Group_alternative,
					Token:    p.current_token,
					Parent:   parent,
					Children: parent.Children[1:],
				}

				// update the Parent
				for _, c := range group_node.Children {
					c.Parent = group_node
				}

				// recreate Children keeping only Prog_name first node and the new group_node
				parent.Children = []*DocoptAst{parent.Children[0], group_node}
				// prepare the next new container node
				expr := group_node.AddNode(Usage_Expr, nil)
				p.current_node = expr
			} else {
				// token eaten, we create a new Usage_Expr then the next token will continue at this node
				p.current_node = p.current_node.Parent.AddNode(Usage_Expr, nil)
			}
			return Continue, nil
		default:
			return Error, fmt.Errorf("unmatched PUNC: %s", p.current_token.GoString())
		} // end switch PUNCT

		// we found some PUNCT so we modify current_node
		p.ensure_node(Usage_Expr)

		if n == Usage_optional_group || n == Usage_required_group {
			// try to match a group required or optional
			if err := p.Consume_group(n); err != nil {
				return Error, err
			}

			// assert
			if p.current_node.Type != Usage_Expr {
				p.FatalError(fmt.Sprintf("p.Consume_group(%s) did not restore current_node: %s",
					n,
					p.current_node.Type))
			}
			return Continue, nil
		}
		// else: unmatched PUNCT will added to the AST
		// end handling PUNCT in Usage_Expr
	case IDENT:
		n = Usage_command
	default:
		return Error, p.AddError(
			fmt.Errorf("Consume_Usage_Expr: Unmatched token: %s", p.current_token.GoString()))
	} // end switch Token.Type

	p.ensure_node(Usage_Expr)
	p.current_node.AddNode(n, p.current_token)

	return reason, err
} // end Consume_Usage_Expr

func (p *DocoptParser) has_reach_EOF(reason *Reason) bool {
	if p.current_token.Type == lexer.EOF {
		*reason = EOF_reached
		return true
	}
	return false
}

func (p *DocoptParser) has_reach_two_NEWLINE(reason *Reason, consume_newline bool) bool {
	if p.current_token.Type == NEWLINE {
		if p.next_token.Type == NEWLINE {
			// two consecutive NEWLINE
			if consume_newline {
				p.NextToken()
			}
			*reason = TWO_NEWLINE
			return true
		}
	}
	return false
}

func (p *DocoptParser) has_reach_PROG_NAME(reason *Reason) bool {
	if p.current_token != nil && p.current_token.Type == PROG_NAME &&
		p.current_token.Value == p.Prog_name {
		// check sequence
		if p.tokens.Len() > 3 {
			t := p.tokens.Back()
			if t.Prev().Prev().Value.(*lexer.Token).Type == NEWLINE &&
				t.Prev().Value.(*lexer.Token).Type == LONG_BLANK {
				*reason = PROG_NAME_sequence
				return true
			}
		}
	}
	return false
}

// func (p *DocoptParser) has_reach_token(token_type rune, token_value *string) bool {
// 	if p.current_node.Token != nil && p.current_node.Token.Type == token_type {
// 		if token_value != nil {
// 			return p.current_node.Token.Value == *token_value
// 		}
// 		return true
// 	}
// 	return false
// }

func (p *DocoptParser) Consume_ellipsis() error {
	nb := len(p.current_node.Children)
	if nb > 0 {
		p.current_node.Children[nb-1].Repeat = true
	} else {
		return fmt.Errorf("%s: elipsis not expected on such node without Children, invalid Token: %v",
			p.current_node.Type, p.current_token)
	}
	return nil
}

// Consume_group assume that we are in a open Group_alternative
func (p *DocoptParser) Consume_group(group_type DocoptNodeType) error {
	group := p.current_node.AddNode(group_type, nil)
	saved_current_node := p.current_node
	p.current_node = group
	var err error = nil
	var n DocoptNodeType
forLoop:
	for p.run {
		p.NextToken()
		switch p.current_token.Type {
		case lexer.EOF, PROG_NAME:
			err = fmt.Errorf("%s: %s unexpected, missing closing bracket ']'",
				p.current_node.Type,
				p.symbols_name[p.current_token.Type])
			break forLoop
		case USAGE:
			err = fmt.Errorf("%s: USAGE invalid Token: %v", p.current_node.Type, p.current_token)
			break forLoop
		case NEWLINE:
			if p.next_token.Type == NEWLINE {
				// two consecutive NEWLINE
				err = fmt.Errorf("%s: 2 consecutive NEWLINE invalid Token: %v", p.current_node.Type, p.current_token)
				break forLoop
			}
			continue
		case SHORT:
			n = Usage_short_option
		case LONG:
			n = Usage_long_option
		case ARGUMENT:
			n = Usage_argument
		case PUNCT:
			switch p.current_token.Value {
			case "[":
				if err = p.Consume_group(Usage_optional_group); err != nil {
					break forLoop
				}
				continue
			case "(":
				if err = p.Consume_group(Usage_required_group); err != nil {
					break forLoop
				}
				continue
			case "|":
				if p.current_node.Type != Group_alternative {
					// move actual Children to a new Group_alternative node
					alternative := p.current_node.Replace_children_with_group(Group_alternative)
					p.current_node = alternative
				}
				// else will be appended (do we need to create a new node?)
				continue
			case "]":
				if p.current_node.Type == Group_alternative {
					p.current_node = p.current_node.Parent
				}

				if !(p.current_node.Type == Usage_optional_group || p.current_node.Parent.Type == Usage_optional_group) {
					err = fmt.Errorf("%s: closing bracket unexpected, invalid Token: %v", p.current_node.Type, p.current_token)
				}
				break forLoop
			case ")":
				if p.current_node.Type == Group_alternative {
					p.current_node = p.current_node.Parent
				}

				if !(p.current_node.Type == Usage_required_group || p.current_node.Parent.Type == Usage_required_group) {
					err = fmt.Errorf("%s: closing parenthese unexpected, invalid Token: %v", p.current_node.Type, p.current_token)
				}
				break forLoop
			case "=":
				if err = p.Consume_assign(p.next_token); err != nil {
					break forLoop
				}
				// consume ARGUMENT assigned
				p.NextToken()
				continue
			case "...":
				if err = p.Consume_ellipsis(); err != nil {
					break forLoop
				}
				continue
			}

			// unmatched PUNCT
			n = Usage_unmatched_punct

		case IDENT:
			n = Usage_command
		default:
			n = Unmatched_node
		}

		p.current_node.AddNode(n, p.current_token)
	}

	if p.run {
		p.current_node = saved_current_node
		return err
	} else {
		return fmt.Errorf("%s: parser stoped: %s", p.current_node.Type, err)
	}

}

func (p *DocoptParser) Consume_First_Program_Usage() error {
	// assert p.Prog_name == ""
	p.Change_lexer_state("state_First_Program_Usage")
	BLANK := p.all_symbols["BLANK"]
	// p.current_node has been set previously and must be Usage_section
	for p.run {
		p.NextToken()

		if p.current_token.Type == PROG_NAME {
			p.Prog_name = p.current_token.Value
			// update the regex of the lexer with the actul found PROG_NAME value
			// if next_token is also a PROG_NAME (because the regexp also matched it)
			// it must be rejected
			p.s.DynamicRuleUpdate("PROG_NAME", p.Prog_name)

			usage_line := p.current_node.AddNode(Usage_line, nil)
			usage_line.AddNode(Prog_name, p.current_token)
			p.current_node = usage_line
			return nil
		}

		if p.current_token.Type == BLANK {
			continue
		}

		if p.current_token.Type == NEWLINE {
			if p.next_token.Type == NEWLINE {
				// two consecutive NEWLINE
				if p.Prog_name == "" {
					return fmt.Errorf("Consume_First_Program_Usage: PROG_NAME not defined while leaving on 2 consecutive NEWLINE: %v", p.current_token)
				}
				// consume next NEWLINE
				p.NextToken()
				// leave
				return nil
			}

			continue
		}

		return fmt.Errorf("Consume_First_Program_Usage: expecting PROG_NAME, got: %s", p.symbols_name[p.current_token.Type])
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

// This are section like part of the definition not yet used
// This basically allow more comment, but node are added to the ast
func (p *DocoptParser) Consume_Free_section() error {
	if p.s.Current_state.State_name != "state_Free" {
		// entering Free_section after: Usage_section or Options_section
		p.Change_lexer_state("state_Free")
		p.current_node = p.ast.AddNode(Free_section, nil)

	} else {
		// nested free section: we matched another SECTION token inside a Free_section
		p.current_node = p.ast.AddNode(Free_section, nil)
	}

	if p.current_token.Type == SECTION {
		p.current_node.AddNode(Section_name, p.current_token)
	}

	for p.run {
		p.NextToken()

		if p.current_token.Type == lexer.EOF {
			return nil
		}

		// leaving condition
		if p.current_token.Type == SECTION {
			if strings.EqualFold(p.current_token.Value, "options:") {
				return nil
			}

			if strings.EqualFold(p.current_token.Value, "usage:") {
				return fmt.Errorf("%s: Usage: token found outside Usage_section: %v",
					p.current_node.Type,
					p.current_token)
			}

			// test if the current section has already some content (was empty unamed section)
			nb := len(p.current_node.Children)
			if nb == 0 {
				p.current_node.AddNode(Section_name, p.current_token)
				continue
			}

			// nested Free_section
			// Free_section leaving condition are: EOF or SECTION == Options: or error
			return p.Consume_Free_section()
		}

		p.current_node.AddNode(Section_node, p.current_token)
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

func (p *DocoptParser) Change_lexer_state(new_state string) error {
	p.lexer_state_changed = true
	return p.s.ChangeState(new_state)
}

func (p *DocoptParser) Consume_Options() error {
	section_node := p.ast.AddNode(Options_section, nil)
	p.options_node = section_node
	// only start parsing Options if we start on a token SECTION == Options:
	if p.current_token.Type != SECTION || !strings.EqualFold(p.current_token.Value, "options:") {
		return nil
	}

	p.Change_lexer_state("state_Options")
	section_node.AddNode(Section_name, p.current_token)
	p.current_node = section_node

	var n DocoptNodeType
	var err error

	for p.run {
		p.NextToken()

		if p.current_token.Type == lexer.EOF {
			return nil
		}

		if p.current_token.Type == NEWLINE {
			if p.next_token.Type == NEWLINE {
				// two consecutive NEWLINE
				// consume next NEWLINE
				p.NextToken()
				// leave Usage parsing of Consume_Options
				return nil
			}

			// else: single NEWLINE
		}

		n = Options_node

		switch p.current_token.Type {
		case SECTION:
			return nil
		case LONG_BLANK:
			if p.next_token.Type == SHORT || p.next_token.Type == LONG {
				if err = p.Consume_option_line(); err != nil {
					return err
				}
			}
			continue
		case NEWLINE:
			continue
		}

		// unmatch Options_node
		p.current_node.AddNode(n, p.current_token)
	}

	return fmt.Errorf("%s: parser stoped: %s", p.current_node.Type, err)
}

// consume the next token which must be ARGUMENT as the argument of the last
// node added.
func (p *DocoptParser) Consume_assign(argument *lexer.Token) error {
	if argument.Type != ARGUMENT {
		return fmt.Errorf("%s: Consume_assign must be followed by ARGUMENT, invalid token: %v",
			p.current_node.Type, argument)
	}

	nb_children := len(p.current_node.Children)
	if nb_children == 0 {
		// Consume_assign must called after having assigned a option LONG in Usage_Expr
		// or any option in Options_line called with oe without equal sign
		return fmt.Errorf("Consume_assign: current_node must have an option child, invalid Token: %v", p.current_token)
	}

	prev_child := p.current_node.Children[nb_children-1]
	var node_type DocoptNodeType
	switch prev_child.Type {
	// only those kind of node can have assignment with ARGUMENT
	case Usage_long_option:
		node_type = Usage_argument
	case Option_long, Option_short:
		node_type = Option_argument
	default:
		return fmt.Errorf("Consume_assign: node %s cannot have assignment '=', invalid Token: %v",
			prev_child.Type, p.current_token)
	}

	prev_child.AddNode(node_type, argument)
	return nil
}

func (p *DocoptParser) Consume_option_alternative() error {
	// create the parent node on first call
	if p.current_node.Type != Option_alternative_group {
		nb := len(p.current_node.Children)
		if nb == 0 {
			return fmt.Errorf("%s: comma unexpected without alternative option name, invalid Token: %v", p.current_node.Type, p.current_token)
		}

		p.current_node = p.current_node.Replace_children_with_group(Option_alternative_group)
	}

	// eat next option alternative
	for p.run {
		p.NextToken()
		switch p.current_token.Type {
		case lexer.EOF, LONG_BLANK, NEWLINE:
			if len(p.current_node.Children) <= 1 {
				return fmt.Errorf("%s: %s unexpected without matchin alternative option name, invalid Token: %v",
					p.current_node.Type, p.symbols_name[p.current_token.Type], p.current_token)
			}
			// leaving condition OK
			return nil
		case SHORT:
			p.current_node.AddNode(Option_short, p.current_token)
		case LONG:
			p.current_node.AddNode(Option_long, p.current_token)
		case PUNCT:
			switch p.current_token.Value {
			case ",":
				continue
			case "=":
				if err := p.Consume_assign(p.next_token); err != nil {
					return err
				}
				// consume ARGUMENT assigned
				p.NextToken()
				continue
			default:
				return fmt.Errorf("%s: unexpected PUNC, invalid Token: %v", p.current_node.Type, p.current_token)
			}
		}
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

func (p *DocoptParser) Consume_option_line() error {
	// we did look a head on token: p.current_token is an option LONG or SHORT
	// the option argument will be consumed during the first loop
	saved_node := p.current_node
	option_line := p.current_node.AddNode(Option_line, nil)
	p.current_node = option_line
	var err error = nil
forLoopOptionLine:
	for p.run {
		p.NextToken()

		switch p.current_token.Type {
		case lexer.EOF, NEWLINE:
			// leaving condition option without description
			if len(p.current_node.Children) == 0 {
				err = fmt.Errorf("%s: %s unexpected empty option, invalid Token: %s",
					p.current_node.Type, p.symbols_name[p.current_token.Type], p.current_token)
			}
			break forLoopOptionLine
		case LONG_BLANK:
			// LONG_BLANK in Consume_option_line occurs after options are comsumed
			// leaving condition of Consume_Usage_line
			err = p.Consume_option_description()
			break forLoopOptionLine
		case SHORT:
			p.current_node.AddNode(Option_short, p.current_token)
		case LONG:
			p.current_node.AddNode(Option_long, p.current_token)
		case ARGUMENT:
			if err = p.Consume_assign(p.current_token); err != nil {
				break forLoopOptionLine
			}
		case PUNCT:
			switch p.current_token.Value {
			case ",":
				continue
			case "=":
				if err := p.Consume_assign(p.next_token); err != nil {
					break forLoopOptionLine
				}
				// consume ARGUMENT assigned
				p.NextToken()
			default:
				err = fmt.Errorf("%s: unexpected PUNC, invalid Token: %v", p.current_node.Type, p.current_token)
				break forLoopOptionLine
			}
		default:
			err = fmt.Errorf("%s: Consume_option_line invalid Token: %v", p.current_node.Type, p.current_token)
			break forLoopOptionLine
		}
	} // end forLoopOptionLine

	if p.run {
		p.current_node = saved_node
		return err
	} else {
		return fmt.Errorf("%s: parser stoped", p.current_node.Type)
	}
}

// option description occurs after option has been parsed and can continue on multiple line
// indented by LONG_BLANK. The description is terminated when a new option SHORT or LONG
// is matched at the beginning of the line: NEWLINE LONG_BLANK (SHORT | LONG)
//
//                            Start consume description here
//                                |
// Options:                       v
//   -h <msg>, --help=<msg>        The help message in docopt format.
//                                 Without argument outputs this help.
//                                 If - is given, read the help message from
//                                 standard input.
//                                 If no argument is given, print docopts's own
//                                 help message and quit.
// => LONG_BLANK + option ==> leaving
func (p *DocoptParser) Consume_option_description() error {
	description := p.current_node.AddNode(Option_description, nil)
	current_line := 0

	for p.run {
		p.NextToken()

		switch p.current_token.Type {
		case NEWLINE:
			current_line++
			if p.next_token.Type == NEWLINE {
				// two consecutive NEWLINE
				description.AddNode(Description_node, p.current_token)

				// consume next NEWLINE
				p.NextToken()
				// leave Consume_option_description
				return nil
			}
			// else: single NEWLINE will be consumed as part of the description

		// all the following are leaving condition, other token will be collected as part of the description
		case lexer.EOF:
			return nil
		case LONG_BLANK:
			if current_line > 0 && (p.next_token.Type == SHORT || p.next_token.Type == LONG) {
				// LONG_BLANK need to be re extracted for starting the next Options_line
				p.reject_current_token()
				return nil
			}
			// LONG_BLANK inside description
		case DEFAULT:
			return p.Consume_option_default()
		}

		description.AddNode(Description_node, p.current_token)
	}

	return fmt.Errorf("%s: parser stoped", p.current_node.Type)
}

func (p *DocoptParser) Consume_option_default() error {
	return nil
}

func (p *DocoptParser) ensure_node(node_type DocoptNodeType) {
	if p.current_node.Type != node_type {
		p.current_node = p.current_node.AddNode(node_type, nil)
	}
}
