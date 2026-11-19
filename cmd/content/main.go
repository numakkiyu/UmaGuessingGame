// Command content compiles read-only data snapshots into immutable, checksum-verified packs.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/content"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "content: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "build":
		return cmdBuild(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "current":
		return cmdCurrent(args[1:])
	case "sections":
		return cmdSections(args[1:])
	case "contract":
		return cmdContract(args[1:])
	case "audio-verify":
		return cmdAudioVerify(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usageText)
		return nil
	default:
		return usageError()
	}
}

const usageText = `content - compile the content pack

Commands:
  build         read data/ and publish a new pack under content/
  verify        re-verify the manifest and every file checksum of a pack
  current       print the currently published pack directory
  sections      list the sections a pack carries
  contract      check the served API against the project OpenAPI document
  audio-verify  decode the full WebM/Opus library and verify its manifest

Run "content build -h" for the build flags.
`

func usageError() error {
	fmt.Fprint(os.Stderr, usageText)
	return errors.New("expected a command: build, verify, current, sections, contract or audio-verify")
}

func cmdAudioVerify(args []string) error {
	fs := flag.NewFlagSet("audio-verify", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root containing data/")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("audio-verify: unexpected positional arguments")
	}
	report, err := content.VerifyAudioLibrary(*root)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root containing data/")
	out := fs.String("out", "", "content output directory (default <root>/content)")
	evidence := fs.String("evidence", "", "write the build evidence JSON to this path")
	dry := fs.Bool("dry-run", false, "compile and report without publishing")
	datasetVersion := fs.String("dataset-version", "", "override the derived dataset version")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Asset verification stats the filesystem with repo-relative paths, so the
	// working directory is pinned to the root for the whole run.
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	if err := os.Chdir(absRoot); err != nil {
		return fmt.Errorf("enter root %s: %w", absRoot, err)
	}

	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(absRoot, "content")
	}

	started := time.Now()
	res, err := content.Publish(content.PublishOptions{
		Root:           absRoot,
		OutputDir:      outDir,
		DatasetVersion: *datasetVersion,
		DryRun:         *dry,
	})
	if err != nil {
		return err
	}
	elapsed := time.Since(started)

	fmt.Printf("pack version : %s\n", res.Version)
	if *dry {
		fmt.Printf("published    : no (dry run)\n")
	} else {
		fmt.Printf("published    : %s\n", res.Path)
	}
	fmt.Printf("dataset      : %s\n", res.Manifest.DatasetVersion)
	fmt.Printf("generated at : %s\n", res.Manifest.GeneratedAt)
	fmt.Printf("elapsed      : %s\n", elapsed.Round(time.Millisecond))
	fmt.Println()
	printAdmissions(res.Admissions)
	fmt.Println()
	printCounts(res.Report)
	fmt.Println()
	printGaps(res.Report)

	if *evidence != "" {
		if err := writeEvidence(*evidence, res, elapsed); err != nil {
			return err
		}
		fmt.Printf("\nevidence     : %s\n", *evidence)
	}
	return nil
}

func printAdmissions(list []content.ModeAdmission) {
	fmt.Println("mode admission:")
	fmt.Printf("  %-16s %-9s %8s  %s\n", "MODE", "STATE", "ITEMS", "SECTION")
	sorted := append([]content.ModeAdmission(nil), list...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Mode < sorted[j].Mode })
	for _, a := range sorted {
		state := "unavailable"
		if a.Available {
			state = "available"
		}
		fmt.Printf("  %-16s %-9s %8d  %s\n", a.Mode, state, a.QuestionCount, a.Section)
	}
	fmt.Println()
	fmt.Println("unavailable reasons:")
	any := false
	for _, a := range sorted {
		if a.Available {
			continue
		}
		any = true
		fmt.Printf("  %s: %s\n", a.Mode, a.Reason)
	}
	if !any {
		fmt.Println("  (none)")
	}
}

func printCounts(r *content.QualityReport) {
	ec := r.EntityCounts
	fmt.Println("recomputed entity counts:")
	fmt.Printf("  characters %d (playable %d, planned %d, npc %d)\n",
		ec.Characters, ec.CharactersPlayable, ec.CharactersPlanned, ec.CharactersNPC)
	fmt.Printf("  classic ready %d, blocked %d, ambiguous-vector %d\n",
		ec.ClassicReady, ec.ClassicBlocked, ec.AmbiguousVectors)
	fmt.Printf("  outfits %d, aliases %d, secrets %d, goals %d, voices %d\n",
		ec.Outfits, ec.Aliases, ec.Secrets, ec.Goals, ec.Voices)
	fmt.Printf("  races %d, race entries %d\n", ec.Races, ec.RaceEntries)
	fmt.Printf("  prototype horses %d (with NKB id %d)\n", ec.PrototypeHorses, ec.PrototypeWithNKB)
	fmt.Printf("  assets verified on disk %d of manifest %d / cache index %d\n",
		r.Assets.VerifiedOnDisk, r.Assets.ManifestEntries, r.Assets.CacheIndexEntries)
}

func printGaps(r *content.QualityReport) {
	fmt.Println("data gaps (measured, not suppressed):")
	for _, g := range r.Gaps {
		fmt.Printf("  [%s] %-12s %s\n", g.Severity, g.Area, g.Measured)
	}
}

// writeEvidence serialises the build evidence. It is written outside content/ so
// the pack stays purely a publication artifact.
func writeEvidence(path string, res *content.PublishResult, elapsed time.Duration) error {
	ev := content.BuildEvidence(res, elapsed)
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	b = append(b, '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create evidence dir: %w", err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write evidence: %w", err)
	}
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "pack directory (default: the current version)")
	out := fs.String("out", "", "content directory used to resolve the current version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *dir
	if target == "" {
		contentDir := *out
		if contentDir == "" {
			contentDir = "content"
		}
		cur, err := content.ResolveCurrent(contentDir)
		if err != nil {
			return err
		}
		target = cur
	}
	pack, err := content.OpenPack(target)
	if err != nil {
		return err
	}
	fmt.Printf("verified %s\n", target)
	fmt.Printf("  pack version : %s\n", pack.Version())
	m := pack.Manifest()
	fmt.Printf("  schema       : %s\n", m.SchemaVersion)
	fmt.Printf("  dataset      : %s\n", m.DatasetVersion)
	fmt.Printf("  reviewStatus : %s\n", m.ReviewStatus)
	fmt.Printf("  sha256       : %s\n", m.SHA256)
	fmt.Printf("  files        : %d\n", len(m.Files))
	fmt.Printf("  sections     : %v\n", pack.SectionNames())
	return nil
}

func cmdCurrent(args []string) error {
	fs := flag.NewFlagSet("current", flag.ContinueOnError)
	dir := fs.String("out", "content", "content directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cur, err := content.ResolveCurrent(*dir)
	if err != nil {
		return err
	}
	fmt.Println(cur)
	return nil
}

func cmdSections(args []string) error {
	fs := flag.NewFlagSet("sections", flag.ContinueOnError)
	dir := fs.String("dir", "", "pack directory (default: the current version)")
	out := fs.String("out", "content", "content directory used to resolve the current version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *dir
	if target == "" {
		cur, err := content.ResolveCurrent(*out)
		if err != nil {
			return err
		}
		target = cur
	}
	pack, err := content.OpenPack(target)
	if err != nil {
		return err
	}
	for _, name := range pack.SectionNames() {
		b, _ := pack.Section(name)
		fmt.Printf("%-24s %8d bytes\n", name, len(b))
	}
	return nil
}
