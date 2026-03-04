package output_test

import (
	"testing"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/output"
)

func sampleTranscript(id, title, fullText string, date time.Time) *output.Transcript {
	return &output.Transcript{
		ID: id,
		Metadata: output.Metadata{
			Date:            date,
			DurationSeconds: 120,
			Title:           title,
			Source:          "test",
			Model:           "fake",
			Language:        "en",
		},
		Segments: []output.Segment{
			{Start: 0, End: 5, Text: fullText, Confidence: 0.9},
		},
		FullText: fullText,
	}
}

// Test: Write → List → Get → Search round-trip
func TestStoreRoundTrip(t *testing.T) {
	store := output.NewStore(t.TempDir())

	now := time.Now()
	t1 := sampleTranscript("20260303-140000-aaa", "Sprint Planning",
		"Let's prioritize the API migration this sprint.", now.Add(-1*time.Hour))
	t2 := sampleTranscript("20260303-160000-bbb", "1:1 with Sarah",
		"We should discuss the Q2 deadline and hiring plan.", now)

	// Write
	if err := store.Write(t1); err != nil {
		t.Fatalf("Write t1 failed: %v", err)
	}
	if err := store.Write(t2); err != nil {
		t.Fatalf("Write t2 failed: %v", err)
	}

	// List (no filter)
	all, err := store.List(time.Time{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 transcripts, got %d", len(all))
	}
	// Newest first
	if all[0].ID != t2.ID {
		t.Errorf("expected newest first, got %s", all[0].ID)
	}

	// List (with since filter — only recent)
	recent, err := store.List(now.Add(-30 * time.Minute))
	if err != nil {
		t.Fatalf("List with since failed: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent transcript, got %d", len(recent))
	}
	if recent[0].ID != t2.ID {
		t.Errorf("expected %s, got %s", t2.ID, recent[0].ID)
	}

	// Get by ID
	got, err := store.Get(t1.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Metadata.Title != "Sprint Planning" {
		t.Errorf("expected 'Sprint Planning', got %q", got.Metadata.Title)
	}
	if got.FullText != t1.FullText {
		t.Errorf("FullText mismatch")
	}

	// Get non-existent
	_, err = store.Get("nonexistent-id")
	if err == nil {
		t.Error("expected error for non-existent ID")
	}

	// Search — hit
	results, err := store.Search("API migration")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].ID != t1.ID {
		t.Errorf("expected search hit on %s, got %s", t1.ID, results[0].ID)
	}

	// Search — case insensitive
	results, err = store.Search("q2 deadline")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for case-insensitive search, got %d", len(results))
	}

	// Search — no match
	results, err = store.Search("nonexistent query")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// Test: Empty store
func TestStoreEmpty(t *testing.T) {
	store := output.NewStore(t.TempDir())

	list, err := store.List(time.Time{})
	if err != nil {
		t.Fatalf("List on empty store failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}

	results, err := store.Search("anything")
	if err != nil {
		t.Fatalf("Search on empty store failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

// Test: GenerateID format
func TestGenerateID(t *testing.T) {
	id := output.GenerateID()
	if len(id) == 0 {
		t.Fatal("GenerateID returned empty string")
	}
	// Format: YYYYMMDD-HHMMSS-hexhex
	if len(id) < 16 {
		t.Errorf("ID too short: %s", id)
	}
	// Should contain a dash
	if id[8] != '-' {
		t.Errorf("expected dash at position 8: %s", id)
	}

	// Two IDs should be different
	id2 := output.GenerateID()
	if id == id2 {
		t.Errorf("two consecutive IDs should differ: %s == %s", id, id2)
	}
}

// Test: WriteTranscript to specific path
func TestWriteTranscript(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/custom-output.transcript.json"

	tr := sampleTranscript("test-id", "Custom Path Test", "Hello world", time.Now())

	if err := output.WriteTranscript(tr, path); err != nil {
		t.Fatalf("WriteTranscript failed: %v", err)
	}

	// Read it back via a store pointed at the same dir
	store := output.NewStore(tmpDir)
	results, err := store.Search("Hello world")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
