package challenges

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

func TestChallengeFileBoundary(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "game.ohne")
	if err := mapfile.Write(filename, []byte(`{"format":"ohneguessr.challenge"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mapfile.Write(filename, []byte(`{"format":"ohneguessr.challenge","version":1}`), 0o644); err != nil {
		t.Fatalf("overwrite challenge: %v", err)
	}
	contents, err := readChallengeFile(filename)
	if err != nil || contents == "" {
		t.Fatalf("read challenge = %q, %v", contents, err)
	}
	wrong := filepath.Join(dir, "game.json")
	if err := os.WriteFile(wrong, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readChallengeFile(wrong); err == nil {
		t.Fatal("expected non-.ohne file to fail")
	}
	large := filepath.Join(dir, "large.ohne")
	if err := os.WriteFile(large, []byte(strings.Repeat("x", maxChallengeSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readChallengeFile(large); err == nil {
		t.Fatal("expected oversized challenge to fail")
	}
}

func TestServiceLaunchesValidatedChallenge(t *testing.T) {
	launched := ""
	service := NewService(nil, func(target string) error {
		launched = target
		return nil
	}, nil, nil)
	id := "550e8400-e29b-41d4-a716-446655440000"
	contents := `{"format":"ohneguessr.challenge","version":1,"id":"` + id + `"}`
	if err := service.LaunchChallenge(id, contents); err != nil {
		t.Fatal(err)
	}
	if launched != "/?view=game&challenge="+id {
		t.Fatalf("launched %q", launched)
	}
	if active, err := service.GetActiveChallenge(id); err != nil || active != contents {
		t.Fatalf("active challenge = %q, %v", active, err)
	}
	if err := service.LaunchChallenge("wrong", contents); err == nil {
		t.Fatal("expected mismatched challenge ID to fail")
	}
}
