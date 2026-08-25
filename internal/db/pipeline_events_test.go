package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddEventExRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	uid, err := database.CreateUser("test@example.com", "hash", "user")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := database.CreateProject(uid, "/tmp/x.pdf", "x.pdf", "application/pdf", "", 1024)
	if err != nil {
		t.Fatal(err)
	}

	snippet := `{"model":"gemini-3-flash-preview","prompt_chars":100}`
	if err := database.AddEventEx(pid, "llm_call", "ok", "ok", snippet); err != nil {
		t.Fatal(err)
	}

	events, err := database.GetEvents(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != "ok" || ev.ResultSnippet != snippet {
		t.Errorf("got outcome=%q snippet=%q", ev.Outcome, ev.ResultSnippet)
	}

	info, err := database.LLMInfoForProject(pid)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.Model != "gemini-3-flash-preview" {
		t.Errorf("LLMInfoForProject: %+v", info)
	}
}

func TestCreateProjectFileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	uid, _ := database.CreateUser("a@b.com", "h", "user")
	pid, err := database.CreateProject(uid, "/f", "doc.pdf", "application/pdf", "gemini-3-flash-preview", 4096)
	if err != nil {
		t.Fatal(err)
	}
	p, err := database.GetProjectByID(pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.FileSize != 4096 {
		t.Errorf("file_size = %d", p.FileSize)
	}
	_ = os.Remove(path)
}
