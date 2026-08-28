package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsValidUserID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{name: "simple username", id: "user-1", valid: true},
		{name: "email-like id", id: "test.user@example.com", valid: true},
		{name: "single char", id: "a", valid: true},
		{name: "empty", id: "", valid: false},
		{name: "contains space", id: "user 1", valid: false},
		{name: "contains slash (path traversal-ish)", id: "user/1", valid: false},
		{name: "too long (256 chars)", id: strings.Repeat("a", 256), valid: false},
		{name: "exactly 255 chars", id: strings.Repeat("a", 255), valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.valid, isValidUserID(tt.id))
		})
	}
}
