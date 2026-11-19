package main

import (
	"os"
	"strings"
)

// Read only this flag early so policy defaults participate in the normal
// file < environment < explicit flags precedence. flag.Parse still validates all args.
func deploymentConfigPath(args []string) string {
	path := os.Getenv("UMA_CONFIG")
	for i, arg := range args {
		if arg == "--config" || arg == "-config" {
			if i+1 < len(args) {
				path = args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-config=") {
			path = strings.SplitN(arg, "=", 2)[1]
		}
	}
	return path
}
