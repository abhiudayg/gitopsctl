package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/events"
	"go.uber.org/zap"
)

func TestEmitCommandEvent_NoSinkConfigured(t *testing.T) {
	eventsFile = ""
	eventsWebhookURL = ""
	emitCommandEvent(events.TypeAppRegistered, map[string]any{"app": "a1"})
}

func TestCommandEmitter_WithEventsFileUsesBus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cmd-emitter")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	path := filepath.Join(tmpDir, "events.jsonl")

	origFile := eventsFile
	origHook := eventsWebhookURL
	origLog := logger
	defer func() {
		eventsFile = origFile
		eventsWebhookURL = origHook
		logger = origLog
	}()

	eventsFile = path
	eventsWebhookURL = ""
	logger = zap.NewNop()

	em := commandEmitter()
	if _, ok := em.(*events.Bus); !ok {
		t.Fatalf("expected *events.Bus, got %T", em)
	}
}

func TestCommandEmitter_WithWebhookOnlyUsesBus(t *testing.T) {
	origFile := eventsFile
	origHook := eventsWebhookURL
	origLog := logger
	defer func() {
		eventsFile = origFile
		eventsWebhookURL = origHook
		logger = origLog
	}()

	eventsFile = ""
	eventsWebhookURL = "http://127.0.0.1:9/unused-webhook"
	logger = zap.NewNop()

	em := commandEmitter()
	if _, ok := em.(*events.Bus); !ok {
		t.Fatalf("expected *events.Bus with webhook sink, got %T", em)
	}
}
