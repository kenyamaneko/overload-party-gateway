// Package pubsub は gateway の Pub/Sub subscriber を管理する。
//
// gateway は matchmaking サービスからの match 成立通知 (`matchmaking-events`
// topic) を唯一の subscriber として受信し、自 Pod に該当プレイヤーの WS 接続が
// あれば push する。Pub/Sub をサービス間連携専用とする方針 (ADR-027) のもと、
// 他サービスが publish するイベントを subscribe する fan-out 配線は持たない。
package pubsub
