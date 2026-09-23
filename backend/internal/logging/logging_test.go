package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestIDDoesNotLogArbitraryInput(t *testing.T) {
	for _, input := range []string{"alice@example.com", "secret\nforged log", "", "01955ba0-1234-7000-8000-123456789abc"} {
		t.Run(input, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			logger.Info("event created", ID("event_id", input))
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			expected := "invalid"
			if strings.HasPrefix(input, "01955ba0-") {
				expected = input
			}
			if record["event_id"] != expected {
				t.Fatalf("unexpected ID: %v", record["event_id"])
			}
		})
	}
}
