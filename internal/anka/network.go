package anka

import (
	"context"
	"strings"
)

// NetworkFilterRules returns embedded IP filter rules for vm, or empty if none.
func (c *Client) NetworkFilterRules(ctx context.Context, vm string) (string, error) {
	out, err := c.capture(ctx, "show", vm, "network", "-f")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no network filters") {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfigValue reads a single anka config setting.
func (c *Client) ConfigValue(ctx context.Context, key string) (string, error) {
	out, err := c.capture(ctx, "config", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
