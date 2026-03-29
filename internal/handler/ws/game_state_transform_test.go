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
						CardID:         "SH-0001",
						ArtNo:          1,
						Rank:           flexString{ptr("S")},
						InstanceFamily: flexString{ptr("familyA")},
						FaceDown:       false,
						CurrentAV:      3,
						MaxAV:          5,
						CurrentTP:      ptr(int64(2)),
						MaxTP:          ptr(int64(4)),
						CurrentYield:   ptr(int64(1)),
						MaxYield:       ptr(int64(3)),
						Damage:         1,
						Attachments: []battleAttachmentRef{
							{InstanceID: "att-1", CardID: "SH-0002", ArtNo: 2},
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
						CardID:             "NT-0001",
						ArtNo:              3,
						FaceDown:           true,
						DeployingTurnsLeft: 0,
						DeployOrder:        1,
						EffectUsedThisTurn: false,
					},
				},
			},
			Hand: []battleHandCard{
				{InstanceID: "hand-1", CardID: "TK-0001", ArtNo: 4},
				{InstanceID: "hand-2", CardID: "TK-0002", ArtNo: 5},
			},
			RepoCount:  10,
			TrashCount: 2,
			Trash:      []battleHandCard{{InstanceID: "trash-1", CardID: "SL-0001", ArtNo: 6}},
			AvailableActions: []battleAvailableAction{
				{
					Type:              "deploy",
					HandInstanceID:    ptr("hand-1"),
					CardID:            "TK-0001",
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
						CardID:     "TN-0001",
						ArtNo:      1,
						FaceDown:   true,
						CurrentAV:  2,
						MaxAV:      4,
						Attachments:     []battleAttachmentRef{},
						TemporaryEffects: []battleTemporaryEffect{},
					},
				},
				Backend: []*battleResourceInstance{},
				Support: []*battleHiddenSupportInstance{
					{InstanceID: "opp-s1", CardID: "NT-0005", ArtNo: 9, FaceDown: false},
					{InstanceID: "opp-s2", CardID: "NT-0006", ArtNo: 10, FaceDown: true},
				},
			},
			HandCount:  4,
			RepoCount:  8,
			TrashCount: 1,
			Trash:      []battleHandCard{{InstanceID: "opp-trash-1", CardID: "SL-0005", ArtNo: 7}},
		},
	}
}

// --------------------------------------------------------------------------
// 1. Full round-trip — split by concern
// --------------------------------------------------------------------------

// transformTestState is a shared helper that builds, transforms, and parses the battle state.
func transformTestState(t *testing.T) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(buildMinimalBattleState())
	require.NoError(t, err)
	out, err := transformGameState(raw)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	return m
}

func TestTransformGameState_TopLevelKeys(t *testing.T) {
	m := transformTestState(t)

	assert.Equal(t, "game-abc-123", m["gameId"])
	assert.Nil(t, m["gameID"], "old key gameID should not appear")
	assert.Equal(t, float64(3), m["currentTurn"])
	assert.Equal(t, "main", m["currentPhase"])
	assert.Equal(t, float64(1), m["activePlayer"])
	assert.Equal(t, true, m["isMyTurn"])

	assert.NotNil(t, m["my"], "my field must exist")
	assert.Nil(t, m["myView"], "myView should not appear")
	assert.NotNil(t, m["opponent"], "opponent field must exist")
	assert.Nil(t, m["oppView"], "oppView should not appear")

	my := m["my"].(map[string]interface{})
	assert.Equal(t, float64(1), my["playerNum"])
	assert.Equal(t, float64(5), my["budget"])
}

func TestTransformGameState_MyField(t *testing.T) {
	m := transformTestState(t)
	my := m["my"].(map[string]interface{})
	field := my["field"].(map[string]interface{})

	t.Run("hand cards include artNo", func(t *testing.T) {
		hand := my["hand"].([]interface{})
		require.Len(t, hand, 2)
		h0 := hand[0].(map[string]interface{})
		assert.Equal(t, "hand-1", h0["instanceId"])
		assert.Equal(t, "TK-0001", h0["cardId"])
		assert.Equal(t, float64(4), h0["artNo"], "artNo should be passed through")
	})

	t.Run("resource instance", func(t *testing.T) {
		frontend := field["frontend"].([]interface{})
		require.Len(t, frontend, 1)
		res := frontend[0].(map[string]interface{})
		assert.Equal(t, "inst-f1", res["instanceId"])
		assert.Nil(t, res["instanceID"], "old key instanceID should not appear")
		assert.Equal(t, "SH-0001", res["cardId"])
		assert.Nil(t, res["cardID"], "old key cardID should not appear")
		assert.Equal(t, float64(1), res["artNo"], "artNo should be passed through")
		assert.Equal(t, false, res["faceDown"])
		assert.Nil(t, res["faceUp"], "faceUp should not appear in client output")

		attachments := res["attachments"].([]interface{})
		require.Len(t, attachments, 1)
		att := attachments[0].(map[string]interface{})
		assert.Equal(t, "att-1", att["instanceId"])
		assert.Equal(t, "SH-0002", att["cardId"])
		assert.Equal(t, float64(2), att["artNo"], "artNo should be passed through")

		effects := res["temporaryEffects"].([]interface{})
		require.Len(t, effects, 1)
		eff := effects[0].(map[string]interface{})
		assert.Equal(t, "src-1", eff["sourceId"])
		assert.Nil(t, eff["sourceID"], "old key sourceID should not appear")
	})

	t.Run("support instance", func(t *testing.T) {
		support := field["support"].([]interface{})
		require.Len(t, support, 1)
		sup := support[0].(map[string]interface{})
		assert.Equal(t, "inst-s1", sup["instanceId"])
		assert.Equal(t, "NT-0001", sup["cardId"])
		assert.Equal(t, true, sup["faceDown"])
		assert.Equal(t, float64(3), sup["artNo"], "artNo should be passed through")
	})
}

func TestTransformGameState_OpponentView(t *testing.T) {
	m := transformTestState(t)
	opp := m["opponent"].(map[string]interface{})

	assert.Equal(t, float64(2), opp["playerNum"])
	assert.Equal(t, float64(4), opp["handCount"])

	oppField := opp["field"].(map[string]interface{})

	oppFrontend := oppField["frontend"].([]interface{})
	require.Len(t, oppFrontend, 1)
	oppRes := oppFrontend[0].(map[string]interface{})
	assert.Equal(t, true, oppRes["faceDown"])

	oppSupport := oppField["support"].([]interface{})
	require.Len(t, oppSupport, 2)

	os0 := oppSupport[0].(map[string]interface{})
	assert.Equal(t, "opp-s1", os0["instanceId"])
	assert.Equal(t, "NT-0005", os0["cardId"])
	assert.Equal(t, float64(9), os0["artNo"], "artNo should be passed through")
	assert.Equal(t, false, os0["faceDown"])

	os1 := oppSupport[1].(map[string]interface{})
	assert.Equal(t, "opp-s2", os1["instanceId"])
	assert.Equal(t, "NT-0006", os1["cardId"])
	assert.Equal(t, float64(10), os1["artNo"], "artNo should be passed through")
	assert.Equal(t, true, os1["faceDown"])
}

func TestTransformGameState_AvailableActions(t *testing.T) {
	m := transformTestState(t)
	my := m["my"].(map[string]interface{})

	actions := my["available_actions"].([]interface{})
	require.Len(t, actions, 1)
	act := actions[0].(map[string]interface{})
	assert.Equal(t, "deploy", act["type"])
	assert.Equal(t, "hand-1", act["hand_instance_id"])
	assert.Equal(t, "TK-0001", act["card_id"])
	zones := act["valid_zones"].([]interface{})
	assert.Equal(t, []interface{}{"frontend", "backend"}, zones)
	assert.Equal(t, true, act["needs_family"])
	assert.Equal(t, float64(1), act["required_count"])
}

// --------------------------------------------------------------------------
// flexString — number/null/invalid handling
// --------------------------------------------------------------------------

func TestFlexString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *string
		wantErr bool
	}{
		{"string value", `"S"`, ptr("S"), false},
		{"integer", `1`, ptr("1"), false},
		{"float", `2.5`, ptr("2.5"), false},
		{"null", `null`, nil, false},
		{"invalid array", `[]`, nil, true},
		{"invalid object", `{}`, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f flexString
			err := json.Unmarshal([]byte(tc.input), &f)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.want == nil {
				assert.Nil(t, f.Value)
			} else {
				require.NotNil(t, f.Value)
				assert.Equal(t, *tc.want, *f.Value)
			}
		})
	}
}

func TestFlexString_MarshalJSON(t *testing.T) {
	t.Run("non-nil", func(t *testing.T) {
		f := flexString{ptr("A")}
		data, err := json.Marshal(f)
		require.NoError(t, err)
		assert.Equal(t, `"A"`, string(data))
	})
	t.Run("nil", func(t *testing.T) {
		f := flexString{nil}
		data, err := json.Marshal(f)
		require.NoError(t, err)
		assert.Equal(t, `null`, string(data))
	})
}

func TestFlexString_RoundTripInBattleState(t *testing.T) {
	// Simulate C# sending Rank as integer (JsonStringEnumConverter not applied)
	raw := `{"instanceId":"ri-1","cardID":"SH-0001","artNo":1,"rank":1,"instanceFamily":"familyA","faceDown":false,"currentAV":0,"maxAV":0,"damage":0,"monetizedAmount":0,"hasAttacked":false,"effectUsedThisTurn":false,"scaleChangedThisTurn":false,"deployedOnTurn":0,"deployOrder":0,"migratingOnTurn":0,"elasticBonus":0,"attachments":[],"temporaryEffects":[]}`

	var ri battleResourceInstance
	require.NoError(t, json.Unmarshal([]byte(raw), &ri))
	require.NotNil(t, ri.Rank.Value)
	assert.Equal(t, "1", *ri.Rank.Value, "integer rank should be converted to string")
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
				CardID:           "SH-0010",
				FaceDown:         false,
				Attachments:      []battleAttachmentRef{},
				TemporaryEffects: []battleTemporaryEffect{},
			},
			nil,
		},
		Backend: []*battleResourceInstance{nil, nil},
		Support: []*battleSupportInstance{nil, {
			InstanceID: "s1",
			CardID:     "NT-0020",
			FaceDown:   true,
		}},
	}

	c := transformField(field)

	// Frontend: nil preserved, non-nil transformed
	require.Len(t, c.Frontend, 3)
	assert.Nil(t, c.Frontend[0])
	require.NotNil(t, c.Frontend[1])
	assert.Equal(t, "f1", c.Frontend[1].InstanceID)
	assert.Equal(t, false, c.Frontend[1].FaceDown)
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
	assert.Equal(t, true, c.Support[1].FaceDown)
}

// --------------------------------------------------------------------------
// 4. Hidden support slots — cardID pointer nil vs non-nil
// --------------------------------------------------------------------------

func TestTransformHiddenSupportSlots(t *testing.T) {
	slots := []*battleHiddenSupportInstance{
		{InstanceID: "hs-1", CardID: "NT-0042", ArtNo: 5, FaceDown: false},
		{InstanceID: "hs-2", CardID: "NT-0043", ArtNo: 8, FaceDown: true},
		nil,
	}

	out := transformHiddenSupportSlots(slots)

	require.Len(t, out, 3)

	require.NotNil(t, out[0])
	assert.Equal(t, "hs-1", out[0].InstanceID)
	assert.Equal(t, "NT-0042", out[0].CardID)
	assert.Equal(t, int64(5), out[0].ArtNo, "artNo should be passed through")
	assert.Equal(t, false, out[0].FaceDown)

	require.NotNil(t, out[1])
	assert.Equal(t, "hs-2", out[1].InstanceID)
	assert.Equal(t, "NT-0043", out[1].CardID)
	assert.Equal(t, int64(8), out[1].ArtNo, "artNo should be passed through")
	assert.Equal(t, true, out[1].FaceDown)

	// Nil slot preserved
	assert.Nil(t, out[2])
}

// --------------------------------------------------------------------------
// 5. FaceDown passthrough — table-driven
// --------------------------------------------------------------------------

func TestTransformResourceInstance_FaceDownPassthrough(t *testing.T) {
	tests := []struct {
		name     string
		faceDown bool
	}{
		{"faceDown false passes through", false},
		{"faceDown true passes through", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &battleResourceInstance{
				InstanceID:       "ri-1",
				CardID:           "SH-0099",
				FaceDown:         tc.faceDown,
				Attachments:      []battleAttachmentRef{},
				TemporaryEffects: []battleTemporaryEffect{},
			}
			c := transformResourceInstance(b)
			assert.Equal(t, tc.faceDown, c.FaceDown)
		})
	}
}

// --------------------------------------------------------------------------
// 6. Available actions mapping
// --------------------------------------------------------------------------

func TestTransformAvailableActions(t *testing.T) {
	actions := []battleAvailableAction{
		{
			Type:              "use_effect",
			HandInstanceID:    nil,
			CardID:            "",
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
			CardID:            "SL-0055",
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
	assert.Equal(t, "use_effect", out[0].Type)
	assert.Nil(t, out[0].HandInstanceID)
	assert.Equal(t, "", out[0].CardID)
	assert.Nil(t, out[0].ValidZones)
	require.NotNil(t, out[0].SourceInstanceID)
	assert.Equal(t, "src-inst", *out[0].SourceInstanceID)
	assert.Equal(t, []string{"target-1", "target-2"}, out[0].ValidTargets)
	assert.Nil(t, out[0].TargetRank)
	assert.Equal(t, false, out[0].NeedsFamily)
	assert.Equal(t, 2, out[0].RequiredCount)
	require.NotNil(t, out[0].EffectTargetType)
	assert.Equal(t, "resource", *out[0].EffectTargetType)

	// Second action
	assert.Equal(t, "deploy", out[1].Type)
	require.NotNil(t, out[1].HandInstanceID)
	assert.Equal(t, "hand-99", *out[1].HandInstanceID)
	assert.Equal(t, "SL-0055", out[1].CardID)
	assert.Equal(t, []string{"backend"}, out[1].ValidZones)
	assert.Nil(t, out[1].SourceInstanceID)
	require.NotNil(t, out[1].TargetRank)
	assert.Equal(t, "B", *out[1].TargetRank)
	require.NotNil(t, out[1].InstanceFamily)
	assert.Equal(t, "famC", *out[1].InstanceFamily)
	assert.Equal(t, true, out[1].NeedsFamily)
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
	assert.NotNil(t, m["needs_family"], "should use snake_case key needs_family")
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
			{InstanceID: "h1", CardID: "SH-0010", ArtNo: 1},
			{InstanceID: "h2", CardID: "TK-0020", ArtNo: 2},
		}
		out := transformHandCards(cards)
		require.Len(t, out, 2)

		assert.Equal(t, "h1", out[0].InstanceID)
		assert.Equal(t, "SH-0010", out[0].CardID)
		assert.Equal(t, "h2", out[1].InstanceID)
		assert.Equal(t, "TK-0020", out[1].CardID)

		raw, err := json.Marshal(out[0])
		require.NoError(t, err)
		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &m))
		assert.Equal(t, float64(1), m["artNo"], "artNo should be present in client hand card JSON")
		assert.Equal(t, "h1", m["instanceId"])
		assert.Equal(t, "SH-0010", m["cardId"])
	})
}
