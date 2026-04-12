package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// StaticService はお知らせや日替わり Tips など静的コンテンツを提供します
type StaticService struct {
	announcements []apigateway.Announcement
	dailyTips     []apigateway.DailyTip
}

// NewStaticService は dataDir 内の JSON ファイルを読み込み StaticService を生成します
func NewStaticService(dataDir string) (*StaticService, error) {
	s := &StaticService{}

	if data, err := os.ReadFile(filepath.Join(dataDir, "announcements.json")); err == nil {
		if err := json.Unmarshal(data, &s.announcements); err != nil {
			return nil, fmt.Errorf("parse announcements.json: %w", err)
		}
	}

	if data, err := os.ReadFile(filepath.Join(dataDir, "daily_tips.json")); err == nil {
		if err := json.Unmarshal(data, &s.dailyTips); err != nil {
			return nil, fmt.Errorf("parse daily_tips.json: %w", err)
		}
	}

	return s, nil
}

// ActiveAnnouncements は現在有効なお知らせを返します
func (s *StaticService) ActiveAnnouncements() []apigateway.Announcement {
	now := time.Now()
	active := make([]apigateway.Announcement, 0)
	for _, a := range s.announcements {
		if !a.PublishedAt.After(now) && a.ExpiresAt.After(now) {
			active = append(active, a)
		}
	}
	return active
}

// TodaysTip は日付に基づいて決定論的に選択された日替わり Tips を返します
func (s *StaticService) TodaysTip() *apigateway.DailyTip {
	if len(s.dailyTips) == 0 {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	hash := sha256.Sum256([]byte(today))
	idx := int(hash[0]) % len(s.dailyTips)
	return &s.dailyTips[idx]
}
