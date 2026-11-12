package main

import "testing"

func TestTrustProxyEnvOverridesFileValue(t *testing.T) {
	for _, test := range []struct {
		name    string
		file    bool
		env     *string
		want    bool
		wantErr bool
	}{
		{name: "unset keeps file true", file: true, env: nil, want: true},
		{name: "unset keeps file false", file: false, env: nil, want: false},
		{name: "blank compose pass-through keeps file true", file: true, env: strptr(""), want: true},
		{name: "blank compose pass-through keeps file false", file: false, env: strptr("   "), want: false},
		{name: "env enables over file false", file: false, env: strptr("true"), want: true},
		{name: "env disables over file true", file: true, env: strptr("0"), want: false},
		{name: "invalid env refuses boot", file: true, env: strptr("maybe"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.env != nil {
				t.Setenv("UMA_TRUST_PROXY", *test.env)
			} else {
				t.Setenv("UMA_TRUST_PROXY", "")
			}
			got, err := trustProxyFromEnv(test.file)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", *test.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}

func TestTrustedProxyCIDRsEnvOverridesFileValue(t *testing.T) {
	file := []string{"10.0.0.0/8", "127.0.0.1/32"}
	for _, test := range []struct {
		name string
		env  string
		want string
	}{
		{name: "unset keeps file list", env: "", want: "10.0.0.0/8,127.0.0.1/32"},
		{name: "blank compose pass-through keeps file list", env: "  ", want: "10.0.0.0/8,127.0.0.1/32"},
		{name: "env replaces file list", env: "192.0.2.0/24", want: "192.0.2.0/24"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("UMA_TRUSTED_PROXY_CIDRS", test.env)
			if got := trustedProxyCIDRsFromEnv(file); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func strptr(v string) *string { return &v }
