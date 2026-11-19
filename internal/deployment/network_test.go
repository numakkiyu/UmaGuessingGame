package deployment

import (
	"strings"
	"testing"
)

func TestParseTrustedProxyCIDRsRejectsUnsafeEntries(t *testing.T) {
	for _, test := range []struct {
		name  string
		value []string
	}{
		{name: "universal IPv4", value: []string{"0.0.0.0/0"}},
		{name: "universal IPv6", value: []string{"::/0"}},
		{name: "IPv4-mapped net", value: []string{"::ffff:10.0.0.0/104"}},
		{name: "plain address without prefix", value: []string{"10.0.0.1"}},
		{name: "domain", value: []string{"proxy.internal"}},
		{name: "host with port", value: []string{"10.0.0.1:443"}},
		{name: "empty item", value: []string{"10.0.0.0/8", " "}},
		{name: "too many entries", value: func() []string {
			out := make([]string, 65)
			for i := range out {
				out[i] = "10.0.0.0/8"
			}
			return out
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseTrustedProxyCIDRs(test.value); err == nil {
				t.Fatalf("%v was accepted", test.value)
			}
		})
	}
}

func TestParseTrustedProxyCIDRsNormalisesAndDedupes(t *testing.T) {
	got, err := ParseTrustedProxyCIDRs([]string{" 10.0.0.5/8 ", "10.0.0.0/8", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "10.0.0.0/8" {
		t.Fatalf("got %v, want a single masked 10.0.0.0/8", got)
	}
	if _, err := ParseTrustedProxyCIDRs(nil); err != nil {
		t.Fatalf("empty list should be allowed for env/flag defaults: %v", err)
	}
	if strings.Contains(got[0].String(), " ") {
		t.Fatal("whitespace leaked into the prefix")
	}
}
