package main

import "testing"

func TestConfigPathFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("UMA_CONFIG", "env.json")
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "env.json"}, {[]string{"--config", "lan.json"}, "lan.json"}, {[]string{"-config=cloud.json"}, "cloud.json"}, {[]string{"--config=one.json", "--config", "two.json"}, "two.json"},
	} {
		if got := deploymentConfigPath(test.args); got != test.want {
			t.Fatalf("%v: got %s want %s", test.args, got, test.want)
		}
	}
}
