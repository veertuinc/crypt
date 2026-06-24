package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/veertuinc/crypt/internal/anka"
)

func warnIfIPFilterBlocksSSH(ctx context.Context, client *anka.Client, vm string, suppress bool) {
	if suppress {
		return
	}

	vmRules, err := client.NetworkFilterRules(ctx, vm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crypt: warning: could not read IP filter rules for %s: %v\n", vm, err)
		return
	}

	globalFilter := ""
	if vmRules == "" {
		globalFilter, err = client.ConfigValue(ctx, "net_filter")
		if err != nil {
			fmt.Fprintf(os.Stderr, "crypt: warning: could not read anka config net_filter: %v\n", err)
			return
		}
	}

	rules := vmRules
	source := "VM"
	if rules == "" && globalFilter != "" {
		rules = globalFilter
		source = "host net_filter"
	}
	if rules == "" {
		return
	}

	fmt.Fprintf(os.Stderr,
		"crypt: warning: IP filtering rules are enabled (%s); do not block inbound TCP port 22 from the host or Crypt SSH will fail\n",
		source,
	)
	if filterRulesMayBlockHostSSH(rules) {
		fmt.Fprintln(os.Stderr, "crypt: warning: current rules may block host-to-VM SSH (port 22); review `anka show "+vm+" network -f`")
	}
}

func filterRulesMayBlockHostSSH(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" || strings.HasPrefix(line, "pass ") {
			continue
		}
		if strings.Contains(line, "block local") {
			return true
		}
		if strings.Contains(line, "block any") {
			return true
		}
		if strings.Contains(line, "block in") && strings.Contains(line, "port 22") {
			return true
		}
	}
	return false
}
