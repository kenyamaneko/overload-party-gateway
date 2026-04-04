package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

type StaticService struct {
	announcements []model.Announcement
	dailyTips     []model.DailyTip
}

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

// ActiveAnnouncements returns announcements that are currently active.
func (s *StaticService) ActiveAnnouncements() []model.Announcement {
	now := time.Now()
	active := make([]model.Announcement, 0)
	for _, a := range s.announcements {
		if !a.PublishedAt.After(now) && a.ExpiresAt.After(now) {
			active = append(active, a)
		}
	}
	return active
}

// TodaysTip returns a deterministically selected daily tip based on the date.
func (s *StaticService) TodaysTip() *model.DailyTip {
	if len(s.dailyTips) == 0 {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	hash := sha256.Sum256([]byte(today))
	idx := int(hash[0]) % len(s.dailyTips)
	return &s.dailyTips[idx]
}
