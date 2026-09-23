package server

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
)

// RPC と MCP で共通のサービス境界で障害だけを記録する。
// エラー本文には入力値や外部サービスの応答が含まれ得るため出力しない。
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
