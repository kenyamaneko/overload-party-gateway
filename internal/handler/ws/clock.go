package ws

import "time"

// Clock はターン・切断タイマーが依存する時刻取得とタイマー生成を抽象化する。
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer は Clock.AfterFunc が返すタイマーの停止操作を表す。
type Timer interface {
	Stop() bool
}

// systemClock は time パッケージをそのまま使う Clock の既定実装。
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
