package model

import "time"

type NewsArticle struct {
	ArticleID   string     `json:"article_id"`
	Source      string     `json:"source"`
	Title       string     `json:"title"`
	Summary     *string    `json:"summary,omitempty"`
	Tags        []string   `json:"tags"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	FetchedAt   time.Time  `json:"fetched_at"`
}
