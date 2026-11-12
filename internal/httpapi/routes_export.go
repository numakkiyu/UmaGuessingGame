package httpapi

// ServedOperations returns METHOD /path entries from the router for contract parity checks.
// Operational endpoints outside the public API are omitted.
func ServedOperations() []string {
	s := &Server{}
	table := s.table()
	out := make([]string, 0, len(table))
	for _, rt := range table {
		out = append(out, rt.method+" "+rt.pattern)
	}
	return out
}
