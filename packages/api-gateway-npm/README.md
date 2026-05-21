# @kenyamaneko/overload-party-api-gateway

The single npm package for the Overload Party client. Contains all REST
request/response types and WS message envelopes the client needs.

The client only talks to gateway, so every domain's types are consolidated
here:

- **Gateway-native** — Version, Announcement, DailyTip, ErrorResponse
- **Player** (account domain) — PlayerResponse, BattleLimitResponse, UserSettings, etc.
- **Card / Deck** (card domain) — PlayerCardWithDef, Deck, DeckCreateRequest, etc.
- **Shop** (shop domain) — ProductResponse, PurchaseRequest, SubscribeResponse, etc.
- **Scenario** (scenario domain) — EpisodeWithStatus, LockReason, ScenarioScriptResponse, etc.
- **WebSocket** — All WS message envelopes (matchmaking, game relay, stamps)

Published from `overload-party-gateway/packages/api-gateway-npm/`.
