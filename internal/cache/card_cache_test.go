package cache

import (
	"testing"

	gencache "github.com/kenyamaneko/overload-party-common/packages/devdata/cache"
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
	for cardID, card := range cc.All() {
		if isResourceType(card.CardType) {
			assert.NotEmptyf(t, card.ResourceLabel,
				"resource card %s (%s, type=%s) has empty resource_label",
				cardID, card.CardName, card.CardType)
		}
	}
}

func TestResourceLabel_SupportCardsHaveNoLabel(t *testing.T) {
	cc := loadTestCache(t)
	for cardID, card := range cc.All() {
		if !isResourceType(card.CardType) {
			assert.Emptyf(t, card.ResourceLabel,
				"support card %s (%s, type=%s) should have empty resource_label, got %q",
				cardID, card.CardName, card.CardType, card.ResourceLabel)
		}
	}
}

func TestResourceLabel_SpecificCards(t *testing.T) {
	cc := loadTestCache(t)

	tests := []struct {
		cardID       string
		wantName     string
		wantLabel    string
		wantCardType string
	}{
		{"SH-0001", "えくぼ", "Compute", "Compute"},       // SHE Compute
		{"TK-0001", "ソラ", "VM", "Compute"},             // Tenki VM (resource_label differs from card_type)
		{"SH-0010", "メリーモ", "Cache", "CacheDB"},         // SHE CacheDB → label "Cache"
		{"SH-0014", "SHE Firewall", "", "Platform"},      // Platform — no label
		{"NT-0013", "DDoS 攻撃", "", "Incident"},         // Incident — no label
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			card := cc.Get(tt.cardID)
			require.NotNilf(t, card, "card %s not found", tt.cardID)
			assert.Equal(t, tt.wantName, card.CardName, "card %s: card_name", tt.cardID)
			assert.Equal(t, tt.wantLabel, card.ResourceLabel, "card %s: resource_label", tt.cardID)
			assert.Equal(t, tt.wantCardType, card.CardType, "card %s: card_type", tt.cardID)
		})
	}
}
