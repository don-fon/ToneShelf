package server

import (
	"context"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

func testContext() context.Context {
	return currentuser.WithID(context.Background(), store.DefaultUserID)
}
