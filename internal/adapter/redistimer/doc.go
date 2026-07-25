// Package redistimer は port.TimerStore を Upstash Redis 上に実装する adapter。
//
// gateway の対戦ごとの計時 (切断猶予・ターン) はインメモリを主とし、本 adapter は
// プロセス終了後も期限を参照できるようにするための写しの読み書きのみを担う。
package redistimer
