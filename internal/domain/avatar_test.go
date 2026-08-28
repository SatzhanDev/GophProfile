package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAvatar_IsDeleted(t *testing.T) {
	active := &Avatar{}
	require.False(t, active.IsDeleted())

	deletedAt := time.Now()
	deleted := &Avatar{DeletedAt: &deletedAt}
	require.True(t, deleted.IsDeleted())
}
