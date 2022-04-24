// vim: set ts=4 sw=4 sts=4 noet:
//
// docopts.go is a command line wrapper for docopt.go to be used by bash scripts.
//
package main

import (
	"fmt"
	// we use our own embedded docopt-go lib version
	"github.com/docopt/docopts/docopt-go"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// vars defined at compile time
// https://github.com/ahmetb/govvv
var (
	Version        string
	BuildDate      string
	GitCommit      string
	GoBuildVersion string
)

var copyleft = `
Copyleft (Ɔ)  2022 Sylvain Viart (golang version).
Copyright (C) 2013 Vladimir Keleshev, Lari Rasku.
License MIT <http://opensource.org/licenses/MIT>.
This is free software: you are free to change and redistribute it.
There is NO WARRANTY, to the extent permitted by law.
`

// Version will be build by main() function
var Docopts_Version string

var Usage string = `Shell interface for docopt, the CLI description language.

Usage:
  docopts [options] [--docopt_sh] -h <msg> : [<argv>...]
  docopts [options] -G <prefix> [--docopt_sh] -h <msg> : [<argv>...]
  docopts [options] --no-mangle  -h <msg> : [<argv>...]
  docopts [options] [--no-declare] -A <name>   -h <msg> : [<argv>...]
  docopts [options] (--print-ast|--print-pat-fix|--print-parsed) -h <msg> : [<argv>...]

Options:
  -h <msg>, --help=<msg>        The help message in docopt format.
                                Without argument outputs this help.
                                If - is given, read the help message from
                                standard input.
                                If no argument is given, print docopts's own
                                help message and quit.
  -V <msg>, --version=<msg>     A version message.
                                If - is given, read the version message from
                                standard input.  If the help message is also
                                read from standard input, it is read first.
                                If no argument is given, print docopts's own
                                version message and quit.
  -s <str>, --separator=<str>   The string to use to separate the help message
                                from the version message when both are given
                                via standard input. [default: ----]
  -O, --options-first           Disallow interspersing options and positional
                                arguments: all arguments starting from the
                                first one that does not begin with a dash will
                                be treated as positional arguments.
  -H, --no-help                 Don't handle --help and --version specially.
  -A <name>                     Export the arguments as a Bash 4+ associative
                                array called <name>.
  -G <prefix>                   Don't use associative array but output
                                Bash 3.2 compatible GLOBAL variables assignment:
                                  <prefix>_{mangled_args}={parsed_value}
                                Can be used with numeric incompatible options
                                as well.  See also: --no-mangle
  --no-mangle                   Output parsed option not suitable for bash eval.
                                Full option names are kept. Rvalue is still
                                shellquoted. Extra parsing is required.
  --docopt_sh                   Output parsed option suitable for bash eval,
                                names are converted to be compatible with docopt.sh
  --no-declare                  Don't output 'declare -A <name>', used only
                                with -A argument.
  --debug                       Output extra parsing information for debugging.
                                Output cannot be used in bash eval.
`

// testing trick, out can be mocked to catch stdout and validate
// https://stackoverflow.com/questions/34462355/how-to-deal-with-the-fmt-golang-library-package-for-cli-testing
var out io.Writer = os.Stdout

// debug helper
func print_args(args docopt.Opts, message string) string {
	fmt.Printf("################## %s ##################\n", message)
	// use max length argument for alignment output
	var max int
	keys_sorted := Sort_args_keys(args, &max)
	format := fmt.Sprintf("%%%ds : %%v\n", max+2)
	for _, key := range keys_sorted {
		fmt.Printf(format, key, args[key])
	}

	// the format is returned so it can be reused
	return format
}

// Sort args keys and return key max length in max (skipped if max is nil)
func Sort_args_keys(args docopt.Opts, max *int) []string {
	keys_list := make([]string, len(args))
	// compute key max length only if max argument is given
	if max != nil {
		*max = 0
	}
	// extract keys from args
	i := 0
	for k, _ := range args {
		keys_list[i] = k
		i++
		if max != nil {
			l := len(k)
			if l > *max {
				*max = l
			}
		}
	}
	sort.Strings(keys_list)
	return keys_list
}

// Key_Generator is an enum which defines how key of parsed option will be outputted
type Key_Generator int

const (
	Mangled Key_Generator = iota + 1
	Verbatim
	Docopt_sh
)

// Store global behavior to avoid passing many optional arguments to methods.
type Docopts struct {
	Global_prefix  string
	Key_output     Key_Generator
	Output_declare bool
	// Exit_function is experimental
	Exit_function bool
}

// output bash 4+ compatible assoc array, suitable for eval.
func (d *Docopts) Print_bash_args(bash_assoc string, args docopt.Opts) {
	// Reuse python's fake nested Bash arrays for repeatable arguments with values.
	// The structure is:
	// bash_assoc[key,#]=length
	// bash_assoc[key,i]=value
	// 'i' is an integer from 0 to length-1
	// length can be 0, for empty array

	if d.Output_declare {
		fmt.Fprintf(out, "declare -A %s\n", bash_assoc)
	}

	for _, key := range Sort_args_keys(args, nil) {
		value := args[key]
		// some golang tricks here using reflection to loop over the map[]
		rt := reflect.TypeOf(value)
		if IsArray(rt) {
			// all array is outputted even 0 size
			val_arr := value.([]string)
			for index, v := range val_arr {
				fmt.Fprintf(out, "%s['%s,%d']=%s\n", bash_assoc, Shellquote(key), index, To_bash(v))
			}
			// size of the array
			fmt.Fprintf(out, "%s['%s,#']=%d\n", bash_assoc, Shellquote(key), len(val_arr))
		} else {
			// value is not an array
			fmt.Fprintf(out, "%s['%s']=%s\n", bash_assoc, Shellquote(key), To_bash(value))
		}
	}
}

// Check if a value is an array
func IsArray(rt reflect.Type) bool {
	if rt == nil {
		return false
	}
	switch rt.Kind() {
	case reflect.Slice:
		return true
	case reflect.Array:
		return true
	default:
		return false
	}
}

func Shellquote(s string) string {
	return strings.Replace(s, "'", `'\''`, -1)
}

func IsBashIdentifier(s string) bool {
	identifier := regexp.MustCompile(`^([A-Za-z]|[A-Za-z_][0-9A-Za-z_]+)$`)
	return identifier.MatchString(s)
}

// Convert a parsed type to a text string suitable for bash eval
// as a right-hand side of an assignment.
// Handles quoting for string, no quote for number or bool.
func To_bash(v interface{}) string {
	var s string
	switch v.(type) {
	case bool:
		s = fmt.Sprintf("%v", v.(bool))
	case int:
		s = fmt.Sprintf("%d", v.(int))
	case string:
		s = fmt.Sprintf("'%s'", Shellquote(v.(string)))
	case []string:
		arr := v.([]string)
		if len(arr) == 0 {
			// bash empty array
			s = "()"
		} else {
			// escape all strings
			arr_out := make([]string, len(arr))
			for i, e := range arr {
				arr_out[i] = Shellquote(e)
			}
			s = fmt.Sprintf("('%s')", strings.Join(arr_out[:], "' '"))
		}
	case nil:
		s = ""
	default:
		panic(fmt.Sprintf("To_bash():unsuported type: %v for '%v'", reflect.TypeOf(v), v))
	}

	return s
}

// Performs output for bash Globals (not bash 4+ assoc) Names are mangled to become
// suitable for bash eval.
// Docopts.Key_output will determine how key are printed
// --no-mangle	: Verbatim (no suitable for eval(1))
// -G			: Mangled
// --docopt_sh	: Docopt_sh
func (d *Docopts) Print_bash_global(args docopt.Opts) error {
	var new_name string
	var err error
	var out_buf string

	varmap := make(map[string]string)

	// docopt.Opts is of type map[string]interface{}
	// so value is an interface{}
	for _, key := range Sort_args_keys(args, nil) {
		switch d.Key_output {
		case Mangled:
			if key == "--" && d.Global_prefix == "" {
				// skip double-dash that can't be mangled #52
				// so double-dash is not printed for bash
				// but still parsed by docopts
				continue
			}

			new_name, err = d.Name_mangle(key)
			if err != nil {
				return err
			}
		case Verbatim:
			new_name = key
		case Docopt_sh:
			new_name, err = d.Replace_with_undercore(key)
			if err != nil {
				return err
			}
			// alter output if we have a prefix
			key_fmt := "%s"
			if d.Global_prefix != "" {
				key_fmt = fmt.Sprintf("%s_%%s", d.Global_prefix)
			}
			new_name = fmt.Sprintf(key_fmt, new_name)

		}

		// test if already present in the map
		prev_key, seen := varmap[new_name]
		if seen {
			return fmt.Errorf("%s: two or more elements have identically mangled names", prev_key)
		} else {
			varmap[new_name] = key
		}

		out_buf += fmt.Sprintf("%s=%s\n", new_name, To_bash(args[key]))
	}

	// final output
	fmt.Fprintf(out, "%s", out_buf)

	return nil
}

// Transform a parsed option or place-holder name into a bash identifier if possible.
// Docopts.Global_prefix will be prepended if given, wrong prefix may produce invalid
// bash identifier and this method will fail too.
func (d *Docopts) Name_mangle(elem string) (string, error) {
	var v string

	if d.Global_prefix == "" && (elem == "-" || elem == "--") {
		return "", fmt.Errorf("Mangling not supported for: '%s'", elem)
	}

	if Match(`^<.*>$`, elem) {
		v = elem[1 : len(elem)-1]
	} else if Match(`^-[^-]$`, elem) {
		v = fmt.Sprintf("%c", elem[1])
	} else if Match(`^--.+$`, elem) {
		v = elem[2:]
	} else {
		// also this case for '-' when d.Global_prefix != ""
		v = elem
	}

	// alter output if we have a prefix
	key_fmt := "%s"
	if d.Global_prefix != "" {
		key_fmt = fmt.Sprintf("%s_%%s", d.Global_prefix)
	}

	v = fmt.Sprintf(key_fmt, strings.Replace(v, "-", "_", -1))

	if !IsBashIdentifier(v) {
		return "", fmt.Errorf("cannot transform into a bash identifier: '%s' => '%s'", elem, v)
	}

	return v, nil
}

// Experimental: issue #36
// Transform a string which may contains character no suitable for bash eval
// to underscores. Compatible with https://github.com/andsens/docopt.sh
func (d *Docopts) Replace_with_undercore(elem string) (string, error) {
	var v string

	if elem == "-" || elem == "--" {
		return "", fmt.Errorf("not supported")
	}

	// from python r'^[^a-z_]|[^a-z0-9_]', '_', name, 0, re.IGNORECASE
	re := regexp.MustCompile(`^[^a-zA-Z_]|[^a-zA-Z0-9_]`)
	v = re.ReplaceAllString(elem, "_")

	return v, nil
}

// helper for lazy typing
func Match(regex string, source string) bool {
	matched, _ := regexp.MatchString(regex, source)
	return matched
}

// Experimental: issue #43
// Change bash exit source code based on '--function' parameter
func (d *Docopts) Get_exit_code(exit_code int) (str_code string) {
	if d.Exit_function {
		str_code = fmt.Sprintf("return %d", exit_code)
	} else {
		str_code = fmt.Sprintf("exit %d", exit_code)
	}
	return
}

// Our HelpHandler which outputs bash source code to be evaled as error and stop or
// display program's help or version.
func (d *Docopts) HelpHandler_for_bash_eval(err error, usage string) {
	if err != nil {
		fmt.Printf("echo 'error: %s\n%s' >&2\n%s\n",
			Shellquote(err.Error()),
			Shellquote(usage),
			d.Get_exit_code(64),
		)
		os.Exit(1)
	} else {
		// --help or --version found and --no-help was not given
		fmt.Printf("echo '%s'\n%s\n", Shellquote(usage), d.Get_exit_code(0))
		os.Exit(0)
	}
}

// HelpHandler for go parser which parses docopts options. See: HelpHandler_for_bash_eval for parsing
// bash options. This handler is called when docopts itself detects a parse error on docopts usage.
// If docopts parsing is OK, then HelpHandler_for_bash_eval will be called by a second parser based on the
// help string given with -h <msg> or --help=<msg>. This behavior is a legacy behavior from docopts python
// previous version. This introduce strange hack in option parsing and may be changed after initial docopts go
// version release.
func HelpHandler_golang(err error, usage string) {
	if err != nil {
		err_str := err.Error()
		// we hack for our polymorphic argument -h or -V
		// it was the same hack in python version
		if len(err_str) >= 9 {
			if err_str[0:2] == "-h" || err_str[0:6] == "--help" {
				// print full usage message (global var)
				fmt.Println(strings.TrimSpace(Usage))
				os.Exit(0)
			}
			if err_str[0:2] == "-V" || err_str[0:9] == "--version" {
				fmt.Println(strings.TrimSpace(Docopts_Version))
				os.Exit(0)
			}
		}

		// When we have an error with err_str empty, this is a special case:
		// we received an usage string which MUST receive an argument and no argument has been
		// given by the user. So this is a valid, from golang point of view but not for bash.
		if len(err_str) == 0 {
			// no arg at all, display small usage, also exits 1
			d := &Docopts{Exit_function: false}
			d.HelpHandler_for_bash_eval(fmt.Errorf("no argument"), usage)
		}

		// real error
		fmt.Fprintf(os.Stderr, "my error: %v, %v\n", err, usage)
		os.Exit(1)
	} else {
		// no error, never reached?
		fmt.Println(usage)
		os.Exit(0)
	}
}

func docopts_error(msg string, err error) {
	if err != nil {
		msg = fmt.Sprintf(msg, err)
	}
	fmt.Fprintf(os.Stderr, "docopts:error: %s\n", msg)
	os.Exit(1)
}

func main() {
	golang_parser := &docopt.Parser{
		OptionsFirst:  true,
		SkipHelpFlags: true,
		HelpHandler:   HelpHandler_golang,
	}

	// build Docopts_Version string
	Docopts_Version = fmt.Sprintf("docopts %s commit %s built at %s\nbuilt from: %s\n%s",
		Version,
		GitCommit,
		BuildDate,
		GoBuildVersion,
		strings.TrimSpace(copyleft))

	arguments, err := golang_parser.ParseArgs(Usage, nil, Docopts_Version)

	if err != nil {
		msg := fmt.Sprintf("mypanic: %v\n", err)
		panic(msg)
	}

	debug := arguments["--debug"].(bool)
	var debug_print_format string
	if debug {
		debug_print_format = print_args(arguments, "golang")
	}

	// create our Docopts struct
	d := &Docopts{
		Global_prefix:  "",
		Key_output:     Mangled,
		Output_declare: true,
		// Exit_function is experimental
		Exit_function: false,
	}

	// parse docopts's own arguments
	argv := arguments["<argv>"].([]string)
	doc := arguments["--help"].(string)
	bash_version, _ := arguments.String("--version")
	options_first := arguments["--options-first"].(bool)
	no_help := arguments["--no-help"].(bool)
	separator := arguments["--separator"].(string)
	if arguments["--no-mangle"].(bool) {
		d.Key_output = Verbatim
	}
	if arguments["--docopt_sh"].(bool) {
		d.Key_output = Docopt_sh
	}
	d.Output_declare = !arguments["--no-declare"].(bool)
	global_prefix, err := arguments.String("-G")
	if err == nil {
		d.Global_prefix = global_prefix
	}

	// read from stdin
	if doc == "-" && bash_version == "-" {
		bytes, _ := ioutil.ReadAll(os.Stdin)
		arr := strings.Split(string(bytes), separator)
		if len(arr) == 2 {
			doc, bash_version = arr[0], arr[1]
		} else {
			msg := "error: help + version stdin, not found"
			if debug {
				msg += fmt.Sprintf("\nseparator is: '%s'\n", separator)
				msg += fmt.Sprintf("spliting has given %d blocs, exactly 2 are expected\n", len(arr))
			}
			panic(msg)
		}
	} else if doc == "-" {
		bytes, _ := ioutil.ReadAll(os.Stdin)
		doc = string(bytes)
	} else if bash_version == "-" {
		bytes, _ := ioutil.ReadAll(os.Stdin)
		bash_version = string(bytes)
	}

	doc = strings.TrimSpace(doc)
	bash_version = strings.TrimSpace(bash_version)
	if debug {
		fmt.Printf(debug_print_format, "doc", doc)
		fmt.Printf(debug_print_format, "bash_version", bash_version)
	}

	// now parses bash program's arguments
	parser := &docopt.Parser{
		HelpHandler:   d.HelpHandler_for_bash_eval,
		OptionsFirst:  options_first,
		SkipHelpFlags: no_help,
	}

	var bash_args docopt.Opts
	if arguments["--print-ast"].(bool) {
		docopt.PRINT_AST = true
		// parser will print its own ast
		bash_args, err = parser.ParseArgs(doc, argv, bash_version)
		return
	} else if arguments["--print-pat-fix"].(bool) {
		docopt.PRINT_PAT_FIX = true
		// parser will print its own ast after fix
		bash_args, err = parser.ParseArgs(doc, argv, bash_version)
		return
	} else if arguments["--print-parsed"].(bool) {
		docopt.PRINT_PARSE_RESULT = true
		// parser will print its own ast after fix
		bash_args, err = parser.ParseArgs(doc, argv, bash_version)
		return
	} else {
		bash_args, err = parser.ParseArgs(doc, argv, bash_version)
	}
	if err == nil {
		if debug {
			print_args(bash_args, "bash")
			fmt.Println("----------------------------------------")
		}
		name, err := arguments.String("-A")
		if err == nil {
			if !IsBashIdentifier(name) {
				fmt.Printf("-A: not a valid Bash identifier: '%s'", name)
				return
			}
			d.Print_bash_args(name, bash_args)
		} else {
			err = d.Print_bash_global(bash_args)
			if err != nil {
				docopts_error("Print_bash_global:%v", err)
			}
		}
	} else {
		panic(err)
	}
}
