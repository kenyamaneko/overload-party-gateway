package ws

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers
func ptr[T any](v T) *T { return &v }

func buildMinimalBattleState() battleGameState {
	return battleGameState{
		GameID:       "game-abc-123",
		CurrentTurn:  3,
		CurrentPhase: "main",
		ActivePlayer: 1,
		IsMyTurn:     true,
		MyView: battlePlayerView{
			PlayerNum:   1,
			Budget:      5,
			InsightPool: 2,
			TimeBank:    30,
			Field: battleField{
				Frontend: []*battleResourceInstance{
					{
						InstanceID:     "inst-f1",
						CardID:         100,
						ArtNo:          1,
						Rank:           ptr("S"),
						InstanceFamily: ptr("familyA"),
						FaceUp:         true,
						CurrentAV:      3,
						MaxAV:          5,
						CurrentTP:      ptr(int64(2)),
						MaxTP:          ptr(int64(4)),
						CurrentYield:   ptr(int64(1)),
						MaxYield:       ptr(int64(3)),
						Damage:         1,
						Attachments: []battleAttachmentRef{
							{InstanceID: "att-1", CardID: 200, ArtNo: 2},
						},
						TemporaryEffects: []battleTemporaryEffect{
							{EffectType: "buff", Value: 1, Duration: "endOfTurn", SourceID: "src-1"},
						},
						MonetizedAmount:      0,
						HasAttacked:          false,
						EffectUsedThisTurn:   true,
						ScaleChangedThisTurn: false,
						DeployedOnTurn:       1,
						DeployOrder:          0,
						MigratingFrom:        nil,
						MigrationTarget:      nil,
						MigratingOnTurn:      0,
						ElasticBonus:         2,
					},
				},
				Backend: []*battleResourceInstance{},
				Support: []*battleSupportInstance{
					{
						InstanceID:         "inst-s1",
						CardID:             300,
						ArtNo:              3,
						FaceUp:             false,
						DeployingTurnsLeft: 0,
						DeployOrder:        1,
						EffectUsedThisTurn: false,
					},
				},
			},
			Hand: []battleHandCard{
				{InstanceID: "hand-1", CardID: 400, ArtNo: 4},
				{InstanceID: "hand-2", CardID: 401, ArtNo: 5},
			},
			RepoCount:  10,
			TrashCount: 2,
			Trash:      []battleHandCard{{InstanceID: "trash-1", CardID: 500, ArtNo: 6}},
			AvailableActions: []battleAvailableAction{
				{
					Type:              "deploy",
					HandInstanceID:    ptr("hand-1"),
					CardID:            400,
					ValidZones:        []string{"frontend", "backend"},
					SourceInstanceID:  nil,
					ValidTargets:      nil,
					TargetRank:        ptr("A"),
					InstanceFamily:    ptr("familyB"),
					NeedsFamily:       true,
					RemainingCapacity: 2,
					EffectTargetType:  ptr("resource"),
					RequiredCount:     1,
					ChoiceOptions:     []string{"opt1", "opt2"},
				},
			},
		},
		OppView: battleOpponentView{
			PlayerNum:   2,
			Budget:      3,
			InsightPool: 1,
			TimeBank:    25,
			Field: battleOpponentField{
				Frontend: []*battleResourceInstance{
					{
						InstanceID: "opp-f1",
						CardID:     150,
						ArtNo:      1,
						FaceUp:     false,
						CurrentAV:  2,
						MaxAV:      4,
						Attachments:     []battleAttachmentRef{},
						TemporaryEffects: []battleTemporaryEffect{},
					},
				},
				Backend: []*battleResourceInstance{},
				Support: []*battleHiddenSupportInstance{
					{InstanceID: "opp-s1", CardID: ptr(int64(350)), FaceUp: true},
					{InstanceID: "opp-s2", CardID: nil, FaceUp: false},
				},
			},
			HandCount:  4,
			RepoCount:  8,
			TrashCount: 1,
			Trash:      []battleHandCard{{InstanceID: "opp-trash-1", CardID: 550, ArtNo: 7}},
		},
	}
}

// --------------------------------------------------------------------------
// 1. Full round-trip
// --------------------------------------------------------------------------

func TestTransformGameState_FullRoundTrip(t *testing.T) {
	b := buildMinimalBattleState()
	raw, err := json.Marshal(b)
	require.NoError(t, err)

	out, err := transformGameState(raw)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))

	// Top-level field renames
	assert.Equal(t, "game-abc-123", m["gameId"])
	assert.Nil(t, m["gameID"], "old key gameID should not appear")
	assert.Equal(t, float64(3), m["currentTurn"])
	assert.Equal(t, "main", m["currentPhase"])
	assert.Equal(t, float64(1), m["activePlayer"])
	assert.Equal(t, true, m["isMyTurn"])

	// myView → my, oppView → opponent
	assert.NotNil(t, m["my"], "my field must exist")
	assert.Nil(t, m["myView"], "myView should not appear")
	assert.NotNil(t, m["opponent"], "opponent field must exist")
	assert.Nil(t, m["oppView"], "oppView should not appear")

	// Verify my.playerNum
	my := m["my"].(map[string]interface{})
	assert.Equal(t, float64(1), my["playerNum"])
	assert.Equal(t, float64(5), my["budget"])

	// Verify hand cards drop artNo
	hand := my["hand"].([]interface{})
	require.Len(t, hand, 2)
	h0 := hand[0].(map[string]interface{})
	assert.Equal(t, "hand-1", h0["instanceId"])
	assert.Equal(t, float64(400), h0["cardId"])
	assert.Nil(t, h0["artNo"], "artNo should be dropped from hand cards")

	// Verify field frontend resource instance
	field := my["field"].(map[string]interface{})
	frontend := field["frontend"].([]interface{})
	require.Len(t, frontend, 1)
	res := frontend[0].(map[string]interface{})
	assert.Equal(t, "inst-f1", res["instanceId"])
	assert.Nil(t, res["instanceID"], "old key instanceID should not appear")
	assert.Equal(t, float64(100), res["cardId"])
	assert.Nil(t, res["cardID"], "old key cardID should not appear")
	assert.Nil(t, res["artNo"], "artNo should be dropped from resource instances")
	// faceUp=true → faceDown=false
	assert.Equal(t, false, res["faceDown"])
	assert.Nil(t, res["faceUp"], "faceUp should not appear in client output")

	// Verify attachment drops artNo
	attachments := res["attachments"].([]interface{})
	require.Len(t, attachments, 1)
	att := attachments[0].(map[string]interface{})
	assert.Equal(t, "att-1", att["instanceId"])
	assert.Equal(t, float64(200), att["cardId"])
	assert.Nil(t, att["artNo"], "artNo should be dropped from attachments")

	// Verify temporary effect sourceID → sourceId
	effects := res["temporaryEffects"].([]interface{})
	require.Len(t, effects, 1)
	eff := effects[0].(map[string]interface{})
	assert.Equal(t, "src-1", eff["sourceId"])
	assert.Nil(t, eff["sourceID"], "old key sourceID should not appear")

	// Verify support instance
	support := field["support"].([]interface{})
	require.Len(t, support, 1)
	sup := support[0].(map[string]interface{})
	assert.Equal(t, "inst-s1", sup["instanceId"])
	assert.Equal(t, float64(300), sup["cardId"])
	// faceUp=false → faceDown=true
	assert.Equal(t, true, sup["faceDown"])
	assert.Nil(t, sup["artNo"], "artNo should be dropped from support instances")

	// Verify opponent view
	opp := m["opponent"].(map[string]interface{})
	assert.Equal(t, float64(2), opp["playerNum"])
	assert.Equal(t, float64(4), opp["handCount"])

	// Verify opponent hidden support
	oppField := opp["field"].(map[string]interface{})
	oppSupport := oppField["support"].([]interface{})
	require.Len(t, oppSupport, 2)
	os0 := oppSupport[0].(map[string]interface{})
	assert.Equal(t, "opp-s1", os0["instanceId"])
	assert.Equal(t, float64(350), os0["cardId"])
	assert.Equal(t, false, os0["faceDown"]) // faceUp=true → faceDown=false

	os1 := oppSupport[1].(map[string]interface{})
	assert.Equal(t, "opp-s2", os1["instanceId"])
	assert.Equal(t, float64(0), os1["cardId"]) // nil cardID → 0
	assert.Equal(t, true, os1["faceDown"])      // faceUp=false → faceDown=true

	// Verify available_actions in client output
	actions := my["available_actions"].([]interface{})
	require.Len(t, actions, 1)
	act := actions[0].(map[string]interface{})
	assert.Equal(t, "deploy", act["type"])
	assert.Equal(t, "hand-1", act["hand_instance_id"])
	assert.Equal(t, float64(400), act["card_id"])
	zones := act["valid_zones"].([]interface{})
	assert.Equal(t, []interface{}{"frontend", "backend"}, zones)
}

// --------------------------------------------------------------------------
// 2. Invalid JSON
// --------------------------------------------------------------------------

func TestTransformGameState_InvalidJSON(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
	}{
		{"empty bytes", json.RawMessage{}},
		{"garbage", json.RawMessage(`{not json}`)},
		{"truncated", json.RawMessage(`{"gameID": "x"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := transformGameState(tc.input)
			assert.Error(t, err)
		})
	}
}

// --------------------------------------------------------------------------
// 3. Nil slots in field arrays
// --------------------------------------------------------------------------

func TestTransformField_NilSlots(t *testing.T) {
	field := battleField{
		Frontend: []*battleResourceInstance{
			nil,
			{
				InstanceID:       "f1",
				CardID:           10,
				FaceUp:           true,
				Attachments:      []battleAttachmentRef{},
				TemporaryEffects: []battleTemporaryEffect{},
			},
			nil,
		},
		Backend: []*battleResourceInstance{nil, nil},
		Support: []*battleSupportInstance{nil, {
			InstanceID: "s1",
			CardID:     20,
			FaceUp:     false,
		}},
	}

	c := transformField(field)

	// Frontend: nil preserved, non-nil transformed
	require.Len(t, c.Frontend, 3)
	assert.Nil(t, c.Frontend[0])
	require.NotNil(t, c.Frontend[1])
	assert.Equal(t, "f1", c.Frontend[1].InstanceID)
	assert.Equal(t, false, c.Frontend[1].FaceDown) // faceUp=true
	assert.Nil(t, c.Frontend[2])

	// Backend: all nil
	require.Len(t, c.Backend, 2)
	assert.Nil(t, c.Backend[0])
	assert.Nil(t, c.Backend[1])

	// Support: nil + non-nil
	require.Len(t, c.Support, 2)
	assert.Nil(t, c.Support[0])
	require.NotNil(t, c.Support[1])
	assert.Equal(t, "s1", c.Support[1].InstanceID)
	assert.Equal(t, true, c.Support[1].FaceDown) // faceUp=false
}

// --------------------------------------------------------------------------
// 4. Hidden support slots — cardID pointer nil vs non-nil
// --------------------------------------------------------------------------

func TestTransformHiddenSupportSlots(t *testing.T) {
	slots := []*battleHiddenSupportInstance{
		{InstanceID: "hs-1", CardID: ptr(int64(42)), FaceUp: true},
		{InstanceID: "hs-2", CardID: nil, FaceUp: false},
		nil,
	}

	out := transformHiddenSupportSlots(slots)

	require.Len(t, out, 3)

	// Non-nil cardID
	require.NotNil(t, out[0])
	assert.Equal(t, "hs-1", out[0].InstanceID)
	assert.Equal(t, int64(42), out[0].CardID)
	assert.Equal(t, false, out[0].FaceDown) // faceUp=true

	// Nil cardID → 0
	require.NotNil(t, out[1])
	assert.Equal(t, "hs-2", out[1].InstanceID)
	assert.Equal(t, int64(0), out[1].CardID)
	assert.Equal(t, true, out[1].FaceDown) // faceUp=false

	// Nil slot preserved
	assert.Nil(t, out[2])
}

// --------------------------------------------------------------------------
// 5. FaceUp inversion — table-driven
// --------------------------------------------------------------------------

func TestTransformResourceInstance_FaceUpInversion(t *testing.T) {
	tests := []struct {
		name             string
		faceUp           bool
		expectedFaceDown bool
	}{
		{"faceUp true becomes faceDown false", true, false},
		{"faceUp false becomes faceDown true", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &battleResourceInstance{
				InstanceID:       "ri-1",
				CardID:           99,
				FaceUp:           tc.faceUp,
				Attachments:      []battleAttachmentRef{},
				TemporaryEffects: []battleTemporaryEffect{},
			}
			c := transformResourceInstance(b)
			assert.Equal(t, tc.expectedFaceDown, c.FaceDown)
		})
	}
}

// --------------------------------------------------------------------------
// 6. Available actions mapping
// --------------------------------------------------------------------------

func TestTransformAvailableActions(t *testing.T) {
	actions := []battleAvailableAction{
		{
			Type:              "activate_effect",
			HandInstanceID:    nil,
			CardID:            0,
			ValidZones:        nil,
			SourceInstanceID:  ptr("src-inst"),
			ValidTargets:      []string{"target-1", "target-2"},
			TargetRank:        nil,
			InstanceFamily:    nil,
			NeedsFamily:       false,
			RemainingCapacity: 0,
			EffectTargetType:  ptr("resource"),
			RequiredCount:     2,
			ChoiceOptions:     nil,
		},
		{
			Type:              "deploy",
			HandInstanceID:    ptr("hand-99"),
			CardID:            55,
			ValidZones:        []string{"backend"},
			SourceInstanceID:  nil,
			ValidTargets:      nil,
			TargetRank:        ptr("B"),
			InstanceFamily:    ptr("famC"),
			NeedsFamily:       true,
			RemainingCapacity: 3,
			EffectTargetType:  nil,
			RequiredCount:     0,
			ChoiceOptions:     []string{"x"},
		},
	}

	out := transformAvailableActions(actions)

	require.Len(t, out, 2)

	// First action
	assert.Equal(t, "activate_effect", out[0].Type)
	assert.Nil(t, out[0].HandInstanceID)
	assert.Equal(t, int64(0), out[0].CardID)
	assert.Nil(t, out[0].ValidZones)
	require.NotNil(t, out[0].SourceInstanceID)
	assert.Equal(t, "src-inst", *out[0].SourceInstanceID)
	assert.Equal(t, []string{"target-1", "target-2"}, out[0].ValidTargets)
	assert.Nil(t, out[0].TargetRank)
	require.NotNil(t, out[0].EffectTargetType)
	assert.Equal(t, "resource", *out[0].EffectTargetType)

	// Second action
	assert.Equal(t, "deploy", out[1].Type)
	require.NotNil(t, out[1].HandInstanceID)
	assert.Equal(t, "hand-99", *out[1].HandInstanceID)
	assert.Equal(t, int64(55), out[1].CardID)
	assert.Equal(t, []string{"backend"}, out[1].ValidZones)
	assert.Nil(t, out[1].SourceInstanceID)
	require.NotNil(t, out[1].TargetRank)
	assert.Equal(t, "B", *out[1].TargetRank)
	require.NotNil(t, out[1].InstanceFamily)
	assert.Equal(t, "famC", *out[1].InstanceFamily)
	assert.Equal(t, int64(3), out[1].RemainingCapacity)
	assert.Nil(t, out[1].EffectTargetType)
	assert.Equal(t, []string{"x"}, out[1].ChoiceOptions)

	// Verify JSON serialisation uses snake_case keys
	raw, err := json.Marshal(out[1])
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.NotNil(t, m["hand_instance_id"], "should use snake_case key hand_instance_id")
	assert.NotNil(t, m["card_id"], "should use snake_case key card_id")
	assert.NotNil(t, m["valid_zones"], "should use snake_case key valid_zones")
	assert.NotNil(t, m["target_rank"], "should use snake_case key target_rank")
	assert.NotNil(t, m["instance_family"], "should use snake_case key instance_family")
	assert.NotNil(t, m["remaining_capacity"], "should use snake_case key remaining_capacity")
	assert.NotNil(t, m["choice_options"], "should use snake_case key choice_options")
	// Verify camelCase keys are absent
	assert.Nil(t, m["handInstanceID"])
	assert.Nil(t, m["cardID"])
	assert.Nil(t, m["validZones"])
}

// --------------------------------------------------------------------------
// 7. Hand cards — empty and populated
// --------------------------------------------------------------------------

func TestTransformHandCards(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		out := transformHandCards([]battleHandCard{})
		require.NotNil(t, out, "should return non-nil empty slice")
		assert.Len(t, out, 0)
	})

	t.Run("nil slice", func(t *testing.T) {
		out := transformHandCards(nil)
		require.NotNil(t, out, "should return non-nil slice even for nil input")
		assert.Len(t, out, 0)
	})

	t.Run("populated", func(t *testing.T) {
		cards := []battleHandCard{
			{InstanceID: "h1", CardID: 10, ArtNo: 1},
			{InstanceID: "h2", CardID: 20, ArtNo: 2},
		}
		out := transformHandCards(cards)
		require.Len(t, out, 2)

		assert.Equal(t, "h1", out[0].InstanceID)
		assert.Equal(t, int64(10), out[0].CardID)
		assert.Equal(t, "h2", out[1].InstanceID)
		assert.Equal(t, int64(20), out[1].CardID)

		// artNo must not appear in JSON
		raw, err := json.Marshal(out[0])
		require.NoError(t, err)
		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &m))
		assert.Nil(t, m["artNo"], "artNo should not be present in client hand card JSON")
		assert.Equal(t, "h1", m["instanceId"])
		assert.Equal(t, float64(10), m["cardId"])
	})
}
