package ws

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// flexString accepts both JSON strings and numbers, converting numbers to strings.
// This handles C# enum fields that may be serialized as integers or strings
// depending on whether JsonStringEnumConverter is applied.
type flexString struct {
	Value *string
}

func (f *flexString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		f.Value = &s
		return nil
	}
	// Fall back to number → string
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		s = n.String()
		f.Value = &s
		return nil
	}
	return fmt.Errorf("flexString: cannot unmarshal %s", strconv.Quote(string(data)))
}

func (f flexString) MarshalJSON() ([]byte, error) {
	if f.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*f.Value)
}

// ========================================================================
// Battle server input structs (C# ASP.NET camelCase serialization)
// ========================================================================

type battleGameState struct {
	GameID        string             `json:"gameID"`
	CurrentTurn   int64              `json:"currentTurn"`
	CurrentPhase  string             `json:"currentPhase"`
	ActivePlayer  int64              `json:"activePlayer"`
	IsMyTurn      bool               `json:"isMyTurn"`
	TurnStartedAt string             `json:"turnStartedAt"`
	MyView        battlePlayerView   `json:"myView"`
	OppView       battleOpponentView `json:"oppView"`
}

type battlePlayerView struct {
	PlayerNum        int64                    `json:"playerNum"`
	Budget           int64                    `json:"budget"`
	InsightPool      int64                    `json:"insightPool"`
	TimeBank         int64                    `json:"timeBank"`
	Field            battleField              `json:"field"`
	Hand             []battleHandCard         `json:"hand"`
	RepoCount        int                      `json:"repoCount"`
	TrashCount       int                      `json:"trashCount"`
	Trash            []battleHandCard         `json:"trash"`
	AvailableActions []battleAvailableAction  `json:"availableActions"`
}

type battleOpponentView struct {
	PlayerNum  int64                `json:"playerNum"`
	Budget     int64                `json:"budget"`
	InsightPool int64               `json:"insightPool"`
	TimeBank   int64                `json:"timeBank"`
	Field      battleOpponentField  `json:"field"`
	HandCount  int                  `json:"handCount"`
	RepoCount  int                  `json:"repoCount"`
	TrashCount int                  `json:"trashCount"`
	Trash      []battleHandCard     `json:"trash"`
}

type battleField struct {
	Frontend []*battleResourceInstance `json:"frontend"`
	Backend  []*battleResourceInstance `json:"backend"`
	Support  []*battleSupportInstance  `json:"support"`
}

type battleOpponentField struct {
	Frontend []*battleResourceInstance      `json:"frontend"`
	Backend  []*battleResourceInstance      `json:"backend"`
	Support  []*battleHiddenSupportInstance `json:"support"`
}

type battleResourceInstance struct {
	InstanceID           string                   `json:"instanceID"`
	CardID               string                   `json:"cardID"`
	ArtNo                int64                    `json:"artNo"`
	Rank                 flexString               `json:"rank"`
	InstanceFamily       flexString               `json:"instanceFamily"`
	FaceUp               bool                     `json:"faceUp"`
	DeployingTurnsLeft   int64                    `json:"deployingTurnsLeft"`
	CurrentAV            int64                    `json:"currentAV"`
	MaxAV                int64                    `json:"maxAV"`
	CurrentTP            *int64                   `json:"currentTP"`
	MaxTP                *int64                   `json:"maxTP"`
	CurrentYield         *int64                   `json:"currentYield"`
	MaxYield             *int64                   `json:"maxYield"`
	Damage               int64                    `json:"damage"`
	Attachments          []battleAttachmentRef    `json:"attachments"`
	TemporaryEffects     []battleTemporaryEffect  `json:"temporaryEffects"`
	MonetizedAmount      int64                    `json:"monetizedAmount"`
	HasAttacked          bool                     `json:"hasAttacked"`
	EffectUsedThisTurn   bool                     `json:"effectUsedThisTurn"`
	ScaleChangedThisTurn bool                     `json:"scaleChangedThisTurn"`
	DeployedOnTurn       int64                    `json:"deployedOnTurn"`
	DeployOrder          int64                    `json:"deployOrder"`
	MigratingFrom        *string                  `json:"migratingFrom"`
	MigrationTarget      *string                  `json:"migrationTarget"`
	MigratingOnTurn      int64                    `json:"migratingOnTurn"`
	ElasticBonus         int64                    `json:"elasticBonus"`
}

type battleSupportInstance struct {
	InstanceID         string `json:"instanceID"`
	CardID             string `json:"cardID"`
	ArtNo              int64  `json:"artNo"`
	FaceUp             bool   `json:"faceUp"`
	DeployingTurnsLeft int64  `json:"deployingTurnsLeft"`
	DeployOrder        int64  `json:"deployOrder"`
	EffectUsedThisTurn bool   `json:"effectUsedThisTurn"`
}

type battleHiddenSupportInstance struct {
	InstanceID string `json:"instanceID"`
	CardID     string `json:"cardID"`
	FaceUp     bool   `json:"faceUp"`
}

type battleHandCard struct {
	InstanceID string `json:"instanceID"`
	CardID     string `json:"cardID"`
	ArtNo      int64  `json:"artNo"`
}

type battleAttachmentRef struct {
	InstanceID string `json:"instanceID"`
	CardID     string `json:"cardID"`
	ArtNo      int64  `json:"artNo"`
}

type battleTemporaryEffect struct {
	EffectType string `json:"effectType"`
	Value      int64  `json:"value"`
	Duration   string `json:"duration"`
	SourceID   string `json:"sourceID"`
}

type battleAvailableAction struct {
	Type              string   `json:"type"`
	HandInstanceID    *string  `json:"handInstanceID"`
	CardID            string   `json:"cardID"`
	ValidZones        []string `json:"validZones"`
	SourceInstanceID  *string  `json:"sourceInstanceID"`
	ValidTargets      []string `json:"validTargets"`
	TargetRank        *string  `json:"targetRank"`
	InstanceFamily    *string  `json:"instanceFamily"`
	NeedsFamily       bool     `json:"needsFamily"`
	RemainingCapacity int64    `json:"remainingCapacity"`
	EffectTargetType  *string  `json:"effectTargetType"`
	RequiredCount     int      `json:"requiredCount"`
	ChoiceOptions     []string `json:"choiceOptions"`
}

// ========================================================================
// Client output structs (TypeScript client expected format)
// ========================================================================

type clientGameState struct {
	GameID        string             `json:"gameId"`
	CurrentTurn   int64              `json:"currentTurn"`
	CurrentPhase  string             `json:"currentPhase"`
	ActivePlayer  int64              `json:"activePlayer"`
	IsMyTurn      bool               `json:"isMyTurn"`
	TurnStartedAt string             `json:"turnStartedAt"`
	My            clientPlayerView   `json:"my"`
	Opponent      clientOpponentView `json:"opponent"`
}

type clientPlayerView struct {
	PlayerNum        int64                   `json:"playerNum"`
	Budget           int64                   `json:"budget"`
	InsightPool      int64                   `json:"insightPool"`
	TimeBank         int64                   `json:"timeBank"`
	Field            clientField             `json:"field"`
	Hand             []clientHandCard        `json:"hand"`
	RepoCount        int                     `json:"repoCount"`
	TrashCount       int                     `json:"trashCount"`
	Trash            []clientHandCard        `json:"trash"`
	AvailableActions []clientAvailableAction `json:"available_actions,omitempty"`
}

type clientOpponentView struct {
	PlayerNum   int64               `json:"playerNum"`
	Budget      int64               `json:"budget"`
	InsightPool int64               `json:"insightPool"`
	TimeBank    int64               `json:"timeBank"`
	Field       clientOpponentField `json:"field"`
	HandCount   int                 `json:"handCount"`
	RepoCount   int                 `json:"repoCount"`
	TrashCount  int                 `json:"trashCount"`
	Trash       []clientHandCard    `json:"trash"`
}

type clientField struct {
	Frontend []*clientResourceInstance `json:"frontend"`
	Backend  []*clientResourceInstance `json:"backend"`
	Support  []*clientSupportInstance  `json:"support"`
}

type clientOpponentField struct {
	Frontend []*clientResourceInstance      `json:"frontend"`
	Backend  []*clientResourceInstance      `json:"backend"`
	Support  []*clientHiddenSupportInstance `json:"support"`
}

type clientResourceInstance struct {
	InstanceID         string                   `json:"instanceId"`
	CardID             string                   `json:"cardId"`
	Rank               *string                  `json:"rank"`
	InstanceFamily     *string                  `json:"instanceFamily"`
	CurrentAV          int64                    `json:"currentAV"`
	MaxAV              int64                    `json:"maxAV"`
	CurrentTP          *int64                   `json:"currentTP"`
	MaxTP              *int64                   `json:"maxTP"`
	CurrentYield       *int64                   `json:"currentYield"`
	MaxYield           *int64                   `json:"maxYield"`
	Damage             int64                    `json:"damage"`
	Attachments        []clientAttachmentRef    `json:"attachments"`
	TemporaryEffects   []clientTemporaryEffect  `json:"temporaryEffects"`
	MonetizedAmount    int64                    `json:"monetizedAmount"`
	HasAttacked        bool                     `json:"hasAttacked"`
	EffectUsedThisTurn bool                     `json:"effectUsedThisTurn"`
	DeployedOnTurn     int64                    `json:"deployedOnTurn"`
	DeployOrder        int64                    `json:"deployOrder"`
	FaceDown           bool                     `json:"faceDown"`
	ElasticBonus       int64                    `json:"elasticBonus"`
}

type clientSupportInstance struct {
	InstanceID         string `json:"instanceId"`
	CardID             string `json:"cardId"`
	FaceDown           bool   `json:"faceDown"`
	DeployOrder        int64  `json:"deployOrder"`
	EffectUsedThisTurn bool   `json:"effectUsedThisTurn"`
}

type clientHiddenSupportInstance struct {
	InstanceID string `json:"instanceId"`
	CardID     string `json:"cardId"`
	FaceDown   bool   `json:"faceDown"`
}

type clientHandCard struct {
	InstanceID string `json:"instanceId"`
	CardID     string `json:"cardId"`
}

type clientAttachmentRef struct {
	InstanceID string `json:"instanceId"`
	CardID     string `json:"cardId"`
}

type clientTemporaryEffect struct {
	EffectType string `json:"effectType"`
	Value      int64  `json:"value"`
	Duration   string `json:"duration"`
	SourceID   string `json:"sourceId"`
}

type clientAvailableAction struct {
	Type              string   `json:"type"`
	HandInstanceID    *string  `json:"hand_instance_id,omitempty"`
	CardID            string   `json:"card_id,omitempty"`
	ValidZones        []string `json:"valid_zones,omitempty"`
	SourceInstanceID  *string  `json:"source_instance_id,omitempty"`
	ValidTargets      []string `json:"valid_targets,omitempty"`
	TargetRank        *string  `json:"target_rank,omitempty"`
	InstanceFamily    *string  `json:"instance_family,omitempty"`
	RemainingCapacity int64    `json:"remaining_capacity,omitempty"`
	EffectTargetType  *string  `json:"effect_target_type,omitempty"`
	ChoiceOptions     []string `json:"choice_options,omitempty"`
}

// ========================================================================
// Transform function
// ========================================================================

// transformGameState converts battle server JSON into client-expected JSON.
func transformGameState(raw json.RawMessage) (json.RawMessage, error) {
	var b battleGameState
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("unmarshal battle state: %w", err)
	}

	c := clientGameState{
		GameID:        b.GameID,
		CurrentTurn:   b.CurrentTurn,
		CurrentPhase:  b.CurrentPhase,
		ActivePlayer:  b.ActivePlayer,
		IsMyTurn:      b.IsMyTurn,
		TurnStartedAt: b.TurnStartedAt,
		My:            transformPlayerView(b.MyView),
		Opponent:      transformOpponentView(b.OppView),
	}

	out, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal client state: %w", err)
	}
	return out, nil
}

func transformPlayerView(b battlePlayerView) clientPlayerView {
	c := clientPlayerView{
		PlayerNum:   b.PlayerNum,
		Budget:      b.Budget,
		InsightPool: b.InsightPool,
		TimeBank:    b.TimeBank,
		Field:       transformField(b.Field),
		Hand:        transformHandCards(b.Hand),
		RepoCount:   b.RepoCount,
		TrashCount:  b.TrashCount,
		Trash:       transformHandCards(b.Trash),
	}
	if len(b.AvailableActions) > 0 {
		c.AvailableActions = transformAvailableActions(b.AvailableActions)
	}
	return c
}

func transformOpponentView(b battleOpponentView) clientOpponentView {
	return clientOpponentView{
		PlayerNum:   b.PlayerNum,
		Budget:      b.Budget,
		InsightPool: b.InsightPool,
		TimeBank:    b.TimeBank,
		Field:       transformOpponentField(b.Field),
		HandCount:   b.HandCount,
		RepoCount:   b.RepoCount,
		TrashCount:  b.TrashCount,
		Trash:       transformHandCards(b.Trash),
	}
}

func transformField(b battleField) clientField {
	return clientField{
		Frontend: transformResourceSlots(b.Frontend),
		Backend:  transformResourceSlots(b.Backend),
		Support:  transformSupportSlots(b.Support),
	}
}

func transformOpponentField(b battleOpponentField) clientOpponentField {
	return clientOpponentField{
		Frontend: transformResourceSlots(b.Frontend),
		Backend:  transformResourceSlots(b.Backend),
		Support:  transformHiddenSupportSlots(b.Support),
	}
}

func transformResourceSlots(slots []*battleResourceInstance) []*clientResourceInstance {
	out := make([]*clientResourceInstance, len(slots))
	for i, s := range slots {
		if s != nil {
			out[i] = transformResourceInstance(s)
		}
	}
	return out
}

func transformResourceInstance(b *battleResourceInstance) *clientResourceInstance {
	return &clientResourceInstance{
		InstanceID:         b.InstanceID,
		CardID:             b.CardID,
		Rank:               b.Rank.Value,
		InstanceFamily:     b.InstanceFamily.Value,
		CurrentAV:          b.CurrentAV,
		MaxAV:              b.MaxAV,
		CurrentTP:          b.CurrentTP,
		MaxTP:              b.MaxTP,
		CurrentYield:       b.CurrentYield,
		MaxYield:           b.MaxYield,
		Damage:             b.Damage,
		Attachments:        transformAttachments(b.Attachments),
		TemporaryEffects:   transformTemporaryEffects(b.TemporaryEffects),
		MonetizedAmount:    b.MonetizedAmount,
		HasAttacked:        b.HasAttacked,
		EffectUsedThisTurn: b.EffectUsedThisTurn,
		DeployedOnTurn:     b.DeployedOnTurn,
		DeployOrder:        b.DeployOrder,
		FaceDown:           !b.FaceUp,
		ElasticBonus:       b.ElasticBonus,
	}
}

func transformSupportSlots(slots []*battleSupportInstance) []*clientSupportInstance {
	out := make([]*clientSupportInstance, len(slots))
	for i, s := range slots {
		if s != nil {
			out[i] = &clientSupportInstance{
				InstanceID:         s.InstanceID,
				CardID:             s.CardID,
				FaceDown:           !s.FaceUp,
				DeployOrder:        s.DeployOrder,
				EffectUsedThisTurn: s.EffectUsedThisTurn,
			}
		}
	}
	return out
}

func transformHiddenSupportSlots(slots []*battleHiddenSupportInstance) []*clientHiddenSupportInstance {
	out := make([]*clientHiddenSupportInstance, len(slots))
	for i, s := range slots {
		if s != nil {
			out[i] = &clientHiddenSupportInstance{
				InstanceID: s.InstanceID,
				CardID:     s.CardID,
				FaceDown:   !s.FaceUp,
			}
		}
	}
	return out
}

func transformHandCards(cards []battleHandCard) []clientHandCard {
	out := make([]clientHandCard, len(cards))
	for i, c := range cards {
		out[i] = clientHandCard{
			InstanceID: c.InstanceID,
			CardID:     c.CardID,
		}
	}
	return out
}

func transformAttachments(refs []battleAttachmentRef) []clientAttachmentRef {
	out := make([]clientAttachmentRef, len(refs))
	for i, r := range refs {
		out[i] = clientAttachmentRef{
			InstanceID: r.InstanceID,
			CardID:     r.CardID,
		}
	}
	return out
}

func transformTemporaryEffects(effs []battleTemporaryEffect) []clientTemporaryEffect {
	out := make([]clientTemporaryEffect, len(effs))
	for i, e := range effs {
		out[i] = clientTemporaryEffect(e)
	}
	return out
}

func transformAvailableActions(actions []battleAvailableAction) []clientAvailableAction {
	out := make([]clientAvailableAction, len(actions))
	for i, a := range actions {
		out[i] = clientAvailableAction{
			Type:              a.Type,
			HandInstanceID:    a.HandInstanceID,
			CardID:            a.CardID,
			ValidZones:        a.ValidZones,
			SourceInstanceID:  a.SourceInstanceID,
			ValidTargets:      a.ValidTargets,
			TargetRank:        a.TargetRank,
			InstanceFamily:    a.InstanceFamily,
			RemainingCapacity: a.RemainingCapacity,
			EffectTargetType:  a.EffectTargetType,
			ChoiceOptions:     a.ChoiceOptions,
		}
	}
	return out
}
