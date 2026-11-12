package main

import (
	"errors"
	"os"
	"strings"
)

// trustProxyFromEnv overlays UMA_TRUST_PROXY on the deployment JSON value.
//
// Precedence is file < environment < flag: the resolved value becomes the flag
// default, so an explicit flag still wins. A blank variable counts as "not
// provided" so Compose pass-through (UMA_TRUST_PROXY: "${UMA_TRUST_PROXY:-}")
// cannot silently disable a policy file that enables proxy trust.
func trustProxyFromEnv(fileValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv("UMA_TRUST_PROXY"))
	if value == "" {
		return fileValue, nil
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("UMA_TRUST_PROXY must be a boolean")
	}
}

// trustedProxyCIDRsFromEnv overlays UMA_TRUSTED_PROXY_CIDRS on the JSON list.
// Blank means "not provided", not "erase the file value": clearing the list
// while trust is enabled must fail startup instead of happening implicitly.
func trustedProxyCIDRsFromEnv(fileValues []string) string {
	if value := strings.TrimSpace(os.Getenv("UMA_TRUSTED_PROXY_CIDRS")); value != "" {
		return value
	}
	return strings.Join(fileValues, ",")
}
