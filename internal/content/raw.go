package content

// These adapters read source snapshots without writing back to data/.

// RawQuestionBankEntry is one item of data/question_bank_ready.json (118 items).
// It is the reviewed legacy classic question bank: the nine comparison fields
// plus identity and asset pointers.
type RawQuestionBankEntry struct {
	ID                string   `json:"id"`
	NameZh            string   `json:"name_zh"`
	NameTw            string   `json:"name_tw"`
	NameJp            string   `json:"name_jp"`
	NameEn            string   `json:"name_en"`
	Aliases           []string `json:"aliases"`
	Star              *int     `json:"star"`
	SurfaceGroup      []string `json:"surface_group"`
	DistanceGroup     []string `json:"distance_group"`
	RunningStyleGroup []string `json:"running_style_group"`
	SexType           string   `json:"sex_type"`
	G1Bracket         string   `json:"g1_bracket"`
	G23Bracket        string   `json:"g23_bracket"`
	G1HasJpn          bool     `json:"g1_has_jpn"`
	G23HasJpn         bool     `json:"g23_has_jpn"`
	SchoolGrade       string   `json:"school_grade"`
	Dormitory         string   `json:"dormitory"`
	ImageURL          string   `json:"image_url"`
	ImageLocalPath    string   `json:"image_local_path"`
	AssetID           string   `json:"asset_id"`
	SourcePriority    []string `json:"source_priority"`
}

// RawBlockedEntry is one item of data/question_bank_blocked.json (23 items).
// It has no Japanese name, so it is joined to the wiki snapshot through the
// normalised file_name, never by display name.
type RawBlockedEntry struct {
	FileName      string   `json:"file_name"`
	ID            string   `json:"id"`
	NameZh        string   `json:"name_zh"`
	Order         int      `json:"order"`
	ReleaseStatus string   `json:"release_status"`
	Reason        string   `json:"reason"`
	MissingFields []string `json:"missing_fields"`
}

// RawNormalizedCharacter is one file of data/normalized/characters/*.json. It is
// the bridge between the legacy romanised id and the wiki snapshot: it carries
// both the romanised id and the Japanese name.
type RawNormalizedCharacter struct {
	ID            string `json:"id"`
	Order         int    `json:"order"`
	FileName      string `json:"file_name"`
	ReleaseStatus string `json:"release_status"`
	Names         struct {
		ZhCN string  `json:"zh_cn"`
		ZhTW *string `json:"zh_tw"`
		Jp   string  `json:"jp"`
		En   string  `json:"en"`
	} `json:"names"`
	Aliases []string `json:"aliases"`
	Profile struct {
		Birthday    string `json:"birthday"`
		BirthdayRaw string `json:"birthday_raw"`
		HeightCm    *int   `json:"height_cm"`
		CV          string `json:"cv"`
		SchoolGrade string `json:"school_grade"`
		Dormitory   string `json:"dormitory"`
		Star        *int   `json:"star"`
		SexType     string `json:"sex_type"`
	} `json:"profile"`
	Racing struct {
		SurfaceGroup      []string          `json:"surface_group"`
		DistanceGroup     []string          `json:"distance_group"`
		RunningStyleGroup []string          `json:"running_style_group"`
		Aptitudes         map[string]string `json:"aptitudes"`
		G1Bracket         string            `json:"g1_bracket"`
		G23Bracket        string            `json:"g23_bracket"`
		G1HasJpn          bool              `json:"g1_has_jpn"`
		G23HasJpn         bool              `json:"g23_has_jpn"`
	} `json:"racing"`
	Media struct {
		ImageURL       string   `json:"image_url"`
		ImageURLs      []string `json:"image_urls"`
		ImageLocalPath string   `json:"image_local_path"`
		AssetID        string   `json:"asset_id"`
	} `json:"media"`
	Raw struct {
		MoegirlDetail struct {
			Aliases []string `json:"aliases"`
		} `json:"moegirl_detail"`
	} `json:"raw"`
	Sources struct {
		SourcePages []string `json:"source_pages"`
		SourceURLs  []string `json:"source_urls"`
	} `json:"sources"`
}

// RawWikiSnapshot is data/wiki/v2/characters.json. It holds 147 records, which
// include non-playable cast; implementation.status is not playability .
type RawWikiSnapshot struct {
	Meta       RawWikiMeta        `json:"meta"`
	Characters []RawWikiCharacter `json:"characters"`
}

// RawWikiMeta is the snapshot's own self-reported counts. They are recorded for
// comparison but never used as the published figure .
type RawWikiMeta struct {
	GeneratedAt  string            `json:"generatedAt"`
	SourceSite   string            `json:"sourceSite"`
	VersionScope string            `json:"versionScope"`
	Counts       map[string]int    `json:"counts"`
	Sources      map[string]string `json:"sources"`
}

// RawWikiCharacter is one wiki snapshot record.
type RawWikiCharacter struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Implementation struct {
		Status         string `json:"status"`
		InGameCategory bool   `json:"inGameCategory"`
		HasOwnPage     bool   `json:"hasOwnPage"`
		Source         string `json:"source"`
		Note           string `json:"note"`
	} `json:"implementation"`
	Names struct {
		Ja           string   `json:"ja"`
		ZhHans       string   `json:"zhHans"`
		ZhHant       string   `json:"zhHant"`
		En           string   `json:"en"`
		Romaji       *string  `json:"romaji"`
		ZhAll        []string `json:"zhAll"`
		GameZhHans   string   `json:"gameZhHans"`
		GameZhHant   string   `json:"gameZhHant"`
		WikiTitle    string   `json:"wikiTitle"`
		NicknameJp   string   `json:"nicknameJp"`
		MoegirlTitle string   `json:"moegirlTitle"`
		MoegirlZh    string   `json:"moegirlZh"`
		MoegirlJa    string   `json:"moegirlJa"`
		ZhHantSource string   `json:"zhHantSource"`
		EnSource     string   `json:"enSource"`
		JaConfidence string   `json:"jaConfidence"`
		JaSource     string   `json:"jaSource"`
	} `json:"names"`
	Profile struct {
		Birthday struct {
			Raw      string `json:"raw"`
			MonthDay string `json:"monthDay"`
			Month    *int   `json:"month"`
			Day      *int   `json:"day"`
		} `json:"birthday"`
		HeightCm *int   `json:"heightCm"`
		CV       string `json:"cv"`
		Star     *int   `json:"star"`
	} `json:"profile"`
	Game struct {
		Aptitudes        RawAptitudeBlock `json:"aptitudes"`
		Stats            map[string][]int `json:"stats"`
		Growth           map[string]int   `json:"growth"`
		Skills           *RawSkills       `json:"skills"`
		Goals            []RawGoal        `json:"goals"`
		Star             *int             `json:"star"`
		Introduction     string           `json:"introduction"`
		SelfIntroduction string           `json:"selfIntroduction"`
		DataSource       string           `json:"dataSource"`
	} `json:"game"`
	Outfits        []RawOutfit       `json:"outfits"`
	PrototypeHorse RawPrototypeHorse `json:"prototypeHorse"`
	Voices         []RawVoice        `json:"voices"`
	Trivia         struct {
		Secrets     []string `json:"secrets"`
		SourceName  string   `json:"sourceName"`
		Quote       string   `json:"quote"`
		RosterIntro string   `json:"rosterIntro"`
	} `json:"trivia"`
	Article map[string]any `json:"article"`
	Sources struct {
		Pages []string `json:"pages"`
	} `json:"sources"`
}

// RawAptitudeBlock is the game data aptitude block: the raw A-G grades. These are
// a different field from the legacy *_group buckets .
type RawAptitudeBlock struct {
	Surface      map[string]string `json:"surface"`
	Distance     map[string]string `json:"distance"`
	RunningStyle map[string]string `json:"runningStyle"`
}

// RawSkills is the per-character skill block. 343 attribution rows exist over
// 41 characters, but only 22 match a definition name exactly .
type RawSkills struct {
	Initial   []string            `json:"initial"`
	Awakening map[string][]string `json:"awakening"`
	Unique    struct {
		LowStar   []string `json:"lowStar"`
		ThreeStar []string `json:"threeStar"`
	} `json:"unique"`
}

// Rows flattens every skill attribution row in this block.
func (s *RawSkills) Rows() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, 8)
	out = append(out, s.Initial...)
	for _, group := range sortedKeys(s.Awakening) {
		out = append(out, s.Awakening[group]...)
	}
	out = append(out, s.Unique.LowStar...)
	out = append(out, s.Unique.ThreeStar...)
	return out
}

// RawGoal is one training objective row.
type RawGoal struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Time      string `json:"time"`
	Condition string `json:"condition"`
	Race      string `json:"race"`
}

// RawOutfit is one record of data/wiki/v2/outfits.json (272 items).
type RawOutfit struct {
	OutfitID        string `json:"outfitId"`
	CharacterID     string `json:"characterId"`
	CharacterNameZh string `json:"characterNameZh"`
	CharacterNameJa string `json:"characterNameJa"`
	NicknameJp      string `json:"nicknameJp"`
	NicknameZhHans  string `json:"nicknameZhHans"`
	NicknameZhHant  string `json:"nicknameZhHant"`
	Title           string `json:"title"`
	Type            string `json:"type"`
	Star            *int   `json:"star"`
	Implemented     bool   `json:"implemented"`
}

// RawOutfitFile is data/wiki/v2/outfits.json.
type RawOutfitFile struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Outfits []RawOutfit `json:"outfits"`
}

// RawPrototypeHorse is the prototype block inside a wiki character record.
type RawPrototypeHorse struct {
	NKBID string `json:"nkbId"`
	Names struct {
		Zh string `json:"zh"`
		Ja string `json:"ja"`
		En string `json:"en"`
	} `json:"names"`
	Gender      string `json:"gender"`
	Birthday    string `json:"birthday"`
	BirthDate   string `json:"birthDate"`
	Owner       string `json:"owner"`
	Color       string `json:"color"`
	Trainer     string `json:"trainer"`
	Farm        string `json:"farm"`
	Nation      string `json:"nation"`
	RaceResult  string `json:"raceResult"`
	MainWinning string `json:"mainWinning"`
	Glory       string `json:"glory"`
	Earnings    string `json:"earnings"`
	Pedigree    struct {
		Sire     string `json:"sire"`
		Dam      string `json:"dam"`
		SireSire string `json:"sireSire"`
		SireDam  string `json:"sireDam"`
		DamSire  string `json:"damSire"`
		DamDam   string `json:"damDam"`
	} `json:"pedigree"`
	RaceHistory    []RawRaceRow `json:"raceHistory"`
	RaceCount      int          `json:"raceCount"`
	SourceBiliwiki bool         `json:"sourceBiliwiki"`
	SourceMoegirl  bool         `json:"sourceMoegirl"`
}

// RawRaceRow is one prototype start. 2,127 rows over 1,590 raceIds .
type RawRaceRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Date       string `json:"date"`
	Course     string `json:"course"`
	Surface    string `json:"surface"`
	Condition  string `json:"condition"`
	Grade      string `json:"grade"`
	Distance   string `json:"distance"`
	FinishRank *int   `json:"finishRank"`
	Jockey     string `json:"jockey"`
	Odds       string `json:"odds"`
	Weight     string `json:"weight"`
	Earnings   string `json:"earnings"`
}

// RawVoice is one voiced line. 15,222 rows over 80 characters, but 6,177 lack a
// simplified-Chinese transcript .
type RawVoice struct {
	AudioURL string `json:"audioUrl"`
	Jp       string `json:"jp"`
	Cht      string `json:"cht"`
	Chs      string `json:"chs"`
}

// RawAssetManifestEntry is one record of data/assets/asset-manifest.json.
type RawAssetManifestEntry struct {
	AssetID       string `json:"asset_id"`
	CharacterID   string `json:"character_id"`
	SourceSite    string `json:"source_site"`
	SourceURL     string `json:"source_url"`
	LocalPath     string `json:"local_path"`
	DownloadedAt  string `json:"downloaded_at"`
	UsageScope    string `json:"usage_scope"`
	CopyrightNote string `json:"copyright_note"`
	CacheStatus   string `json:"cache_status"`
	LastCheckedAt string `json:"last_checked_at"`
}

// RawSkillFile is data/wiki/skills.json.
type RawSkillFile struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Skills []RawSkillDef `json:"skills"`
}

// RawSkillDef is one skill definition (1,289 rows).
type RawSkillDef struct {
	ID         string `json:"id"`
	NameZh     string `json:"nameZh"`
	DescZh     string `json:"descZh"`
	Rarity     string `json:"rarity"`
	GroupID    string `json:"groupId"`
	GradeValue int    `json:"gradeValue"`
	Category   string `json:"category"`
	Condition  string `json:"condition"`
}

// RawFieldOverrides is data/manual/field-overrides.json: a manual correction
// table keyed by legacy romanised id. Currently one entry, which is why the
// overrides count must appear in the quality report rather than being assumed.
type RawFieldOverrides map[string]map[string]string
