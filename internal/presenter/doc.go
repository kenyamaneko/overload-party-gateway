// Package presenter は下流サービス (account / card / shop / scenario / battle) の wire 型と
// 本リポの公開 wire 型 (packages/api-gateway) の境界変換を集約する。
//
// gateway は固有のドメインロジックを持たないため、shop の Phase 1 のような
// domain ↔ wire 変換ではなく、downstream-wire ↔ gateway-wire 変換が責務。
// 差異 (e.g. apiaccount.PlayerResponse.InitialFaction → apigateway.PlayerResponse.SelectedFaction)
// は本パッケージで明示する。
package presenter
