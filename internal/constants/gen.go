// Re-export generated constants from the shared package.
// This allows all existing code to continue importing "internal/constants" unchanged.
package constants

import genconstants "github.com/kenyamaneko/overload-party-common/packages/gamedata/constants"

const (
	DeckSize       = genconstants.DeckSize
	FactionSHE     = genconstants.FactionSHE
	FactionTenki   = genconstants.FactionTenki
	FactionSugar   = genconstants.FactionSugar
	FactionTuners  = genconstants.FactionTuners
	FactionNeutral = genconstants.FactionNeutral
)
