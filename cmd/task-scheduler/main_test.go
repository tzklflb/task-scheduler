package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--input", input}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK: 12 タスク") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSummaryWritesOnlyJSON(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"summary", "--format", "json", "--input", input}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var summary struct {
		Assignments []struct {
			ID       string  `json:"id"`
			Assignee string  `json:"assignee"`
			Start    string  `json:"start"`
			End      string  `json:"end"`
			Status   *string `json:"status"`
		} `json:"assignments"`
		CoreEnd              string  `json:"core_end"`
		CompletionWithBuffer string  `json:"completion_with_buffer"`
		RemainingEffort      float64 `json:"remaining_effort"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("stdout is not JSON: %q (%v)", stdout.String(), err)
	}
	if len(summary.Assignments) != 11 || summary.CoreEnd != "2026-01-20" || summary.CompletionWithBuffer != "2026-01-30" || summary.RemainingEffort != 8.5 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestSummaryMatchesGolden(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		golden string
	}{
		{
			name:   "generic epic",
			input:  filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"),
			golden: filepath.Join("..", "..", "testdata", "generic-epic", "expected-summary.json"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"summary", "--format", "json", "--input", test.input}, &stdout, &stderr); code != 0 {
				t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
			}

			got := bytes.TrimSpace(stdout.Bytes())
			golden, err := os.ReadFile(test.golden)
			if err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			if err := json.Compact(&want, golden); err != nil {
				t.Fatalf("golden is not JSON: %v", err)
			}
			if !bytes.Equal(got, want.Bytes()) {
				t.Fatalf("summary differs from %s\n got: %s\nwant: %s", test.golden, got, want.Bytes())
			}
		})
	}
}

func TestRenderWritesHTML(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml")
	path := filepath.Join(t.TempDir(), "gantt.html")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"render", "--format", "html", "--input", input, "--output", path, "--available", "contributor=2026-01-08"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 || !strings.Contains(stdout.String(), "OK:") {
		t.Fatalf("content = %q, stdout = %q", content, stdout.String())
	}
	if !strings.Contains(string(content), "（このシナリオ試算では contributor の参画日を 2026-01-08 に上書き）") {
		t.Fatalf("rendered content does not contain the available override assumption: %q", content)
	}
}

func TestCheckRejectsPositionalArguments(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--input", input, "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); got != "unexpected positional arguments\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestCheckReportsInputProblems(t *testing.T) {
	for name, test := range map[string]struct {
		content string
		want    []string
	}{
		"YAML": {
			content: `epic: {id: TEST, id: AGAIN, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
			want: []string{"重複", "NG: 入力に問題があります"},
		},
		"semantic": {
			content: `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks:
  - {id: t1, name: one, plan: p, effort: 1, deps: [missing], lane: ios}
  - {id: t2, name: two, plan: p, effort: 1, deps: [], lane: be, status: ""}
`,
			want: []string{"依存", "lane", "status"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schedule.yaml")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := run([]string{"check", "--input", path}, &stdout, &stderr); code != 1 {
				t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
			for _, fragment := range test.want {
				if !strings.Contains(stderr.String(), fragment) {
					t.Errorf("stderr = %q, want %q", stderr.String(), fragment)
				}
			}
		})
	}
}

func TestApplyCandidate(t *testing.T) {
	for name, test := range map[string]struct {
		prepare func(t *testing.T, target, candidate string) []string
		want    int
		check   func(t *testing.T, target, candidate string, stdout, stderr bytes.Buffer)
	}{
		"creates new schedule": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				return []string{"--expect-absent"}
			},
			want: 0,
			check: func(t *testing.T, target, candidate string, stdout, stderr bytes.Buffer) {
				if stderr.Len() != 0 || !strings.Contains(stdout.String(), "OK:") {
					t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
				}
				if _, err := os.Stat(target); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(candidate); !os.IsNotExist(err) {
					t.Fatalf("candidate still exists: %v", err)
				}
			},
		},
		"updates existing schedule and preserves mode": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				if err := os.Chmod(target, 0o666); err != nil {
					t.Fatal(err)
				}
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				content, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(candidate, bytes.Replace(content, []byte("汎用テスト"), []byte("更新済み"), 1), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--expected-sha256", fileDigest(t, target)}
			},
			want: 0,
			check: func(t *testing.T, target, candidate string, stdout, stderr bytes.Buffer) {
				content, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(content, []byte("更新済み")) || stderr.Len() != 0 {
					t.Fatalf("content = %q, stderr = %q", content, stderr.String())
				}
				info, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != 0o666 {
					t.Fatalf("mode = %o, want 666", got)
				}
			},
		},
		"rejects hash conflict": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				expected := fileDigest(t, target)
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				if err := os.WriteFile(target, append([]byte("# changed\n"), mustReadFile(t, target)...), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--expected-sha256", expected}
			},
			want:  1,
			check: unchangedCandidateAndTarget,
		},
		"rejects wrong epic ID": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				writeApplyFixture(t, candidate, "WRONG")
				return []string{"--expected-sha256", fileDigest(t, target)}
			},
			want:  1,
			check: unchangedCandidateAndTarget,
		},
		"rejects as of change without opt in": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				content, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(candidate, bytes.Replace(content, []byte(`as_of: "2026-01-06"`), []byte(`as_of: "2026-01-07"`), 1), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--expected-sha256", fileDigest(t, target)}
			},
			want:  1,
			check: unchangedCandidateAndTarget,
		},
		"allows as of change with opt in": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				content := mustReadFile(t, candidate)
				if err := os.WriteFile(candidate, bytes.Replace(content, []byte("as_of: \"2026-01-06\""), []byte("as_of: \"2026-01-07\""), 1), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--expected-sha256", fileDigest(t, target), "--allow-as-of-change"}
			},
			want: 0,
			check: func(t *testing.T, target, candidate string, stdout, stderr bytes.Buffer) {
				if stderr.Len() != 0 || !bytes.Contains(mustReadFile(t, target), []byte("as_of: \"2026-01-07\"")) {
					t.Fatalf("target = %q, stderr = %q", mustReadFile(t, target), stderr.String())
				}
				if _, err := os.Stat(candidate); !os.IsNotExist(err) {
					t.Fatalf("candidate still exists: %v", err)
				}
			},
		},
		"rejects existing target for absent expectation": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				return []string{"--expect-absent"}
			},
			want:  1,
			check: unchangedCandidateAndTarget,
		},
		"rejects invalid candidate": {
			prepare: func(t *testing.T, target, candidate string) []string {
				writeApplyFixture(t, target, filepath.Base(filepath.Dir(target)))
				writeApplyFixture(t, candidate, filepath.Base(filepath.Dir(target)))
				content, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(candidate, bytes.Replace(content, []byte("effort: 1.0"), []byte("effort: 0.25"), 1), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--expected-sha256", fileDigest(t, target)}
			},
			want:  1,
			check: unchangedCandidateAndTarget,
		},
	} {
		t.Run(name, func(t *testing.T) {
			epicDir := applyTestEpicDir(t)
			target := filepath.Join(epicDir, "schedule.yaml")
			candidate := filepath.Join(epicDir, ".schedule.yaml.candidate-test")
			args := []string{"apply-candidate", "--target", target, "--candidate", candidate}
			args = append(args, test.prepare(t, target, candidate)...)
			var beforeTarget, beforeCandidate []byte
			if test.want != 0 {
				beforeTarget = mustReadFile(t, target)
				beforeCandidate = mustReadFile(t, candidate)
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != test.want {
				t.Fatalf("run() = %d, want %d, stderr = %s", code, test.want, stderr.String())
			}
			if test.want != 0 {
				if got := mustReadFile(t, target); !bytes.Equal(got, beforeTarget) {
					t.Fatalf("target changed: got %q, want %q", got, beforeTarget)
				}
				if got := mustReadFile(t, candidate); !bytes.Equal(got, beforeCandidate) {
					t.Fatalf("candidate changed: got %q, want %q", got, beforeCandidate)
				}
			}
			test.check(t, target, candidate, stdout, stderr)
		})
	}
}

func TestApplyCandidateRejectsInvalidUsage(t *testing.T) {
	epicDir := applyTestEpicDir(t)
	target := filepath.Join(epicDir, "schedule.yaml")
	candidate := filepath.Join(epicDir, ".schedule.yaml.candidate-test")
	writeApplyFixture(t, candidate, filepath.Base(epicDir))
	otherEpicDir := applyTestEpicDir(t)
	otherCandidate := filepath.Join(otherEpicDir, ".schedule.yaml.candidate-test")
	writeApplyFixture(t, otherCandidate, filepath.Base(otherEpicDir))
	for _, args := range [][]string{
		{"apply-candidate", "--target", target, "--candidate", candidate},
		{"apply-candidate", "--target", target, "--candidate", candidate, "--expected-sha256", strings.Repeat("a", 64), "--expect-absent"},
		{"apply-candidate", "--target", target, "--candidate", candidate, "--expected-sha256", "invalid"},
		{"apply-candidate", "--target", target, "--candidate", candidate, "--expect-absent", "--allow-as-of-change"},
		{"apply-candidate", "--target", target, "--candidate", candidate, "--expect-absent", "--input", "unexpected"},
		{"apply-candidate", "--target", target, "--candidate", otherCandidate, "--expect-absent"},
		{"apply-candidate", "--target", target, "--candidate", filepath.Join(t.TempDir(), ".schedule.yaml.candidate-test"), "--expect-absent"},
		{"apply-candidate", "--target", target, "--candidate", filepath.Join(epicDir, ".wrong"), "--expect-absent"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) = %d, stderr = %s", args, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q", stdout.String())
		}
	}
}

func TestApplyCandidateUsesTargetRepositoryOutsideWorkingDirectory(t *testing.T) {
	epicDir := applyTestEpicDir(t)
	target := filepath.Join(epicDir, "schedule.yaml")
	candidate := filepath.Join(epicDir, ".schedule.yaml.candidate-test")
	writeApplyFixture(t, target, filepath.Base(epicDir))
	writeApplyFixture(t, candidate, filepath.Base(epicDir))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"apply-candidate",
		"--target", target,
		"--candidate", candidate,
		"--expected-sha256", fileDigest(t, target),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatalf("candidate still exists: %v", err)
	}
}

func TestApplyCandidateRejectsSymbolicLinks(t *testing.T) {
	epicDir := applyTestEpicDir(t)
	target := filepath.Join(epicDir, "schedule.yaml")
	candidate := filepath.Join(epicDir, ".schedule.yaml.candidate-test")
	writeApplyFixture(t, candidate, filepath.Base(epicDir))
	if err := os.Symlink(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), target); err != nil {
		t.Fatal(err)
	}
	assertApplyUsageError(t, []string{"apply-candidate", "--target", target, "--candidate", candidate, "--expect-absent"})

	root, err := repositoryRoot(epicDir)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "epics", "apply-link")
	if err := os.Symlink(epicDir, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	assertApplyUsageError(t, []string{
		"apply-candidate",
		"--target", filepath.Join(link, "schedule.yaml"),
		"--candidate", filepath.Join(link, ".schedule.yaml.candidate-test"),
		"--expect-absent",
	})
}

func assertApplyUsageError(t *testing.T, args []string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 2 {
		t.Fatalf("run(%q) = %d, stderr = %s", args, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func applyTestEpicDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "epics", "apply-test")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func writeApplyFixture(t *testing.T, path, epicID string) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Replace(fixture, []byte("id: TEST-42"), []byte("id: "+epicID), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	return fmt.Sprintf("%x", sha256.Sum256(mustReadFile(t, path)))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func unchangedCandidateAndTarget(t *testing.T, target, candidate string, stdout, stderr bytes.Buffer) {
	t.Helper()
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatal(err)
	}
}
