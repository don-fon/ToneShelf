package currentuser

import (
	"context"
	"errors"
	"strings"
)

type contextKey struct{}

var ErrMissing = errors.New("current user is required")

// WithID is for trusted request resolvers and persisted background-job owners.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func ID(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return id
}

func Require(ctx context.Context) error {
	if ID(ctx) == "" {
		return ErrMissing
	}
	return nil
}
