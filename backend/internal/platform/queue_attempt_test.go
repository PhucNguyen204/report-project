package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestWorkerUsesQueueAttemptBudgetInsteadOfPayloadMetadata(t *testing.T) {
	cipher, err := NewEventCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	event, err := cipher.Seal(Event{Event: "ai.suggest", Key: "ai.suggest:one", Payload: json.RawMessage(`{"jobId":"fixture"}`)})
	if err != nil {
		t.Fatal(err)
	}
	w := eventWorker{cipher: cipher, handler: func(_ context.Context, e Event) error {
		if e.Attempt != 2 || e.MaxAttempts != 2 || !bytes.Equal(e.Payload, []byte(`{"jobId":"fixture"}`)) {
			t.Fatalf("worker lost trusted queue attempt budget: %d/%d", e.Attempt, e.MaxAttempts)
		}
		return nil
	}}
	if err := w.Work(context.Background(), &river.Job[Event]{JobRow: &rivertype.JobRow{Attempt: 2, MaxAttempts: 2}, Args: event}); err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal([]byte(`{"event":"ai.suggest","Attempt":99,"MaxAttempts":99}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Attempt != 0 || decoded.MaxAttempts != 0 {
		t.Fatal("untrusted payload supplied execution metadata")
	}
}
