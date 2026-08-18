package auth

import (
	"context"
	"time"
)

// timeoutCtx is a tiny helper so file.go can bound its lock wait without
// threading a context through the Store interface, which every other
// implementation would ignore.
func timeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
