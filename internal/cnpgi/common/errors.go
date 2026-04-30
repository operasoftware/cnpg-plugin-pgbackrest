package common

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newWALNotFoundError creates a gRPC NotFound error for a missing WAL file
func newWALNotFoundError() error {
	return status.Error(codes.NotFound, "WAL file not found")
}

// ErrEndOfWALStreamReached is returned when end of WAL is detected in the cloud archive.
var ErrEndOfWALStreamReached = status.Error(codes.OutOfRange, "end of WAL reached")

// ErrMissingPermissions is returned when the plugin doesn't have the required permissions.
var ErrMissingPermissions = status.Error(codes.FailedPrecondition,
	"backup credentials don't yet have access permissions. Will retry reconciliation loop")
