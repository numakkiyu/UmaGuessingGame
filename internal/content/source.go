package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// Pack is a loaded, checksum-verified content pack that satisfies
// modes.ContentSource. It reads only from the published public/ tree: a mode must
// never receive private answer material through Section .
//
// Pack is safe for concurrent use: sections are decoded once at load time and
// served from an immutable map.
type Pack struct {
	manifest *Manifest
	version  string
	sections map[string]json.RawMessage
	// aliases maps every accepted alias to its primary section name.
	aliases map[string]string
	dir     string
}

// compile-time proof that Pack satisfies the rule-engine contract.
var _ modes.ContentSource = (*Pack)(nil)

// PackOption tunes loading.
type PackOption func(*packLoadConfig)

type packLoadConfig struct {
	// allowUnverified skips the checksum gate. It exists only for tests that
	// exercise drift rejection; production code must not set it.
	allowUnverified bool
}

// WithUnverifiedChecksum disables checksum verification. Use only in tests.
func WithUnverifiedChecksum() PackOption {
	return func(c *packLoadConfig) { c.allowUnverified = true }
}

// OpenPack loads the pack at dir, verifies its manifest checksum, and decodes
// every public section. A checksum mismatch is a hard error: a server must not
// load a partially modified pack .
func OpenPack(dir string, opts ...PackOption) (*Pack, error) {
	cfg := &packLoadConfig{}
	for _, o := range opts {
		o(cfg)
	}

	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("content: read manifest in %s: %w", dir, err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("content: parse manifest in %s: %w", dir, err)
	}
	if !cfg.allowUnverified {
		if err := VerifyManifest(&m); err != nil {
			return nil, err
		}
		// Every published file must still match its declared digest, otherwise a
		// file was edited in place after publication.
		if err := verifyPublishedPack(dir); err != nil {
			return nil, err
		}
	}

	p := &Pack{
		manifest: &m,
		version:  m.ContentPackVersion,
		sections: make(map[string]json.RawMessage, len(m.PublicSections)),
		aliases:  make(map[string]string, len(sectionAliases)),
		dir:      dir,
	}
	for alias, primary := range sectionAliases {
		p.aliases[alias] = primary
	}

	for _, name := range m.PublicSections {
		full := filepath.Join(dir, PublicDirName, sectionFileName(name))
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("content: read public section %q: %w", name, err)
		}
		if !json.Valid(b) {
			return nil, fmt.Errorf("content: public section %q is not valid JSON", name)
		}
		p.sections[name] = json.RawMessage(b)
	}

	return p, nil
}

// OpenCurrent resolves the current version pointer and opens that pack.
func OpenCurrent(contentDir string, opts ...PackOption) (*Pack, error) {
	dir, err := ResolveCurrent(contentDir)
	if err != nil {
		return nil, err
	}
	return OpenPack(dir, opts...)
}

// Section implements modes.ContentSource. It returns the raw JSON for a named
// section. The primary name is tried first; accepted aliases resolve to it. An
// unavailable mode has no section, so ok is false and the mode reports itself
// unavailable rather than the pack failing to load .
func (p *Pack) Section(name string) (json.RawMessage, bool) {
	if p == nil {
		return nil, false
	}
	if b, ok := p.sections[name]; ok {
		return b, true
	}
	if primary, ok := p.aliases[name]; ok {
		if b, ok := p.sections[primary]; ok {
			return b, true
		}
	}
	// Fall back to a normalised spelling so a caller's casing or padding does not
	// silently miss a section that exists.
	n := normalizeSectionName(name)
	if n != name {
		if b, ok := p.sections[n]; ok {
			return b, true
		}
		if primary, ok := p.aliases[n]; ok {
			if b, ok := p.sections[primary]; ok {
				return b, true
			}
		}
	}
	return nil, false
}

// Version implements modes.ContentSource.
func (p *Pack) Version() string {
	if p == nil {
		return ""
	}
	return p.version
}

// Manifest returns the verified manifest.
func (p *Pack) Manifest() *Manifest {
	if p == nil {
		return nil
	}
	return p.manifest
}

// Dir returns the pack directory.
func (p *Pack) Dir() string {
	if p == nil {
		return ""
	}
	return p.dir
}

// SectionNames lists the sections this pack actually carries, sorted.
func (p *Pack) SectionNames() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.sections))
	for k := range p.sections {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ModeAvailable reports whether a mode was admitted when this pack was built.
func (p *Pack) ModeAvailable(mode string) (ModeAdmission, bool) {
	if p == nil || p.manifest == nil {
		return ModeAdmission{}, false
	}
	for _, a := range p.manifest.Modes {
		if a.Mode == mode {
			return a, true
		}
	}
	return ModeAdmission{}, false
}

// SecretStore gives a server read access to the private tree. It is a distinct
// type from Pack on purpose: code that handles public projections cannot
// accidentally reach the answer material through it .
type SecretStore struct {
	mu       sync.RWMutex
	dir      string
	version  string
	sections map[string]json.RawMessage
}

// OpenSecrets loads the private sections of a published pack. It verifies the
// same manifest checksum before reading.
func OpenSecrets(dir string, opts ...PackOption) (*SecretStore, error) {
	cfg := &packLoadConfig{}
	for _, o := range opts {
		o(cfg)
	}
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("content: read manifest in %s: %w", dir, err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("content: parse manifest in %s: %w", dir, err)
	}
	if !cfg.allowUnverified {
		if err := VerifyManifest(&m); err != nil {
			return nil, err
		}
	}
	s := &SecretStore{
		dir:      dir,
		version:  m.ContentPackVersion,
		sections: make(map[string]json.RawMessage, len(m.PrivateSections)),
	}
	for _, name := range m.PrivateSections {
		full := filepath.Join(dir, PrivateDirName, sectionFileName(name))
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("content: read private section %q: %w", name, err)
		}
		if !json.Valid(b) {
			return nil, fmt.Errorf("content: private section %q is not valid JSON", name)
		}
		s.sections[name] = json.RawMessage(b)
	}
	return s, nil
}

// Section returns a private section. It deliberately does not accept aliases:
// secret material must be addressed by its exact name.
func (s *SecretStore) Section(name string) (json.RawMessage, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.sections[name]
	return b, ok
}

// Version returns the pack version the secrets were loaded from.
func (s *SecretStore) Version() string {
	if s == nil {
		return ""
	}
	return s.version
}

// SectionNames lists the private section names.
func (s *SecretStore) SectionNames() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sections))
	for k := range s.sections {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalizeSectionName lower-cases and trims a section name so a caller's
// spelling does not silently miss.
func normalizeSectionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
