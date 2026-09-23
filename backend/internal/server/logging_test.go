package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

func TestRPCFailureLogPrivacy(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	for _, code := range []connect.Code{connect.CodeInternal, connect.CodeUnavailable, connect.CodeDeadlineExceeded} {
		output.Reset()
		var err error = connect.NewError(code, errors.New("alice@example.com token=secret https://example.com/private"))
		logRPCFailure(context.Background(), "CreateEvent", &err)
		if strings.Contains(output.String(), "alice") || strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "example.com") {
			t.Fatal("error text leaked")
		}
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record["operation"] != "CreateEvent" || record["code"] != code.String() || record["level"] != "ERROR" {
			t.Fatalf("unexpected record: %v", record)
		}
	}
	output.Reset()
	for _, err := range []error{nil, connect.NewError(connect.CodeInvalidArgument, errors.New("private input")), connect.NewError(connect.CodePermissionDenied, errors.New("private input"))} {
		logRPCFailure(context.Background(), "CreateEvent", &err)
	}
	if output.Len() != 0 {
		t.Fatal("normal results must not produce failure logs")
	}
}

// 実際の書き込みで、成功ログ・失敗時の非出力・入力値の非出力をまとめて確認する。
func TestIntegrationMutationLogs(t *testing.T) {
	db := testdb.Open(t)
	adminID := createTestUser(t, db, "private-display-name", auth.RoleAdmin)
	eventID := createTestEvent(t, db, "private-event-title")
	svc := newTicketService(db)
	req := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId: eventID, StartAt: "2026-10-01T14:00:00+09:00",
		MaxParticipants: 1, MeetingPlace: "private-meeting-place",
		ParticipantUserIds: []string{adminID},
	})
	setSessionCookie(t, db, req, adminID)
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	res, err := svc.CreateTicket(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "ticket created" || record["ticket_id"] != res.Msg.Ticket.Id || record["actor_user_id"] != adminID {
		t.Fatalf("unexpected log: %v", record)
	}
	for _, forbidden := range []string{"private-", req.Header().Get("Cookie")} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatal("private data leaked")
		}
	}
	output.Reset()
	req.Msg.MaxParticipants = 0
	if _, err := svc.CreateTicket(context.Background(), req); err == nil {
		t.Fatal("expected validation error")
	}
	if output.Len() != 0 {
		t.Fatal("rejected mutation logged as successful")
	}
}
