package cli

import (
	"os"
	"strings"
)

type flagSet struct {
	bools  map[string]bool
	values map[string]bool
}

var cryptRootFlags = flagSet{
	bools: map[string]bool{
		"help":    true,
		"version": true,
	},
	values: map[string]bool{
		"name": true,
		"vm":   true,
	},
}

var cryptRunFlags = flagSet{
	bools: map[string]bool{
		"mount":    true,
		"no-local": true,
		"destroy":  true,
	},
	values: map[string]bool{
		"cpu":    true,
		"memory": true,
	},
}

// passthroughAgentArgs returns agent arguments from the original command line,
// skipping Crypt's own flags. Cobra consumes unrecognized flags during parse,
// so positional recovery from os.Args is required for flags like Grok's
// --reasoning-effort.
func passthroughAgentArgs(subcommand string) []string {
	return passthroughAgentArgsFrom(subcommand, os.Args)
}

func passthroughAgentArgsFrom(subcommand string, argv []string) []string {
	args := append([]string(nil), argv...)
	if len(args) == 0 {
		return nil
	}
	if args[0] != subcommand && args[0] != "crypt" {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "crypt" {
		args = args[1:]
	}

	args = cryptRootFlags.skip(args)

	for len(args) > 0 && args[0] != subcommand {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil
	}
	args = args[1:]
	args = cryptRunFlags.skip(args)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return args
}

func (s flagSet) skip(args []string) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(rest, args[i:]...)
		}

		name, isFlag := flagName(arg)
		if !isFlag {
			rest = append(rest, arg)
			continue
		}
		if s.bools[name] {
			continue
		}
		if s.values[name] {
			if !strings.Contains(arg, "=") {
				i++
			}
			continue
		}
		rest = append(rest, arg)
	}
	return rest
}

func flagName(arg string) (string, bool) {
	switch {
	case arg == "-h":
		return "help", true
	case strings.HasPrefix(arg, "--"):
		name, _, _ := strings.Cut(arg[2:], "=")
		return name, true
	case strings.HasPrefix(arg, "-") && len(arg) == 2:
		return arg[1:], true
	default:
		return "", false
	}
}
