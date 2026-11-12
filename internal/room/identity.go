package room

import (
	"fmt"
	"golang.org/x/text/unicode/norm"
	"strings"
)

func nameKey(name string) string { return norm.NFC.String(strings.TrimSpace(name)) }
func numberedName(name string, number int64) string {
	if number <= 0 {
		return name
	}
	return fmt.Sprintf("%s · %03d", strings.TrimSpace(name), number)
}
