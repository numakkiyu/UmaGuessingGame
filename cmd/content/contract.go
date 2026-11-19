package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
)

// cmdContract checks local references, operation IDs, write headers and mode discriminators.
// It validates project invariants, not the full JSON Schema meta-schema.
func cmdContract(args []string) error {
	fs := flag.NewFlagSet("contract", flag.ContinueOnError)
	spec := fs.String("spec", "api/openapi.json", "path to the OpenAPI document")
	quiet := fs.Bool("quiet", false, "only report problems")
	checkRoutes := fs.Bool("routes", true, "also compare the document against the router's real route table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *spec == "" {
		return contractErrNoSpec
	}
	if err := initComponentParams(*spec); err != nil {
		return fmt.Errorf("load component parameters: %w", err)
	}
	report, err := validateContract(*spec)
	if err != nil {
		return err
	}
	// Compare both directions to detect missing and unimplemented operations
	if *checkRoutes {
		if err := compareServedRoutes(*spec, report); err != nil {
			return err
		}
	}
	if report.HasProblems() {
		for _, p := range report.Problems {
			fmt.Fprintf(os.Stderr, "contract: %s\n", p)
		}
		return fmt.Errorf("contract: %d problem(s) in %s", len(report.Problems), *spec)
	}
	if !*quiet {
		fmt.Printf("contract OK: %s\n", *spec)
		fmt.Printf("  openapi        %s\n", report.Version)
		fmt.Printf("  paths          %d\n", report.Paths)
		fmt.Printf("  operations     %d (operationId unique)\n", report.Operations)
		fmt.Printf("  schemas        %d\n", report.Schemas)
		fmt.Printf("  $refs checked  %d\n", report.Refs)
		fmt.Printf("  discriminators %d (all mappings resolve)\n", report.Discriminators)
		fmt.Printf("  write ops      %d (CSRF + Idempotency-Key declared)\n", report.WriteOps)
		if report.ServedRoutes > 0 {
			fmt.Printf("  routes         %d served, %d documented (match)\n",
				report.ServedRoutes, report.DocumentedRoutes)
		}
	}
	return nil
}

// contractReport is the outcome of a validation run.
type contractReport struct {
	Version        string
	Paths          int
	Operations     int
	Schemas        int
	Refs           int
	Discriminators int
	WriteOps       int
	// ServedRoutes is how many "METHOD /path" entries the router actually serves.
	ServedRoutes int
	// DocumentedRoutes is how many operation+path pairs the document declares.
	DocumentedRoutes int
	Problems         []string
}

// HasProblems reports whether any invariant failed.
func (r *contractReport) HasProblems() bool { return len(r.Problems) > 0 }

// problem records one violation with its JSON pointer-ish location.
func (r *contractReport) problem(format string, args ...any) {
	r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
}

// httpMethods are the operations the contract may declare on a path item.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// validateContract loads and checks the document.
func validateContract(specPath string) (*contractReport, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}
	var doc map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	rep := &contractReport{}

	// ---- version ----
	if v, ok := doc["openapi"].(string); ok && v != "" {
		rep.Version = v
	} else {
		rep.problem("missing or empty 'openapi' version string")
	}

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	rep.Schemas = len(schemas)

	// ---- $ref resolution ----
	// Walk the whole document once and resolve every local reference. A dangling
	// $ref is the failure mode that silently produces an empty generated type.
	refs := 0
	var walk func(node any, where string)
	walk = func(node any, where string) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				refs++
				if !strings.HasPrefix(ref, "#/") {
					rep.problem("%s: non-local $ref %q (external refs are not supported)", where, ref)
				} else if !resolvePointer(doc, ref) {
					rep.problem("%s: dangling $ref %q", where, ref)
				}
			}
			for k, child := range v {
				walk(child, where+"/"+k)
			}
		case []any:
			for i, child := range v {
				walk(child, fmt.Sprintf("%s/%d", where, i))
			}
		}
	}
	walk(doc, "#")
	rep.Refs = refs

	// ---- paths, operations, path parameters ----
	paths, _ := doc["paths"].(map[string]any)
	rep.Paths = len(paths)
	seenOps := map[string]string{}

	// Sort path keys so the report is deterministic.
	pathKeys := make([]string, 0, len(paths))
	for k := range paths {
		pathKeys = append(pathKeys, k)
	}
	sort.Strings(pathKeys)

	for _, p := range pathKeys {
		item, _ := paths[p].(map[string]any)
		if item == nil {
			rep.problem("paths[%q] is not an object", p)
			continue
		}

		// Every template placeholder must be declared as a path parameter, either
		// on the path item or on each operation.
		placeholders := templatePlaceholders(p)
		pathLevelParams := paramNames(item["parameters"])

		opKeys := make([]string, 0, len(item))
		for m := range item {
			if httpMethods[strings.ToLower(m)] {
				opKeys = append(opKeys, m)
			}
		}
		sort.Strings(opKeys)
		if len(opKeys) == 0 {
			rep.problem("paths[%q] declares no HTTP operation", p)
		}

		for _, m := range opKeys {
			op, _ := item[m].(map[string]any)
			if op == nil {
				continue
			}
			rep.Operations++
			where := fmt.Sprintf("paths[%q].%s", p, strings.ToLower(m))

			// operationId must be present and unique: clients and the drift check
			// both key on it.
			opID, _ := op["operationId"].(string)
			if opID == "" {
				rep.problem("%s: missing operationId", where)
			} else if prev, dup := seenOps[opID]; dup {
				rep.problem("%s: duplicate operationId %q (also at %s)", where, opID, prev)
			} else {
				seenOps[opID] = where
			}

			// Path parameters.
			declared := map[string]bool{}
			for k := range pathLevelParams {
				declared[k] = true
			}
			for k := range paramNames(op["parameters"]) {
				declared[k] = true
			}
			for _, ph := range placeholders {
				if !declared[ph] {
					rep.problem("%s: path placeholder {%s} has no declared path parameter", where, ph)
				}
			}

			// Mutating operations must declare the headers the server enforces. A
			// write operation missing Idempotency-Key would be documented as safe to
			// retry when it is not .
			method := strings.ToLower(m)
			if method == "post" || method == "patch" || method == "put" || method == "delete" {
				hasCSRF, hasIdem := headerParams(op, item)
				// Session creation, single-use grant redemption and invitation
				// resolution are the documented exceptions .
				if opID == "renewAccount" || opID == "deleteAccountSession" {
					hasAccountCSRF := false
					parameters, _ := op["parameters"].([]any)
					for _, v := range parameters {
						p, _ := v.(map[string]any)
						if p["in"] == "header" && p["name"] == "X-Account-CSRF" && p["required"] == true {
							hasAccountCSRF = true
						}
					}
					if !hasAccountCSRF {
						rep.problem("%s: account write must declare X-Account-CSRF", where)
					}
				} else if !opIsUnsafeWithoutCSRF(opID) && opID != "rotateSessionCsrf" {
					if !hasCSRF {
						rep.problem("%s (%s): mutating operation does not declare X-CSRF-Token", where, opID)
					}
					if !hasIdem && requiresIdempotency(opID) {
						rep.problem("%s (%s): mutating operation does not declare Idempotency-Key", where, opID)
					}
				}
				rep.WriteOps++
			}
		}
	}

	// ---- discriminators ----
	if schemas != nil {
		disc := 0
		schemaNames := make([]string, 0, len(schemas))
		for k := range schemas {
			schemaNames = append(schemaNames, k)
		}
		sort.Strings(schemaNames)
		for _, name := range schemaNames {
			sch, _ := schemas[name].(map[string]any)
			if sch == nil {
				continue
			}
			d, ok := sch["discriminator"].(map[string]any)
			if !ok {
				continue
			}
			disc++
			prop, _ := d["propertyName"].(string)
			if prop == "" {
				rep.problem("schemas[%q].discriminator: missing propertyName", name)
			}
			mapping, _ := d["mapping"].(map[string]any)
			if len(mapping) == 0 {
				rep.problem("schemas[%q].discriminator: empty mapping", name)
			}
			keys := make([]string, 0, len(mapping))
			for k := range mapping {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				target, _ := mapping[k].(string)
				if !strings.HasPrefix(target, "#/") || !resolvePointer(doc, target) {
					rep.problem("schemas[%q].discriminator.mapping[%q] -> %q does not resolve",
						name, k, target)
				}
			}
		}
		rep.Discriminators = disc
	}

	// ---- ModeAction coverage ----
	// The mode action discriminator is the one place where a silent mismatch makes
	// every gameplay submission fail, so it is checked explicitly.
	if err := checkModeActionCoverage(schemas, rep); err != nil {
		return nil, err
	}

	return rep, nil
}

// resolvePointer reports whether a "#/a/b/c" reference resolves in doc.
func resolvePointer(doc map[string]any, ref string) bool {
	if !strings.HasPrefix(ref, "#/") {
		return false
	}
	cur := any(doc)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		next, ok := m[part]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

// templatePlaceholders extracts {name} placeholders from a path template.
func templatePlaceholders(path string) []string {
	var out []string
	for {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			return out
		}
		closing := strings.IndexByte(path[open:], '}')
		if closing < 0 {
			return out
		}
		out = append(out, path[open+1:open+closing])
		path = path[open+closing+1:]
	}
}

// paramNames collects the "name" of each declared parameter.
//
// A parameter may be inline or a $ref; a $ref is followed one level, which is all
// the contract uses.
func paramNames(raw any) map[string]bool {
	out := map[string]bool{}
	list, _ := raw.([]any)
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		if m == nil {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out[name] = true
			continue
		}
		// A $ref to a component parameter: resolve the name from the ref tail.
		if ref, ok := m["$ref"].(string); ok {
			if tail := ref[strings.LastIndex(ref, "/")+1:]; tail != "" {
				// The component parameter's own "name" is what matters; the mapping
				// below keeps the two in sync without loading the document twice.
				if n, ok := componentParamName(ref); ok {
					out[n] = true
				} else {
					out[tail] = true
				}
			}
		}
	}
	return out
}

// componentParamName is filled in by validateContract before path checks run.
var componentParamName func(ref string) (string, bool)

// headerParams reports whether the operation declares the CSRF and idempotency
// headers, either inline or through a component parameter reference.
func headerParams(op, item map[string]any) (csrf, idem bool) {
	check := func(raw any) {
		list, _ := raw.([]any)
		for _, entry := range list {
			m, _ := entry.(map[string]any)
			if m == nil {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				if ref, ok := m["$ref"].(string); ok {
					if n, ok := componentParamName(ref); ok {
						name = n
					}
				}
			}
			switch name {
			case "X-CSRF-Token":
				csrf = true
			case "Idempotency-Key":
				idem = true
			}
		}
	}
	check(op["parameters"])
	check(item["parameters"])
	return csrf, idem
}

// opIsUnsafeWithoutCSRF lists the operations the contract explicitly exempts from
// the cookie-write CSRF requirement : initial session creation, single-use
// grant redemption, and invitation resolution.
//
// rotateSessionCsrf is exempt for a different but equally sound reason: its entire
// purpose is to hand out a fresh CSRF token to a session that has one stored only as
// a hash. Requiring the old token would make the endpoint unusable in exactly the
// situation it exists for (a reloaded page that no longer has it). The server marks
// this route noCsrf: true, and the contract omits both headers to match.
func opIsUnsafeWithoutCSRF(opID string) bool {
	switch opID {
	case "createSession", "redeemDisplayGrant", "redeemRecoveryGrant", "resolveRoomInvitation", "startAccountLogin":
		return true
	}
	return false
}

// requiresIdempotency reports whether the contract requires an Idempotency-Key for
// this operation.
//
// Exempt are the one-shot / first-contact operations where a key cannot exist yet,
// plus rotateSessionCsrf, which is deliberately NOT idempotent: each call returns a
// new token, so replaying it with a stored receipt would hand back a stale token.
func requiresIdempotency(opID string) bool {
	// Admission consumes a provider token and issues a bounded cookie. It does
	// not allocate a game resource; an existing valid cookie restores success.
	// Persisting a generic receipt would replay an expired grant after restart.
	// Matchmaking challenge replacement and presence renewal are bounded leases,
	// not game commands; replaying a receipt would not renew their validity.
	if opID == "rotateSessionCsrf" || opID == "verifyAdmission" || opID == "createMatchmakingProof" || opID == "heartbeatMatchmakingTicket" {
		return false
	}
	return !opIsUnsafeWithoutCSRF(opID)
}

// checkModeActionCoverage verifies the ModeAction discriminator is populated and
// that every mapping target is a real schema declaring the same const kind.
//
// This is the check that would have caught the "contract says classic.guess, the
// mode only accepts guess" defect at contract level rather than at runtime.
func checkModeActionCoverage(schemas map[string]any, rep *contractReport) error {
	if schemas == nil {
		rep.problem("no components.schemas: cannot check ModeAction coverage")
		return nil
	}
	ma, _ := schemas["ModeAction"].(map[string]any)
	if ma == nil {
		rep.problem("components.schemas.ModeAction is missing")
		return nil
	}
	d, _ := ma["discriminator"].(map[string]any)
	mapping, _ := d["mapping"].(map[string]any)
	if len(mapping) == 0 {
		rep.problem("ModeAction.discriminator.mapping is empty: no action kind is declared")
		return nil
	}
	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, kind := range keys {
		ref, _ := mapping[kind].(string)
		target := schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
		sch, _ := target.(map[string]any)
		if sch == nil {
			// resolvePointer already reported it.
			continue
		}
		props, _ := sch["properties"].(map[string]any)
		kindProp, _ := props["kind"].(map[string]any)
		if kindProp == nil {
			rep.problem("ModeAction %q -> schema has no 'kind' property", kind)
			continue
		}
		if c, ok := kindProp["const"].(string); !ok || c != kind {
			rep.problem("ModeAction mapping key %q does not match the schema's kind const %v", kind, kindProp["const"])
		}
	}
	return nil
}

// initComponentParams pre-loads component parameter names so headerParams and
// paramNames can resolve $refs without re-reading the file.
func initComponentParams(specPath string) error {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	components, _ := doc["components"].(map[string]any)
	params, _ := components["parameters"].(map[string]any)
	componentParamName = func(ref string) (string, bool) {
		tail := ref[strings.LastIndex(ref, "/")+1:]
		p, ok := params[tail].(map[string]any)
		if !ok {
			return "", false
		}
		name, ok := p["name"].(string)
		return name, ok && name != ""
	}
	_ = filepath.Separator
	return nil
}

// contractErrNoSpec is returned when the spec path is empty.
var contractErrNoSpec = errors.New("contract: empty --spec path")

// compareServedRoutes checks route parity in both directions.
// Document paths are resolved against servers[0].url before comparison.
func compareServedRoutes(specPath string, rep *contractReport) error {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	prefix := ""
	if len(doc.Servers) > 0 {
		prefix = strings.TrimRight(doc.Servers[0].URL, "/")
	}

	// documented: "METHOD /full/path"
	//
	// An operation may override the document-level servers list, and the health
	// endpoints do exactly that: they declare servers:[{url:"/"}] because they live
	// at the site root rather than under /api/v1. Honouring the override is required
	// by the OpenAPI spec and, more practically, without it the comparison would
	// report every root-level endpoint as both undocumented and unserved.
	documented := map[string]bool{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			m := strings.ToLower(method)
			if !httpMethods[m] {
				continue
			}
			opPrefix := prefix
			if len(raw) > 0 {
				var op struct {
					Servers []struct {
						URL string `json:"url"`
					} `json:"servers"`
				}
				if err := json.Unmarshal(raw, &op); err == nil && len(op.Servers) > 0 {
					opPrefix = strings.TrimRight(op.Servers[0].URL, "/")
					// A bare "/" means "no prefix".
					if opPrefix == "/" {
						opPrefix = ""
					}
				}
			}
			documented[strings.ToUpper(m)+" "+opPrefix+path] = true
		}
	}

	served := map[string]bool{}
	for _, entry := range httpapi.ServedOperations() {
		served[entry] = true
	}

	rep.ServedRoutes = len(served)
	rep.DocumentedRoutes = len(documented)

	// A documented operation the server does not serve is a broken promise: a client
	// generated from this document will call it and get 404.
	var missing []string
	for route := range documented {
		if !served[route] {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	for _, route := range missing {
		rep.problem("documented but not served: %s (a client generated from this document would 404)", route)
	}

	// A served operation the document omits is invisible: no types, no validation,
	// and no client. It is just as much a defect, so it fails too.
	var undocumented []string
	for route := range served {
		if !documented[route] {
			undocumented = append(undocumented, route)
		}
	}
	sort.Strings(undocumented)
	for _, route := range undocumented {
		rep.problem("served but not documented: %s (clients and generated types cannot see it)", route)
	}

	return nil
}
