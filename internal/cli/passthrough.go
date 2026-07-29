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
		"name":            true,
		"vm":              true,
		"unlock-keychain": true,
	},
}

var cryptRunFlags = flagSet{
	bools: map[string]bool{
		"no-local": true,
		"destroy":  true,
	},
	values: map[string]bool{
		"cpu":             true,
		"memory":          true,
		"mount":           true,
		"env":             true,
		"socket":          true,
		"unlock-keychain": true, // also allowed after the subcommand
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

	// Drop root flags and other tokens only until the agent subcommand.
	// Do not strip root flags after it — agents such as cursor-agent use
	// --name on management subcommands (worker start --name ...).
	args = cryptRootFlags.skipUntil(args, subcommand)
	if len(args) == 0 || args[0] != subcommand {
		return nil
	}
	args = args[1:]
	args = cryptRunFlags.skip(args)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return args
}

// skipUntil removes flags in s from the prefix of args before stop, then
// returns the slice that starts at stop (or nil if stop is missing).
func (s flagSet) skipUntil(args []string, stop string) []string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == stop {
			return args[i:]
		}
		if arg == "--" {
			return nil
		}
		name, isFlag := flagName(arg)
		if !isFlag {
			continue
		}
		if s.values[name] && !strings.Contains(arg, "=") {
			i++
		}
	}
	return nil
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
