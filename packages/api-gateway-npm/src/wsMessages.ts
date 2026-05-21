// Overload Party gateway WebSocket message envelope types.
// These are cross-service message shapes that the gateway relays between
// client and battle service. Hand-maintained in the gateway repo.

import type { ClientGameState } from '@kenyamaneko/overload-party-game-state';

export interface MatchmakingStartMessage {
  deck_id: number;
}

export interface GameEnterMessage {
  game_id: string;
  deck_id: number;
}

export interface NPCBattleStartMessage {
  deck_id: number;
  npc_model: string;
}

export interface GameActionMessage {
  game_id: string;
  action_type: string;
  data: Record<string, unknown>;
}

export interface UseStampMessage {
  game_id: string;
  stamp_no: number;
}

export interface ErrorMessage {
  error_code: string;
  message: string;
  retryable: boolean;
}

export interface MatchFoundMessage {
  game_id: string;
}

export interface GameOverMessage {
  game_id: string;
  winning_player_num: number;
  win_reason: string;
}

export interface ActionRejectedMessage {
  game_id: string;
  action_type: string;
  reason: string;
}

/** ActionPerformedMessage notifies a player of an action and the resulting state. */
export interface ActionPerformedMessage {
  sequence: number;
  action_type: string;
  action_data: Record<string, unknown>;
  state: ClientGameState;
}

export interface StampUsedMessage {
  game_id: string;
  player_num: number;
  stamp_no: number;
}

export interface NPCBattleCreatedMessage {
  game_id: string;
}

export interface OpponentDisconnectedMessage {
}

export interface OpponentReconnectedMessage {
}

