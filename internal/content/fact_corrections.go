package content

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

func validateFieldOverrides(in *RawInputs) error {
	known := map[string]bool{}
	for _, r := range in.Ready {
		known[r.ID] = true
	}
	for _, n := range in.Normalized {
		known[n.ID] = true
	}
	for id, fields := range in.Overrides {
		if !known[id] {
			return fmt.Errorf("content: field correction references unknown legacy id %q", id)
		}
		racing := false
		for key, value := range fields {
			switch key {
			case "grade", "dormitory", "sex_type", "racing_source_url", "racing_note", "racing_checked_at":
			case "g1_bracket":
				racing = true
				if !containsString([]string{"0", "1-2", "3-4", "5+"}, value) {
					return fmt.Errorf("content: invalid G1 correction for %s: %q", id, value)
				}
			case "g23_bracket":
				racing = true
				if !containsString([]string{"0", "1-3", "4-6", "7+"}, value) {
					return fmt.Errorf("content: invalid G23 correction for %s: %q", id, value)
				}
			case "g1_has_jpn", "g23_has_jpn":
				racing = true
				if value != "true" && value != "false" {
					return fmt.Errorf("content: invalid Jpn marker for %s", id)
				}
			default:
				return fmt.Errorf("content: unknown correction field %q for %s", key, id)
			}
		}
		if racing {
			u, err := url.ParseRequestURI(fields["racing_source_url"])
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return fmt.Errorf("content: racing correction for %s requires an HTTPS source", id)
			}
			if _, err := time.Parse("2006-01-02", fields["racing_checked_at"]); err != nil {
				return fmt.Errorf("content: racing correction for %s requires a checked date", id)
			}
			if strings.TrimSpace(fields["racing_note"]) == "" {
				return fmt.Errorf("content: racing correction for %s requires a counting note", id)
			}
		}
	}
	return nil
}

// Race corrections preserve the fetched snapshot and share one value across all starters.
type RawRaceCorrection struct {
	Grade     string `json:"grade"`
	SourceURL string `json:"sourceURL"`
	CheckedAt string `json:"checkedAt"`
}

func applyRaceCorrections(races []Race, corrections map[string]RawRaceCorrection) error {
	byID := map[string]*Race{}
	for i := range races {
		byID[races[i].RaceID] = &races[i]
	}
	for id, correction := range corrections {
		r := byID[id]
		if r == nil {
			return fmt.Errorf("content: race correction references unknown race %s", id)
		}
		if !containsString([]string{"G1", "G2", "G3", "Jpn1", "Jpn2", "Jpn3"}, correction.Grade) {
			return fmt.Errorf("content: invalid corrected grade for race %s", id)
		}
		u, err := url.ParseRequestURI(correction.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("content: race correction %s requires an HTTPS source", id)
		}
		if _, err := time.Parse("2006-01-02", correction.CheckedAt); err != nil {
			return fmt.Errorf("content: race correction %s requires a checked date", id)
		}
		r.Grade = correction.Grade
		for i := range r.Entries {
			r.Entries[i].Grade = correction.Grade
		}
	}
	return nil
}

// validateKnowledge rejects malformed facts before they can become answer keys.
func validateKnowledge(in *RawInputs) error {
	races := map[string]RawRaceRow{}
	for _, c := range in.Wiki.Characters {
		birthday := c.Profile.Birthday
		if birthday.Month != nil && birthday.Day != nil {
			m, d := *birthday.Month, *birthday.Day
			date := time.Date(2000, time.Month(m), d, 0, 0, 0, 0, time.UTC)
			if int(date.Month()) != m || date.Day() != d {
				return fmt.Errorf("content: invalid birthday for %s", c.ID)
			}
		}
		for _, group := range []map[string]string{c.Game.Aptitudes.Surface, c.Game.Aptitudes.Distance, c.Game.Aptitudes.RunningStyle} {
			for key, value := range group {
				if len(value) != 1 || value[0] < 'A' || value[0] > 'G' {
					return fmt.Errorf("content: invalid aptitude %s=%q for %s", key, value, c.ID)
				}
			}
		}
		seen := map[string]bool{}
		for _, row := range c.PrototypeHorse.RaceHistory {
			if row.ID == "" {
				continue
			}
			if seen[row.ID] {
				return fmt.Errorf("content: duplicate start %s for %s", row.ID, c.ID)
			}
			seen[row.ID] = true
			if row.FinishRank != nil && *row.FinishRank < 1 {
				return fmt.Errorf("content: invalid finish rank for %s in %s", c.ID, row.ID)
			}
			if previous, ok := races[row.ID]; ok {
				for field, values := range map[string][2]string{"date": {previous.Date, row.Date}, "course": {previous.Course, row.Course}, "surface": {previous.Surface, row.Surface}, "distance": {previous.Distance, row.Distance}, "grade": {previous.Grade, row.Grade}} {
					if values[0] != "" && values[1] != "" && values[0] != values[1] {
						return fmt.Errorf("content: conflicting %s in race %s", field, row.ID)
					}
				}
				if row.Date == "" {
					row.Date = previous.Date
				}
				if row.Course == "" {
					row.Course = previous.Course
				}
				if row.Surface == "" {
					row.Surface = previous.Surface
				}
				if row.Distance == "" {
					row.Distance = previous.Distance
				}
				if row.Grade == "" {
					row.Grade = previous.Grade
				}
			}
			races[row.ID] = row
		}
	}
	return nil
}
