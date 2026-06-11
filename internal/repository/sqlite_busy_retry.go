package repository

import (
	"context"
	"strings"
	"time"
)

const sqliteBusyRetryMaxElapsed = 6 * time.Second

func withSQLiteBusyRetry(ctx context.Context, op func() error) error {
	delay := 25 * time.Millisecond
	deadline := time.Now().Add(sqliteBusyRetryMaxElapsed)
	for {
		err := op()
		if !IsSQLiteBusyError(err) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if time.Now().Add(delay).After(deadline) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 500*time.Millisecond {
			delay *= 2
		}
	}
}

func IsSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked") ||
		strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}
