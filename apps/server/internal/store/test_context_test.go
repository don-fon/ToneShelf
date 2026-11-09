package store

import (
	"context"

	"music-player/server/internal/currentuser"
)

func testContext() context.Context { return currentuser.WithID(context.Background(), DefaultUserID) }
