package eventstate

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func isNotFound(err error) bool { return status.Code(err) == codes.NotFound }
