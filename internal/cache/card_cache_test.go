package cache

import (
	"testing"

	gencache "github.com/kenyamaneko/overload-party-common/packages/gamedata/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isResourceType returns true if the card type is a deployable resource.
func isResourceType(cardType string) bool {
	switch cardType {
	case "Compute", "Container", "Orchestrator", "Serverless", "AI/ML", "Database", "CacheDB", "ObjectStorage":
		return true
	}
	return false
}

func loadTestCache(t *testing.T) *CardCache {
	t.Helper()
	cc := NewCardCache()
	if err := cc.LoadFromBytes(gencache.CardsJSON); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	return cc
}

func TestLoadFromJSON_CardCount(t *testing.T) {
	cc := loadTestCache(t)
	require.NotZero(t, cc.Count(), "no cards loaded")
}

func TestResourceLabel_ResourceCardsHaveLabel(t *testing.T) {
	cc := loadTestCache(t)
	for cardNo, card := range cc.All() {
		if isResourceType(card.CardType) {
			assert.NotEmptyf(t, card.ResourceLabel,
				"resource card #%d (%s, type=%s) has empty resource_label",
				cardNo, card.CardName, card.CardType)
		}
	}
}

func TestResourceLabel_SupportCardsHaveNoLabel(t *testing.T) {
	cc := loadTestCache(t)
	for cardNo, card := range cc.All() {
		if !isResourceType(card.CardType) {
			assert.Emptyf(t, card.ResourceLabel,
				"support card #%d (%s, type=%s) should have empty resource_label, got %q",
				cardNo, card.CardName, card.CardType, card.ResourceLabel)
		}
	}
}

func TestResourceLabel_SpecificCards(t *testing.T) {
	cc := loadTestCache(t)

	tests := []struct {
		cardNo       int64
		wantName     string
		wantLabel    string
		wantCardType string
	}{
		{1, "えくぼ", "Compute", "Compute"},       // SHE Compute
		{23, "ソラ", "VM", "Compute"},             // Tenki VM (resource_label differs from card_type)
		{11, "メリーモ", "Cache", "CacheDB"},         // SHE CacheDB → label "Cache"
		{15, "SHE Firewall", "", "Platform"},      // Platform — no label
		{104, "DDoS 攻撃", "", "Incident"},         // Incident — no label
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			card := cc.Get(tt.cardNo)
			require.NotNilf(t, card, "card #%d not found", tt.cardNo)
			assert.Equal(t, tt.wantName, card.CardName, "card #%d: card_name", tt.cardNo)
			assert.Equal(t, tt.wantLabel, card.ResourceLabel, "card #%d: resource_label", tt.cardNo)
			assert.Equal(t, tt.wantCardType, card.CardType, "card #%d: card_type", tt.cardNo)
		})
	}
}
