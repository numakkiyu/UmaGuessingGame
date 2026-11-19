package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoContract is the tracked contract artifact the build and CI read.
const repoContract = "../../api/openapi.json"

// TestValidateRealContract asserts the shipped contract passes every invariant.
//
// This is the guard that keeps the validator honest: if someone loosens a check
// until nothing can fail, this test still exercises the real document and the
// negative tests below still require the checks to fire.
func TestValidateRealContract(t *testing.T) {
	if _, err := os.Stat(repoContract); err != nil {
		t.Fatalf("tracked contract artifact is missing at %s: %v", repoContract, err)
	}
	if err := initComponentParams(repoContract); err != nil {
		t.Fatalf("init component params: %v", err)
	}
	rep, err := validateContract(repoContract)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rep.HasProblems() {
		for _, p := range rep.Problems {
			t.Errorf("problem: %s", p)
		}
		t.Fatal("the tracked contract has problems")
	}
	if rep.Operations < 40 {
		t.Errorf("operations = %d, want at least 40; a drop this large means paths were lost", rep.Operations)
	}
	if rep.Schemas < 100 {
		t.Errorf("schemas = %d, want at least 100", rep.Schemas)
	}
	if rep.Refs < 200 {
		t.Errorf("refs checked = %d, want at least 200", rep.Refs)
	}
	if rep.Discriminators < 4 {
		t.Errorf("discriminators = %d, want at least 4", rep.Discriminators)
	}
}

// writeContract writes a mutated copy of the real contract into a temp file.
func writeContract(t *testing.T, mutate func(doc map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(repoContract)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var doc map[string]any
	if err := jsonUnmarshalForTest(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	mutate(doc)
	out, err := jsonMarshalForTest(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// TestValidatorCatchesRealDefects is the negative table.
//
// Each case injects the class of defect this project has actually shipped or nearly
// shipped, and asserts the validator reports it. A validator that cannot fail is
// worse than none, so this table is the point of the command.
func TestValidatorCatchesRealDefects(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(doc map[string]any)
		wantText string
	}{
		{
			name: "dangling $ref",
			mutate: func(doc map[string]any) {
				schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
				rc := schemas["RoomCreate"].(map[string]any)
				props := rc["properties"].(map[string]any)
				props["name"] = map[string]any{"$ref": "#/components/schemas/DoesNotExist"}
			},
			wantText: "dangling $ref",
		},
		{
			name: "duplicate operationId",
			mutate: func(doc map[string]any) {
				paths := doc["paths"].(map[string]any)
				cfg := paths["/config"].(map[string]any)
				get := cfg["get"].(map[string]any)
				get["operationId"] = "createSession"
			},
			wantText: "duplicate operationId",
		},
		{
			name: "missing operationId",
			mutate: func(doc map[string]any) {
				paths := doc["paths"].(map[string]any)
				cfg := paths["/config"].(map[string]any)
				delete(cfg["get"].(map[string]any), "operationId")
			},
			wantText: "missing operationId",
		},
		{
			name: "undeclared path placeholder",
			mutate: func(doc map[string]any) {
				paths := doc["paths"].(map[string]any)
				paths["/rooms/{roomId}/ghost"] = map[string]any{
					"get": map[string]any{"operationId": "ghostOp"},
				}
			},
			wantText: "no declared path parameter",
		},
		{
			name: "discriminator mapping does not resolve",
			mutate: func(doc map[string]any) {
				schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
				ma := schemas["ModeAction"].(map[string]any)
				d := ma["discriminator"].(map[string]any)
				d["mapping"].(map[string]any)["classic.guess"] = "#/components/schemas/Nope"
			},
			wantText: "does not resolve",
		},
		{
			// This is the defect that made every gameplay submission fail: the
			// mapping key said one thing and the schema's kind const said another.
			name: "discriminator key disagrees with the schema kind const",
			mutate: func(doc map[string]any) {
				schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
				ma := schemas["ModeAction"].(map[string]any)
				d := ma["discriminator"].(map[string]any)
				d["mapping"].(map[string]any)["classic.guess"] = "#/components/schemas/GridClaim"
			},
			wantText: "does not match the schema's kind const",
		},
		{
			name: "mutating operation without CSRF header",
			mutate: func(doc map[string]any) {
				paths := doc["paths"].(map[string]any)
				rooms := paths["/rooms"].(map[string]any)
				post := rooms["post"].(map[string]any)
				delete(post, "parameters")
			},
			wantText: "does not declare X-CSRF-Token",
		},
		{
			name: "empty ModeAction mapping",
			mutate: func(doc map[string]any) {
				schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
				ma := schemas["ModeAction"].(map[string]any)
				d := ma["discriminator"].(map[string]any)
				d["mapping"] = map[string]any{}
			},
			wantText: "mapping is empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeContract(t, tc.mutate)
			if err := initComponentParams(p); err != nil {
				t.Fatalf("init: %v", err)
			}
			rep, err := validateContract(p)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !rep.HasProblems() {
				t.Fatalf("validator accepted the mutation; want a problem mentioning %q", tc.wantText)
			}
			joined := strings.Join(rep.Problems, "\n")
			if !strings.Contains(joined, tc.wantText) {
				t.Errorf("problems do not mention %q:\n%s", tc.wantText, joined)
			}
		})
	}
}

// TestValidatorRejectsMalformedJSON asserts a broken document is an error, not a
// silent pass with zero operations.
func TestValidatorRejectsMalformedJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := validateContract(p); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}

// TestValidatorRejectsMissingVersion asserts the openapi version is required.
func TestValidatorRejectsMissingVersion(t *testing.T) {
	p := writeContract(t, func(doc map[string]any) { delete(doc, "openapi") })
	if err := initComponentParams(p); err != nil {
		t.Fatalf("init: %v", err)
	}
	rep, err := validateContract(p)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !rep.HasProblems() {
		t.Fatal("a document without an openapi version was accepted")
	}
	if !strings.Contains(strings.Join(rep.Problems, "\n"), "openapi") {
		t.Errorf("problems do not mention the version: %v", rep.Problems)
	}
}

// TestTemplatePlaceholders covers the placeholder parser directly.
func TestTemplatePlaceholders(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"/config", nil},
		{"/rooms/{roomId}", []string{"roomId"}},
		{"/rooms/{roomId}/memberships/{membershipId}", []string{"roomId", "membershipId"}},
		{"/a/{x}/b/{y}/c/{z}", []string{"x", "y", "z"}},
		{"/unclosed/{oops", nil},
	}
	for _, tc := range cases {
		got := templatePlaceholders(tc.path)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.path, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.path, got, tc.want)
				break
			}
		}
	}
}

// TestRoutesMatchServedTable asserts the tracked document and the router agree in
// both directions.
//
// This is the check that makes api/openapi.json trustworthy as the single source of
// truth: without it the document can be perfectly self-consistent while describing
// endpoints that 404, or while missing endpoints that ship. It is also the check that
// was declared but never wired — httpapi.ServedOperations existed with zero callers
// until this test and the CLI flag gave it one.
func TestRoutesMatchServedTable(t *testing.T) {
	if err := initComponentParams(repoContract); err != nil {
		t.Fatalf("init: %v", err)
	}
	rep, err := validateContract(repoContract)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := compareServedRoutes(repoContract, rep); err != nil {
		t.Fatalf("compare routes: %v", err)
	}
	if rep.HasProblems() {
		for _, p := range rep.Problems {
			t.Errorf("problem: %s", p)
		}
		t.Fatal("the document and the router disagree")
	}
	if rep.ServedRoutes == 0 {
		t.Fatal("served route count is 0; ServedOperations returned nothing")
	}
	if rep.ServedRoutes != rep.DocumentedRoutes {
		t.Errorf("served %d routes but documented %d", rep.ServedRoutes, rep.DocumentedRoutes)
	}
}

// TestRouteComparisonCatchesBothDriftDirections is the negative case for the route
// comparison. A one-directional check would pass while half the problem stayed hidden,
// so both directions are exercised.
func TestRouteComparisonCatchesBothDriftDirections(t *testing.T) {
	t.Run("documented but not served", func(t *testing.T) {
		p := writeContract(t, func(doc map[string]any) {
			paths := doc["paths"].(map[string]any)
			paths["/ghost-endpoint"] = map[string]any{
				"get": map[string]any{"operationId": "ghostOp"},
			}
		})
		if err := initComponentParams(p); err != nil {
			t.Fatalf("init: %v", err)
		}
		rep, err := validateContract(p)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if err := compareServedRoutes(p, rep); err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !rep.HasProblems() {
			t.Fatal("an endpoint the router does not serve was accepted")
		}
		joined := strings.Join(rep.Problems, "\n")
		if !strings.Contains(joined, "documented but not served") {
			t.Errorf("missing the not-served problem:\n%s", joined)
		}
		if !strings.Contains(joined, "/ghost-endpoint") {
			t.Errorf("problem does not name the endpoint:\n%s", joined)
		}
	})

	t.Run("served but not documented", func(t *testing.T) {
		p := writeContract(t, func(doc map[string]any) {
			paths := doc["paths"].(map[string]any)
			delete(paths, "/config")
		})
		if err := initComponentParams(p); err != nil {
			t.Fatalf("init: %v", err)
		}
		rep, err := validateContract(p)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if err := compareServedRoutes(p, rep); err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !rep.HasProblems() {
			t.Fatal("a served endpoint missing from the document was accepted")
		}
		joined := strings.Join(rep.Problems, "\n")
		if !strings.Contains(joined, "served but not documented") {
			t.Errorf("missing the undocumented problem:\n%s", joined)
		}
		if !strings.Contains(joined, "/api/v1/config") {
			t.Errorf("problem does not name the endpoint:\n%s", joined)
		}
	})
}

// TestOperationServerOverrideIsHonoured guards the root-level endpoints.
//
// /health/live and /health/ready declare servers:[{url:"/"}], overriding the
// document-level /api/v1 prefix. Ignoring that override made the comparison report
// four problems for two endpoints, so it is pinned here.
func TestOperationServerOverrideIsHonoured(t *testing.T) {
	rep := &contractReport{}
	if err := compareServedRoutes(repoContract, rep); err != nil {
		t.Fatalf("compare: %v", err)
	}
	joined := strings.Join(rep.Problems, "\n")
	for _, want := range []string{"/health/live", "/health/ready"} {
		if strings.Contains(joined, want) {
			t.Errorf("%s is reported as a route problem; the per-operation servers override was not honoured:\n%s",
				want, joined)
		}
	}
}
