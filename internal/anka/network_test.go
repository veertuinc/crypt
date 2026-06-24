package anka

import (
	"context"
	"testing"
)

func TestNetworkFilterRulesMissing(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = show ] && [ "$2" = clone-1 ] && [ "$3" = network ] && [ "$4" = -f ]; then
  echo "no network filters assigned" >&2
  exit 1
fi
echo "unexpected args: $@" >&2
exit 1
`)

	rules, err := c.NetworkFilterRules(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("NetworkFilterRules() error: %v", err)
	}
	if rules != "" {
		t.Fatalf("NetworkFilterRules() = %q, want empty", rules)
	}
}

func TestNetworkFilterRulesPresent(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = show ] && [ "$2" = clone-1 ] && [ "$3" = network ] && [ "$4" = -f ]; then
  printf 'block local\nblock any\n'
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	rules, err := c.NetworkFilterRules(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("NetworkFilterRules() error: %v", err)
	}
	if rules != "block local\nblock any" {
		t.Fatalf("NetworkFilterRules() = %q", rules)
	}
}

func TestConfigValue(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = config ] && [ "$2" = net_filter ]; then
  printf '/etc/anka/filter.rules\n'
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	got, err := c.ConfigValue(context.Background(), "net_filter")
	if err != nil {
		t.Fatalf("ConfigValue() error: %v", err)
	}
	if got != "/etc/anka/filter.rules" {
		t.Fatalf("ConfigValue() = %q, want /etc/anka/filter.rules", got)
	}
}
