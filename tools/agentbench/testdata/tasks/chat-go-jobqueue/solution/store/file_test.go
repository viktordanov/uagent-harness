package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/jobq/internal/clock"
	"example.com/jobq/queue"
)

func TestFileMissingIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	f, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := f.List("")
	if len(all) != 0 {
		t.Fatalf("new store has %d jobs", len(all))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("opening created the file: %v", err)
	}
}

func TestFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "jobs.json")
	f, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := mustAdd(t, f, "echo", "hello")
	b := mustAdd(t, f, "fail", "")
	c := mustAdd(t, f, "echo", "later")

	claimed, ok, err := f.Claim(clock.Epoch)
	if err != nil || !ok || claimed.ID != a.ID {
		t.Fatalf("Claim = %v, %v, %v", claimed, ok, err)
	}
	claimed.State = queue.Done
	if err := f.Update(claimed); err != nil {
		t.Fatal(err)
	}
	b.State = queue.Dead
	b.Attempts = 3
	b.LastError = "boom"
	if err := f.Update(b); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(c.ID); err != nil {
		t.Fatal(err)
	}

	g, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := g.List("")
	if len(all) != 2 {
		t.Fatalf("reopened store has %d jobs: %v", len(all), all)
	}
	if all[0].ID != a.ID || all[0].State != queue.Done || all[0].Attempts != 1 {
		t.Errorf("first job: %+v", all[0])
	}
	if all[1].ID != b.ID || all[1].State != queue.Dead || all[1].LastError != "boom" {
		t.Errorf("second job: %+v", all[1])
	}
	d := mustAdd(t, g, "echo", "new")
	if d.ID != "job-0004" {
		t.Errorf("the reopened store reused an ID: %s", d.ID)
	}
}

func TestFileKeepsTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	f, _ := OpenFile(path)
	task := queue.NewJob("echo", "x")
	task.RunAt = clock.Epoch.Add(90 * time.Minute)
	task.CreatedAt = clock.Epoch
	if _, err := f.Add(task); err != nil {
		t.Fatal(err)
	}
	g, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.Get("job-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !got.RunAt.Equal(task.RunAt) || !got.CreatedAt.Equal(task.CreatedAt) {
		t.Fatalf("times changed: %+v", got)
	}
}

func TestFileRejectsBadContent(t *testing.T) {
	cases := map[string]string{
		"not json":      "{",
		"unknown state": `{"version":1,"next_id":1,"jobs":[{"id":"job-0001","kind":"echo","state":"lost","attempts":0}]}`,
		"duplicate id":  `{"version":1,"next_id":2,"jobs":[{"id":"job-0001","kind":"echo","state":"done"},{"id":"job-0001","kind":"echo","state":"done"}]}`,
		"newer version": `{"version":9,"next_id":0,"jobs":[]}`,
		"unknown field": `{"version":1,"next_id":0,"jobs":[],"extra":true}`,
	}
	for name, content := range cases {
		path := filepath.Join(t.TempDir(), "jobs.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenFile(path); err == nil {
			t.Errorf("%s: OpenFile accepted %s", name, content)
		}
	}
}

func TestFileNextIDFollowsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	content := `{"version":1,"next_id":1,"jobs":[{"id":"job-0007","kind":"echo","state":"pending"}]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustAdd(t, f, "echo", ""); got.ID != "job-0008" {
		t.Fatalf("new ID %s, want job-0008", got.ID)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"next_id": 8`) {
		t.Fatalf("saved file:\n%s", b)
	}
}
