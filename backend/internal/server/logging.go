package server

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
)

func logRPCFailure(ctx context.Context, operation string, err *error) {
	if *err == nil {
		return
	}
	code := connect.CodeOf(*err)
	switch code {
	case connect.CodeInternal, connect.CodeUnknown, connect.CodeUnavailable, connect.CodeDataLoss, connect.CodeDeadlineExceeded:
		slog.ErrorContext(ctx, "operation failed", "operation", operation, "code", code.String())
	}
}
