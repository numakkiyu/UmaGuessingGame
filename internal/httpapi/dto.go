package httpapi

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// Wire DTOs.
//
// Every type here is an explicit whitelist: it contains exactly the fields the
// contract declares, and nothing is copied in from an internal struct wholesale.
// This is what makes "private information never enters an unauthorised
// serialisation" structural rather than a matter of discipline .
//
// Field names and required-ness follow
// api/openapi.json; `omitempty` is used only where the
// contract marks a property optional.

// ---- Problem (handled by internal/httpapi/problem) ----

// ---- session ----

// sessionDTO matches components.schemas.Session.
type sessionDTO struct {
	SessionID   string `json:"sessionId"`
	DisplayName string `json:"displayName"`
	CSRFToken   string `json:"csrfToken"`
	ExpiresAt   string `json:"expiresAt"`
	ServerTime  string `json:"serverTime"`
}

// sessionCreateDTO matches SessionCreate.
type sessionCreateDTO struct {
	DisplayName string `json:"displayName"`
}

// sessionUpdateDTO matches SessionUpdate.
type sessionUpdateDTO struct {
	DisplayName string `json:"displayName"`
}

// ---- config ----

// publicConfigDTO matches PublicConfig.
type publicConfigDTO struct {
	Admission         admission.PublicConfig     `json:"admission"`
	SiteName          string                     `json:"siteName"`
	APIVersion        string                     `json:"apiVersion"`
	ServerTime        string                     `json:"serverTime"`
	DeploymentMode    string                     `json:"deploymentMode"`
	MaxRoomMembers    int                        `json:"maxRoomMembers"`
	MaxRoomSpectators int                        `json:"maxRoomSpectators"`
	EnabledModeIDs    []string                   `json:"enabledModeIds"`
	ModeCapabilities  map[string]map[string]bool `json:"modeCapabilities"`
	DefaultTransport  string                     `json:"defaultTransport"`
	AssetBaseURL      string                     `json:"assetBaseUrl"`
	BuildVersion      string                     `json:"buildVersion"`
}

// modeDTO matches components.schemas.Mode.
type modeDTO struct {
	ModeID            string `json:"modeId"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	RulesVersion      string `json:"rulesVersion"`
	Availability      string `json:"availability"`
	UnavailableReason string `json:"unavailableReason,omitempty"`
	MinPlayers        int    `json:"minPlayers"`
	MaxPlayers        int    `json:"maxPlayers"`
	EstimatedMinutes  int    `json:"estimatedMinutes"`
	InformationStyle  string `json:"informationStyle"`
	SupportsTeams     bool   `json:"supportsTeams"`
	ConfigSchemaURL   string `json:"configSchemaUrl,omitempty"`
}

// modeListDTO matches ModeList.
type modeListDTO struct {
	Items      []modeDTO `json:"items"`
	ServerTime string    `json:"serverTime"`
}

// aptitudesDTO matches components.schemas.Aptitudes.
type aptitudesDTO struct {
	Turf   string `json:"turf"`
	Dirt   string `json:"dirt"`
	Sprint string `json:"sprint"`
	Mile   string `json:"mile"`
	Medium string `json:"medium"`
	Long   string `json:"long"`
	Front  string `json:"front"`
	Pace   string `json:"pace"`
	Late   string `json:"late"`
	End    string `json:"end"`
}

// characterDTO matches components.schemas.Character.
//
// Only public fields appear: no answer material, no private clue, no seed.
type characterDTO struct {
	SearchNames     *characternames.Names `json:"searchNames,omitempty"`
	LocalizedNames  map[string]string     `json:"localizedNames,omitempty"`
	CharacterID     string                `json:"characterId"`
	DisplayName     string                `json:"displayName"`
	Aliases         []string              `json:"aliases"`
	EntityKind      string                `json:"entityKind"`
	PortraitAssetID string                `json:"portraitAssetId,omitempty"`
	// ImagePath is the local media route; RemoteImageURL is the upstream CDN URL.
	// Both are published so a client can prefer the CDN and fall back to the local
	// file, which keeps server bandwidth low without breaking a LAN event .
	ImagePath      string        `json:"imagePath,omitempty"`
	RemoteImageURL string        `json:"remoteImageUrl,omitempty"`
	Birthday       string        `json:"birthday,omitempty"`
	HeightCm       int           `json:"heightCm,omitempty"`
	Aptitudes      *aptitudesDTO `json:"aptitudes,omitempty"`
	DatasetVersion string        `json:"datasetVersion"`
}

// characterListDTO matches CharacterList.
type characterListDTO struct {
	Items          []characterDTO `json:"items"`
	NextCursor     *string        `json:"nextCursor"`
	DatasetVersion string         `json:"datasetVersion"`
}

// ---- rooms ----

// roomCreateDTO matches RoomCreate.
type roomCreateDTO struct {
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	ModeConfig      jsonRawMessage `json:"modeConfig"`
	MaxPlayers      int            `json:"maxPlayers"`
	MaxSpectators   int            `json:"maxSpectators"`
	Visibility      string         `json:"visibility"`
	AllowSpectators bool           `json:"allowSpectators"`
}

// roomPatchDTO matches RoomPatch.
type roomPatchDTO struct {
	ExpectedRoomVersion int64          `json:"expectedRoomVersion"`
	Name                *string        `json:"name,omitempty"`
	ModeConfig          jsonRawMessage `json:"modeConfig,omitempty"`
	AllowSpectators     *bool          `json:"allowSpectators,omitempty"`
}

// membershipDTO matches components.schemas.Membership.
type membershipDTO struct {
	Nickname     string   `json:"nickname,omitempty"`
	PublicCode   string   `json:"publicCode,omitempty"`
	PlayerNumber int64    `json:"playerNumber,omitempty"`
	MembershipID string   `json:"membershipId"`
	DisplayName  string   `json:"displayName"`
	Role         string   `json:"role"`
	TeamID       *string  `json:"teamId"`
	Ready        bool     `json:"ready"`
	Online       bool     `json:"online"`
	Capabilities []string `json:"capabilities"`
}

// teamDTO matches components.schemas.Team.
type teamDTO struct {
	TeamID     string `json:"teamId"`
	Label      string `json:"label"`
	ColorToken string `json:"colorToken"`
}

// teamCreateDTO matches TeamCreate.
type teamCreateDTO struct {
	Label      string `json:"label"`
	ColorToken string `json:"colorToken"`
}

// membershipCreateDTO matches MembershipCreate.
type membershipCreateDTO struct {
	InviteCode    string `json:"inviteCode"`
	RequestedRole string `json:"requestedRole"`
}

// membershipPatchDTO matches MembershipPatch.
type membershipPatchDTO struct {
	Ready               *bool   `json:"ready,omitempty"`
	TeamID              *string `json:"teamId,omitempty"`
	RequestedRole       *string `json:"requestedRole,omitempty"`
	ExpectedRoomVersion int64   `json:"expectedRoomVersion"`
}

// roundSummaryDTO matches components.schemas.RoundSummary.
type roundSummaryDTO struct {
	RoundID            string  `json:"roundId"`
	ModeID             string  `json:"modeId"`
	Phase              string  `json:"phase"`
	PhaseVersion       int64   `json:"phaseVersion"`
	DeadlineAt         *string `json:"deadlineAt"`
	Paused             bool    `json:"paused"`
	RulesVersion       string  `json:"rulesVersion"`
	DatasetVersion     string  `json:"datasetVersion"`
	ContentPackVersion string  `json:"contentPackVersion"`
}

// roomSnapshotDTO matches components.schemas.RoomSnapshot.
type roomSnapshotDTO struct {
	RoomID       string           `json:"roomId"`
	ModeID       string           `json:"modeId"`
	InviteCode   *string          `json:"inviteCode"`
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Status       string           `json:"status"`
	RoomVersion  int64            `json:"roomVersion"`
	Viewer       membershipDTO    `json:"viewer"`
	Members      []membershipDTO  `json:"members"`
	Teams        []teamDTO        `json:"teams"`
	CurrentRound *roundSummaryDTO `json:"currentRound"`
	ServerTime   string           `json:"serverTime"`
	ServerEpoch  string           `json:"serverEpoch"`
	Cursor       string           `json:"cursor"`
}

// roomCreationDTO matches components.schemas.RoomCreation.
type roomCreationDTO struct {
	Room       roomSnapshotDTO `json:"room"`
	InviteCode string          `json:"inviteCode"`
	InviteURL  string          `json:"inviteUrl"`
}

// controlLeaseRequestDTO matches ControlLeaseRequest.
type controlLeaseRequestDTO struct {
	RoundID            string `json:"roundId"`
	MembershipID       string `json:"membershipId"`
	ExpectedGeneration int64  `json:"expectedGeneration"`
}

// controlLeaseDTO matches components.schemas.ControlLease.
type controlLeaseDTO struct {
	LeaseID           string `json:"leaseId"`
	RoundID           string `json:"roundId"`
	MembershipID      string `json:"membershipId"`
	ControlGeneration int64  `json:"controlGeneration"`
	ExpiresAt         string `json:"expiresAt"`
}

// roundCreateDTO matches RoundCreate.
type roundCreateDTO struct {
	ModeConfig jsonRawMessage `json:"modeConfig"`
	SegmentID  string         `json:"segmentId,omitempty"`
}

// transitionCreateDTO matches TransitionCreate.
type transitionCreateDTO struct {
	ExcludedOptionIDs    []string `json:"excludedOptionIds,omitempty"`
	Target               string   `json:"target"`
	ExpectedPhaseVersion int64    `json:"expectedPhaseVersion"`
	Reason               string   `json:"reason,omitempty"`
	ExtensionSeconds     int      `json:"extensionSeconds,omitempty"`
}

// transitionDTO matches components.schemas.Transition.
type transitionDTO struct {
	TransitionID string `json:"transitionId"`
	RoundID      string `json:"roundId"`
	PhaseVersion int64  `json:"phaseVersion"`
	AcceptedAt   string `json:"acceptedAt"`
	RoomVersion  int64  `json:"roomVersion"`
}

// ---- submissions ----

// submissionCreateDTO matches SubmissionCreate.
//
// The modeAction is validated by the mode; the server only requires it to be a
// JSON object with a kind . It is deliberately not a free-form blob on
// the write path: the action kind must map to a known slot.
type submissionCreateDTO struct {
	PhaseVersion       int64         `json:"phaseVersion"`
	SubmissionRevision int64         `json:"submissionRevision"`
	ControlGeneration  int64         `json:"controlGeneration"`
	ModeAction         modeActionDTO `json:"modeAction"`
}

// modeActionDTO is the discriminated action envelope.
//
// The whole object is retained in raw so the mode validates every field against
// its own schema; only kind is lifted out for slot mapping. Accepting an
// arbitrary JSON blob as a "mode action" would let a client bypass the per-mode
// contract , so the kind must match a known action before the mode sees it.
type modeActionDTO struct {
	Kind string
	Raw  json.RawMessage
}

// UnmarshalJSON captures the raw object and its kind.
func (m *modeActionDTO) UnmarshalJSON(b []byte) error {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	m.Kind = probe.Kind
	m.Raw = append(m.Raw[:0], b...)
	return nil
}

// MarshalJSON re-emits the raw action object.
func (m modeActionDTO) MarshalJSON() ([]byte, error) {
	if len(m.Raw) == 0 {
		return []byte("null"), nil
	}
	return m.Raw, nil
}

// submissionReceiptDTO matches SubmissionReceipt.
type submissionReceiptDTO struct {
	SubmissionID       string  `json:"submissionId"`
	ReceiptID          string  `json:"receiptId"`
	AcceptedAt         string  `json:"acceptedAt"`
	RoomVersion        int64   `json:"roomVersion"`
	PhaseVersion       int64   `json:"phaseVersion"`
	SubmissionRevision int64   `json:"submissionRevision"`
	Outcome            string  `json:"outcome"`
	Feedback           rawJSON `json:"feedback,omitempty"`
}

// surrenderCreateDTO matches SurrenderCreate.
type surrenderCreateDTO struct {
	PhaseVersion int64 `json:"phaseVersion"`
}

// surrenderDTO matches components.schemas.Surrender.
type surrenderDTO struct {
	SurrenderID string `json:"surrenderId"`
	RoundID     string `json:"roundId"`
	AcceptedAt  string `json:"acceptedAt"`
}

// pauseCreateDTO matches PauseCreate.
type pauseCreateDTO struct {
	PhaseVersion int64 `json:"phaseVersion"`
}

// pauseResponseDTO matches PauseResponse.
type pauseResponseDTO struct {
	Accept bool `json:"accept"`
}

// pauseRequestDTO matches components.schemas.PauseRequest.
type pauseRequestDTO struct {
	RequestID string `json:"requestId"`
	RoundID   string `json:"roundId"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expiresAt"`
}

// rematchCreateDTO matches RematchCreate.
type rematchCreateDTO struct {
	Accept bool `json:"accept"`
}

// rematchRequestDTO matches components.schemas.RematchRequest.
type rematchRequestDTO struct {
	RequestID  string `json:"requestId"`
	Status     string `json:"status"`
	NewRoundID string `json:"newRoundId,omitempty"`
}

// tradeCreateDTO matches TradeCreate.
type tradeCreateDTO struct {
	PhaseVersion          int64  `json:"phaseVersion"`
	RecipientMembershipID string `json:"recipientMembershipId"`
	OfferedInstanceID     string `json:"offeredInstanceId"`
	RequestedInstanceID   string `json:"requestedInstanceId"`
}

// tradeResponseDTO matches TradeResponse.
type tradeResponseDTO struct {
	Accept bool `json:"accept"`
}

// tradeDTO matches components.schemas.Trade.
type tradeDTO struct {
	TradeID               string `json:"tradeId"`
	SenderMembershipID    string `json:"senderMembershipId"`
	RecipientMembershipID string `json:"recipientMembershipId"`
	OfferedInstanceID     string `json:"offeredInstanceId"`
	RequestedInstanceID   string `json:"requestedInstanceId"`
	Status                string `json:"status"`
	ExpiresAt             string `json:"expiresAt"`
}

// ---- matchmaking ----

// ticketCreateDTO matches TicketCreate.
type ticketCreateDTO struct {
	ModeConfig jsonRawMessage `json:"modeConfig"`
}

// ticketDTO matches components.schemas.Ticket.
type ticketDTO struct {
	TicketID   string  `json:"ticketId"`
	Status     string  `json:"status"`
	RoomID     *string `json:"roomId"`
	ExpiresAt  string  `json:"expiresAt"`
	ServerTime string  `json:"serverTime"`
}

// ticketResponseDTO matches TicketResponse.
type ticketResponseDTO struct {
	Accept bool `json:"accept"`
}

// ---- events ----

// eventCreateDTO matches EventCreate.
type eventCreateDTO struct {
	RoomID    string `json:"roomId"`
	Name      string `json:"name"`
	TiePolicy string `json:"tiePolicy"`
}

// eventDTO matches components.schemas.Event.
type eventDTO struct {
	TiePolicy    string   `json:"tiePolicy"`
	EventID      string   `json:"eventId"`
	RoomID       string   `json:"roomId"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	EventVersion int64    `json:"eventVersion"`
	Segments     []string `json:"segments"`
	// SegmentDetails carries the full segment records so a host console can show a
	// programme with names and statuses without a second round trip.
	//
	// segments (ids only) is kept unchanged: existing clients read it, and the
	// contract declares it. This is an additive field.
	SegmentDetails []segmentDTO `json:"segmentDetails,omitempty"`
}

// segmentCreateDTO matches SegmentCreate.
type segmentCreateDTO struct {
	ModeConfig  jsonRawMessage `json:"modeConfig"`
	Label       string         `json:"label"`
	ScoreBudget int            `json:"scoreBudget"`
	RoundCount  int            `json:"roundCount"`
	Competitive bool           `json:"competitive"`
}

// segmentPatchDTO matches SegmentPatch.
type segmentPatchDTO struct {
	ExpectedEventVersion int64          `json:"expectedEventVersion"`
	Label                *string        `json:"label,omitempty"`
	ScoreBudget          *int           `json:"scoreBudget,omitempty"`
	ModeConfig           jsonRawMessage `json:"modeConfig,omitempty"`
}

type eventItemResultDTO struct {
	PublicCode  string `json:"publicCode,omitempty"`
	SubjectID   string `json:"subjectId"`
	DisplayName string `json:"displayName"`
	ScoreUnits  int    `json:"scoreUnits"`
	MaxUnits    int    `json:"maxUnits"`
}

// segmentDTO matches components.schemas.Segment.
type segmentDTO struct {
	Results     []eventItemResultDTO `json:"results"`
	Competitive bool                 `json:"competitive"`
	SegmentID   string               `json:"segmentId"`
	EventID     string               `json:"eventId"`
	Ordinal     int64                `json:"ordinal"`
	Status      string               `json:"status"`
	// Name is the host-authored programme label ("第一项 · 同场之路"). Without it a
	// console can only show the id's tail, which reads like a database key rather
	// than a programme item.
	Name string `json:"name"`
	// RoundID is the round currently bound to this segment, empty until the host
	// starts it. Without it a console cannot tell whether a segment is playable or
	// which round to follow.
	RoundID           string  `json:"roundId"`
	ModeConfig        rawJSON `json:"modeConfig"`
	ScoreBudget       int     `json:"scoreBudget"`
	RoundCount        int     `json:"roundCount"`
	StartedRounds     int     `json:"startedRounds"`
	CompletedRounds   int     `json:"completedRounds"`
	CurrentRoundPhase string  `json:"currentRoundPhase"`
}

// standingDTO matches components.schemas.Standing.
type standingDTO struct {
	PublicCode  string            `json:"publicCode,omitempty"`
	SubjectID   string            `json:"subjectId"`
	DisplayName string            `json:"displayName"`
	Rank        int               `json:"rank"`
	ScoreUnits  int               `json:"scoreUnits"`
	Segments    []segmentScoreDTO `json:"segments"`
}

// segmentScoreDTO is one segment's contribution to a standing.
type segmentScoreDTO struct {
	SegmentID  string `json:"segmentId"`
	ScoreUnits int    `json:"scoreUnits"`
	MaxUnits   int    `json:"maxUnits"`
}

// standingsDTO matches components.schemas.Standings.
type standingsDTO struct {
	EventID       string        `json:"eventId"`
	EventVersion  int64         `json:"eventVersion"`
	Items         []standingDTO `json:"items"`
	CorrectionIDs []string      `json:"correctionIds"`
}

// correctionCreateDTO matches CorrectionCreate.
type correctionCreateDTO struct {
	SegmentID            string `json:"segmentId"`
	SubjectID            string `json:"subjectId"`
	NewScoreUnits        int    `json:"newScoreUnits"`
	Reason               string `json:"reason"`
	ExpectedEventVersion int64  `json:"expectedEventVersion"`
}

// correctionDTO matches components.schemas.Correction.
type correctionDTO struct {
	CorrectionID string `json:"correctionId"`
	EventVersion int64  `json:"eventVersion"`
	AcceptedAt   string `json:"acceptedAt"`
}

// eventTransitionCreateDTO matches EventTransitionCreate.
type eventTransitionCreateDTO struct {
	Target               string `json:"target"`
	ExpectedEventVersion int64  `json:"expectedEventVersion"`
	Reason               string `json:"reason,omitempty"`
}

// ---- grants ----

// grantDTO matches components.schemas.Grant.
type grantDTO struct {
	GrantCode string `json:"grantCode"`
	ExpiresAt string `json:"expiresAt"`
	SingleUse bool   `json:"singleUse"`
}

// grantRedeemDTO matches GrantRedeem.
type grantRedeemDTO struct {
	GrantCode string `json:"grantCode"`
}

type displayGrantRedeemDTO struct {
	GrantCode string `json:"grantCode"`
	EventID   string `json:"eventId,omitempty"`
}

// recoveryCreateDTO matches RecoveryCreate.
type recoveryCreateDTO struct {
	Reason string `json:"reason"`
}

// displaySessionDTO matches components.schemas.DisplaySession.
type displaySessionDTO struct {
	EventID   string `json:"eventId"`
	RoomID    string `json:"roomId"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expiresAt"`
}

// ---- invitations ----

// inviteResolutionCreateDTO matches InviteResolutionCreate.
type inviteResolutionCreateDTO struct {
	InviteCode string `json:"inviteCode"`
}

// inviteResolutionDTO matches components.schemas.InviteResolution.
type inviteResolutionDTO struct {
	RoomID   string `json:"roomId"`
	Joinable bool   `json:"joinable"`
}

// ---- notifications ----

// eventNotificationDTO matches components.schemas.EventNotification.
type eventNotificationDTO struct {
	SchemaVersion string         `json:"schemaVersion"`
	EventID       string         `json:"eventId"`
	Type          string         `json:"type"`
	RoomID        string         `json:"roomId"`
	RoomVersion   int64          `json:"roomVersion"`
	ServerEpoch   string         `json:"serverEpoch"`
	ServerTime    string         `json:"serverTime"`
	Cursor        string         `json:"cursor"`
	Resource      string         `json:"resource,omitempty"`
	RoundID       string         `json:"roundId,omitempty"`
	PhaseVersion  *int64         `json:"phaseVersion,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	OnlineCount   *int           `json:"onlineCount,omitempty"`
	HeartbeatMS   *int           `json:"heartbeatIntervalMs,omitempty"`
	Payload       jsonRawMessage `json:"-"`
}

// notificationListDTO matches NotificationList.
type notificationListDTO struct {
	Items          []eventNotificationDTO `json:"items"`
	NextCursor     *string                `json:"nextCursor"`
	ResyncRequired bool                   `json:"resyncRequired"`
}

// ---- health ----

// healthDTO matches components.schemas.Health.
type healthDTO struct {
	Status       string `json:"status"`
	BuildVersion string `json:"buildVersion"`
}

// ---- round view ----

// publicRoundViewDTO matches components.schemas.PublicRoundView.
//
// results, revealedAnswerCharacterId and evidence are pointers so they are
// omitted entirely before reveal, matching the contract's conditional schema.
type publicRoundViewDTO struct {
	ViewerRole  string          `json:"viewerRole"`
	Round       roundSummaryDTO `json:"round"`
	PublicState rawJSON         `json:"publicState"`
	// SubjectNames maps subjectId to a player-facing label. A console needs it to
	// render "红队" instead of "tem_2cc002ada4..."; the ids themselves stay opaque.
	SubjectNames              map[string]string   `json:"subjectNames,omitempty"`
	Results                   *[]resultDTO        `json:"results,omitempty"`
	RevealedAnswerCharacterID string              `json:"revealedAnswerCharacterId,omitempty"`
	Evidence                  *[]evidenceDTO      `json:"evidence,omitempty"`
	ServerTime                string              `json:"serverTime"`
	RoomVersion               int64               `json:"roomVersion"`
	TeamControllers           []teamControllerDTO `json:"teamControllers,omitempty"`
	CanSubmit                 bool                `json:"canSubmit"`
	ControlGeneration         int64               `json:"controlGeneration"`
	SubmissionRevision        int64               `json:"submissionRevision"`
	SubmissionRevisionsBySlot map[string]int64    `json:"submissionRevisionsBySlot,omitempty"`
}

// playerRoundViewDTO matches components.schemas.PlayerRoundView.
type playerRoundViewDTO struct {
	ViewerRole  string          `json:"viewerRole"`
	Round       roundSummaryDTO `json:"round"`
	PublicState rawJSON         `json:"publicState"`
	// SubjectNames is the same public id-to-label map as the spectator view.
	SubjectNames              map[string]string `json:"subjectNames,omitempty"`
	Results                   *[]resultDTO      `json:"results,omitempty"`
	RevealedAnswerCharacterID string            `json:"revealedAnswerCharacterId,omitempty"`
	Evidence                  *[]evidenceDTO    `json:"evidence,omitempty"`
	ServerTime                string            `json:"serverTime"`
	RoomVersion               int64             `json:"roomVersion"`
	PrivateState              rawJSON           `json:"privateState"`
	ControlGeneration         int64             `json:"controlGeneration"`
	SubmissionRevision        int64             `json:"submissionRevision"`
	// SubmissionRevisionsBySlot carries the live revision per submit slot.
	//
	// A revision is per (round, subject, slot), so a mode with two slots cannot be
	// served by the single submissionRevision above. Absent slots mean "never
	// submitted there", whose first submission must use 0. Omitted entirely when the
	// caller has no competing subject or the round has no heads yet, so a client can
	// keep using submissionRevision for single-slot modes.
	SubmissionRevisionsBySlot map[string]int64    `json:"submissionRevisionsBySlot,omitempty"`
	CanSubmit                 bool                `json:"canSubmit"`
	TeamControllers           []teamControllerDTO `json:"teamControllers,omitempty"`
}

type teamControllerDTO struct {
	TeamID       string `json:"teamId"`
	MembershipID string `json:"membershipId"`
	DisplayName  string `json:"displayName"`
	Generation   int64  `json:"generation"`
}

func toTeamControllerDTOs(items []room.TeamController) []teamControllerDTO {
	var out []teamControllerDTO
	for _, item := range items {
		out = append(out, teamControllerDTO{item.TeamID, item.MembershipID, item.DisplayName, item.Generation})
	}
	return out
}

// resultDTO matches components.schemas.Result.
type resultDTO struct {
	SubjectID   string           `json:"subjectId"`
	SubjectType string           `json:"subjectType"`
	ScoreUnits  int              `json:"scoreUnits"`
	MaxUnits    int              `json:"maxUnits"`
	Rank        int              `json:"rank,omitempty"`
	Outcome     string           `json:"outcome"`
	Details     []scoreDetailDTO `json:"details"`
}

// scoreDetailDTO matches components.schemas.ScoreDetail.
type scoreDetailDTO struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	EarnedUnits int    `json:"earnedUnits"`
	MaxUnits    int    `json:"maxUnits"`
	Explanation string `json:"explanation"`
}

// evidenceDTO matches components.schemas.Evidence.
type evidenceDTO struct {
	EvidenceID string `json:"evidenceId"`
	SourceURL  string `json:"sourceUrl"`
	Label      string `json:"label"`
	RaceID     string `json:"raceId,omitempty"`
}

// pathNeighborsDTO matches components.schemas.PathNeighbors.
type pathNeighborsDTO struct {
	RoundID     string            `json:"roundId"`
	CharacterID string            `json:"characterId"`
	Neighbors   []pathNeighborDTO `json:"neighbors"`
}

// pathNeighborDTO is one neighbour plus its evidence.
type pathNeighborDTO struct {
	Character publicCardDTO `json:"character"`
	Evidence  []evidenceDTO `json:"evidence"`
}

// publicCardDTO matches components.schemas.PublicCard.
type publicCardDTO struct {
	CardID      string        `json:"cardId"`
	CharacterID string        `json:"characterId,omitempty"`
	DisplayName string        `json:"displayName"`
	AssetID     string        `json:"assetId,omitempty"`
	Aptitudes   *aptitudesDTO `json:"aptitudes,omitempty"`
	Cost        int           `json:"cost,omitempty"`
}
