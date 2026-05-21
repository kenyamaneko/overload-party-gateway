export const WS_SERVER_MSG_TYPES = ["game_state", "game_over", "error", "game_entered", "matchmaking_started", "matchmaking_cancelled", "action_rejected", "stamp_used", "pong", "match_found", "action_performed", "turn_controls", "npc_battle_created", "opponent_disconnected", "opponent_reconnected", "game_state_restore"] as const;
export type WSServerMsgType = (typeof WS_SERVER_MSG_TYPES)[number];

export const WS_CLIENT_MSG_TYPES = ["game_enter", "matchmaking_start", "matchmaking_cancel", "game_action", "use_stamp", "ping", "npc_battle_start"] as const;
export type WSClientMsgType = (typeof WS_CLIENT_MSG_TYPES)[number];
