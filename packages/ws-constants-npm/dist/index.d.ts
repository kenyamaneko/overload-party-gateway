export declare const WS_SERVER_MSG_TYPES: readonly ["game_state", "game_over", "error", "game_entered", "matchmaking_started", "matchmaking_cancelled", "action_rejected", "stamp_used", "pong", "match_found", "action_performed", "turn_controls", "npc_battle_created", "opponent_disconnected", "opponent_reconnected", "game_state_restore"];
export type WSServerMsgType = (typeof WS_SERVER_MSG_TYPES)[number];
export declare const WS_CLIENT_MSG_TYPES: readonly ["game_enter", "matchmaking_start", "matchmaking_cancel", "game_action", "use_stamp", "ping", "npc_battle_start"];
export type WSClientMsgType = (typeof WS_CLIENT_MSG_TYPES)[number];
//# sourceMappingURL=index.d.ts.map