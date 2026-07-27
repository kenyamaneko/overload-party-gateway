package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDatabaseIAMAuthEnabled(t *testing.T) {
	t.Run("DATABASE_IAM_AUTH_ENABLED の解釈", func(t *testing.T) {
		t.Run(`"true" のとき、true になる`, func(t *testing.T) {
			assert.True(t, parseDatabaseIAMAuthEnabled("true"))
		})

		t.Run(`"false" のとき、false になる`, func(t *testing.T) {
			assert.False(t, parseDatabaseIAMAuthEnabled("false"))
		})
	})
}
