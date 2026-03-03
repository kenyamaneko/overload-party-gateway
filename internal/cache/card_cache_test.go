package cache

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kenyamaneko/overload-party-common/model"
)

func testCardsGenPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "cards_gen.json")
}

func loadTestCache(t *testing.T) *CardCache {
	t.Helper()
	cc := NewCardCache()
	if err := cc.LoadFromJSON(testCardsGenPath()); err != nil {
		t.Fatalf("LoadFromJSON: %v", err)
	}
	return cc
}

func TestLoadFromJSON_CardCount(t *testing.T) {
	cc := loadTestCache(t)
	if cc.Count() == 0 {
		t.Fatal("no cards loaded")
	}
}

func TestResourceLabel_ResourceCardsHaveLabel(t *testing.T) {
	cc := loadTestCache(t)
	for cardNo, card := range cc.All() {
		if model.IsResourceType(card.CardType) && card.ResourceLabel == "" {
			t.Errorf("resource card #%d (%s, type=%s) has empty resource_label",
				cardNo, card.CardName, card.CardType)
		}
	}
}

func TestResourceLabel_SupportCardsHaveNoLabel(t *testing.T) {
	cc := loadTestCache(t)
	for cardNo, card := range cc.All() {
		if !model.IsResourceType(card.CardType) && card.ResourceLabel != "" {
			t.Errorf("support card #%d (%s, type=%s) should have empty resource_label, got %q",
				cardNo, card.CardName, card.CardType, card.ResourceLabel)
		}
	}
}

func TestResourceLabel_SpecificCards(t *testing.T) {
	cc := loadTestCache(t)

	tests := []struct {
		cardNo        int64
		wantName      string
		wantLabel     string
		wantCardType  string
	}{
		{1, "えくぼ", "Compute", "Compute"},             // SD Compute
		{23, "ソラ", "VM", "Compute"},                   // Tenki VM (resource_label differs from card_type)
		{11, "メリー", "Cache", "CacheDB"},               // SD CacheDB → label "Cache"
		{15, "SD Firewall", "", "Platform"},              // Platform — no label
		{104, "DDoS 攻撃", "", "Incident"},               // Incident — no label
	}

	for _, tt := range tests {
		card := cc.Get(tt.cardNo)
		if card == nil {
			t.Errorf("card #%d not found", tt.cardNo)
			continue
		}
		if card.CardName != tt.wantName {
			t.Errorf("card #%d: card_name = %q, want %q", tt.cardNo, card.CardName, tt.wantName)
		}
		if card.ResourceLabel != tt.wantLabel {
			t.Errorf("card #%d: resource_label = %q, want %q", tt.cardNo, card.ResourceLabel, tt.wantLabel)
		}
		if card.CardType != tt.wantCardType {
			t.Errorf("card #%d: card_type = %q, want %q", tt.cardNo, card.CardType, tt.wantCardType)
		}
	}
}
