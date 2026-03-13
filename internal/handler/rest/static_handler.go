package rest

import (
	"crypto/sha256"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
)

// Announcement represents a single announcement entry.
type Announcement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Type        string    `json:"type"` // info, warning, maintenance
	PublishedAt time.Time `json:"publishedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// DailyTip represents a single daily tip entry.
type DailyTip struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// StaticHandler serves version, announcements, and daily tip endpoints.
type StaticHandler struct {
	cfg           *config.Config
	announcements []Announcement
	dailyTips     []DailyTip
}

// NewStaticHandler creates a StaticHandler, loading JSON data files from dataDir.
func NewStaticHandler(cfg *config.Config, dataDir string) *StaticHandler {
	h := &StaticHandler{cfg: cfg}

	// Load announcements
	if data, err := os.ReadFile(dataDir + "/announcements.json"); err == nil {
		if err := json.Unmarshal(data, &h.announcements); err != nil {
			log.Printf("WARN: failed to parse announcements.json: %v", err)
		}
	}

	// Load daily tips
	if data, err := os.ReadFile(dataDir + "/daily_tips.json"); err == nil {
		if err := json.Unmarshal(data, &h.dailyTips); err != nil {
			log.Printf("WARN: failed to parse daily_tips.json: %v", err)
		}
	}

	return h
}

// GetVersion returns the app version information.
func (h *StaticHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"minimumVersion": h.cfg.AppMinVersion,
		"latestVersion":  h.cfg.AppLatestVersion,
		"forceUpdate":    h.cfg.AppForceUpdate,
	})
}

// GetAnnouncements returns active announcements.
func (h *StaticHandler) GetAnnouncements(c *gin.Context) {
	now := time.Now()
	active := make([]Announcement, 0)
	for _, a := range h.announcements {
		if !a.PublishedAt.After(now) && a.ExpiresAt.After(now) {
			active = append(active, a)
		}
	}
	c.JSON(http.StatusOK, active)
}

// GetDaily returns today's daily tip (rotated by date).
func (h *StaticHandler) GetDaily(c *gin.Context) {
	if len(h.dailyTips) == 0 {
		c.JSON(http.StatusOK, gin.H{"id": "", "text": ""})
		return
	}

	// Deterministic daily rotation using date hash
	today := time.Now().Format("2006-01-02")
	hash := sha256.Sum256([]byte(today))
	idx := int(hash[0]) % len(h.dailyTips)

	tip := h.dailyTips[idx]
	c.JSON(http.StatusOK, gin.H{
		"id":   tip.ID,
		"text": tip.Text,
	})
}
