package main

import (
	"fmt"
	"strconv"
	//	"github.com/alecthomas/repr"
	"github.com/docopt/docopts/docopt-go"
	"io/ioutil"
	"os"
	"regexp"
)

var Usage string = `explore docopt-go lib function call
Usage:
  explore-docopt call <call_method> <argument_for_method> <filename>
  explore-docopt list_method

Arguments:
  <call_method>           method in docopt-go lib see list_method
  <argument_for_method>   text argument for the method to call, for ParseSection
                          the section string + ending ':' that starts the section
                          bloc.
  <filename>              a valid filename containing the usage to parse, if
                          <filename> is - use stdin.

Call:
  explore-docopt call ParseSection "the section separator + :" <filename>
  explore-docopt call FormalUsage "" <filename>
`

func extract_usage_and_FormalUsage(usage_string string, number_arg string) (string, string) {
	usageSections := docopt.ParseSection("usage:", usage_string)
	var number int = 0
	if len(usageSections) > 1 {
		number, _ = strconv.Atoi(number_arg)
	}
	formal, err := docopt.FormalUsage(usageSections[number])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return usageSections[number], formal
}

func call_method(method_name string, argument_for_method string, usage_string string) {
	fmt.Printf("calling %s...\n", method_name)
	switch method_name {
	case "ParseSection", "parseSection":
		// extract the Usage: section (case-insensitive) until the next empty line
		usageSections := docopt.ParseSection(argument_for_method, usage_string)
		for i, s := range usageSections {
			fmt.Printf("%d: %s\n", i, s)
		}
	case "FormalUsage", "formalUsage":
		usage, formal := extract_usage_and_FormalUsage(usage_string, argument_for_method)
		fmt.Printf("extrated Usage:\n%s\n", usage)
		fmt.Printf("FormalUsage: %s\n", formal)
	case "TokenListFromPattern", "tokenListFromPattern":
		_, formal := extract_usage_and_FormalUsage(usage_string, argument_for_method)
		tokens := docopt.TokenListFromPattern(formal)
		fmt.Println("TokenListFromPattern:")
		for i, t := range tokens.GetTokens() {
			fmt.Printf("%d: '%s'\n", i, t)
		}
	default:
		fmt.Printf("unknown method: %s\n", call_method)
	}
}

func main() {
	arguments, err := docopt.ParseDoc(Usage)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	list_method := arguments["list_method"].(bool)
	available_method := [...]string{"FormalUsage", "ParseSection", "TokenListFromPattern"}
	if list_method {
		fmt.Printf("supported method: %s\n", available_method)
		os.Exit(0)
	}

	// docopt parser ensure that we are in the second choice call
	call := arguments["call"].(bool)
	if !call {
		panic("not call!!")
	}

	method_name := arguments["<call_method>"].(string)
	argument_for_method := arguments["<argument_for_method>"].(string)
	filename := arguments["<filename>"].(string)

	var data []byte
	if filename == "-" {
		data, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("error: fail to open file: %s\n", filename)
			return
		}
		fmt.Printf("parsing from stdin\n")
	} else {
		data, err = os.ReadFile(filename)
		if err != nil {
			fmt.Printf("error: fail to open file: %s\n", filename)
			return
		}
		fmt.Printf("parsing: %s\n", filename)
	}

	doc := string(data)

	// remove comment
	p := regexp.MustCompile(`(^|\n)#[^\n]+\n`)
	doc = p.ReplaceAllString(doc, `$1`)

	call_method(method_name, argument_for_method, doc)
}