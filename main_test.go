package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"vcs/gossip"
)

func stubGitFns(
	t *testing.T,
	runFn func(args ...string) error,
	runOutputFn func(args ...string) (string, error),
) {
	t.Helper()
	oldRun := runGitFn
	oldRunOutput := runGitOutputFn

	if runFn != nil {
		runGitFn = runFn
	}
	if runOutputFn != nil {
		runGitOutputFn = runOutputFn
	}

	t.Cleanup(func() {
		runGitFn = oldRun
		runGitOutputFn = oldRunOutput
	})
}

func TestCmdStageDefaultsToDot(t *testing.T) {
	var got [][]string
	stubGitFns(t, func(args ...string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}, nil)

	if err := cmdStage(nil); err != nil {
		t.Fatalf("cmdStage returned error: %v", err)
	}

	want := []string{"add", "--", "."}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("unexpected git args: got %v want %v", got, want)
	}
}

func TestCmdUnstageFallsBackToReset(t *testing.T) {
	var got [][]string
	stubGitFns(t, func(args ...string) error {
		got = append(got, append([]string(nil), args...))
		if len(got) == 1 {
			return errors.New("restore unavailable")
		}
		return nil
	}, nil)

	if err := cmdUnstage([]string{"file.txt"}); err != nil {
		t.Fatalf("cmdUnstage returned error: %v", err)
	}

	want := [][]string{
		{"restore", "--staged", "--", "file.txt"},
		{"reset", "HEAD", "--", "file.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected git args: got %v want %v", got, want)
	}
}

func TestCmdCommitFlags(t *testing.T) {
	var got [][]string
	stubGitFns(t, func(args ...string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}, nil)

	err := cmdCommit([]string{"-a", "--allow-empty", "-m", "msg"})
	if err != nil {
		t.Fatalf("cmdCommit returned error: %v", err)
	}

	want := []string{"commit", "--all", "--allow-empty", "-m", "msg"}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("unexpected git args: got %v want %v", got, want)
	}
}

func TestCmdAmendDefaultsToNoEdit(t *testing.T) {
	var got [][]string
	stubGitFns(t, func(args ...string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}, nil)

	err := cmdAmend(nil)
	if err != nil {
		t.Fatalf("cmdAmend returned error: %v", err)
	}

	want := []string{"commit", "--amend", "--no-edit"}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("unexpected git args: got %v want %v", got, want)
	}
}

func TestCmdCheckoutRequiresTarget(t *testing.T) {
	stubGitFns(t, func(args ...string) error {
		return nil
	}, nil)

	err := cmdCheckout(nil)
	if err == nil || !strings.Contains(err.Error(), "usage: vcs checkout") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestCmdPushAndPullPassThroughArgs(t *testing.T) {
	var got [][]string
	stubGitFns(t, func(args ...string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}, nil)

	if err := cmdPush([]string{"origin", "main"}); err != nil {
		t.Fatalf("cmdPush returned error: %v", err)
	}
	if err := cmdPull([]string{"origin", "main"}); err != nil {
		t.Fatalf("cmdPull returned error: %v", err)
	}

	want := [][]string{
		{"push", "origin", "main"},
		{"pull", "origin", "main"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected git args: got %v want %v", got, want)
	}
}

func TestCmdSquashValidation(t *testing.T) {
	stubGitFns(t, func(args ...string) error {
		return nil
	}, func(args ...string) (string, error) {
		return "", nil
	})

	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{
			name:     "missing message",
			args:     []string{"--last", "2"},
			contains: "requires -m",
		},
		{
			name:     "both from and last",
			args:     []string{"--last", "2", "--from", "abc", "-m", "x"},
			contains: "exactly one",
		},
		{
			name:     "neither from nor last",
			args:     []string{"-m", "x"},
			contains: "exactly one",
		},
		{
			name:     "last too small",
			args:     []string{"--last", "1", "-m", "x"},
			contains: "--last must be >= 2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cmdSquash(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected error containing %q, got %v", tc.contains, err)
			}
		})
	}
}

func TestCmdSquashLastFlow(t *testing.T) {
	var gitCalls [][]string
	var outputCalls [][]string

	stubGitFns(t, func(args ...string) error {
		gitCalls = append(gitCalls, append([]string(nil), args...))
		return nil
	}, func(args ...string) (string, error) {
		outputCalls = append(outputCalls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
			return "", nil
		}
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--verify" {
			return "deadbeef", nil
		}
		return "", fmt.Errorf("unexpected runGitOutput call: %v", args)
	})

	err := cmdSquash([]string{"--last", "2", "-m", "squashed"})
	if err != nil {
		t.Fatalf("cmdSquash returned error: %v", err)
	}

	wantOutputCalls := [][]string{
		{"status", "--porcelain"},
		{"rev-parse", "--verify", "HEAD~2^{commit}"},
	}
	if !reflect.DeepEqual(outputCalls, wantOutputCalls) {
		t.Fatalf("unexpected output calls: got %v want %v", outputCalls, wantOutputCalls)
	}

	wantGitCalls := [][]string{
		{"reset", "--soft", "HEAD~2"},
		{"commit", "-m", "squashed"},
	}
	if !reflect.DeepEqual(gitCalls, wantGitCalls) {
		t.Fatalf("unexpected git calls: got %v want %v", gitCalls, wantGitCalls)
	}
}

func TestIntegrationCommandFlow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	stubGitFns(t, runGit, runGitOutput)

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	runInDir(t, root, "git", "init", "--bare", remoteDir)

	repo1 := filepath.Join(root, "repo1")
	if err := os.Mkdir(repo1, 0o755); err != nil {
		t.Fatalf("create repo1: %v", err)
	}

	t.Chdir(repo1)
	if err := cmdInit(nil); err != nil {
		t.Fatalf("cmdInit: %v", err)
	}
	runInDir(t, repo1, "git", "config", "user.name", "Test User")
	runInDir(t, repo1, "git", "config", "user.email", "test@example.com")
	runInDir(t, repo1, "git", "remote", "add", "origin", remoteDir)

	writeFile(t, filepath.Join(repo1, "notes.txt"), "one\n")
	if err := cmdStage([]string{"notes.txt"}); err != nil {
		t.Fatalf("cmdStage initial: %v", err)
	}
	if err := cmdCommit([]string{"-m", "first"}); err != nil {
		t.Fatalf("cmdCommit first: %v", err)
	}

	branch := strings.TrimSpace(outputInDir(t, repo1, "git", "symbolic-ref", "--short", "HEAD"))
	if branch == "" {
		t.Fatalf("could not resolve current branch")
	}

	if err := cmdPush([]string{"-u", "origin", branch}); err != nil {
		t.Fatalf("cmdPush initial: %v", err)
	}
	assertStoreHasOpType(t, repo1, gossip.OpTypeGitCommit)
	assertStoreHasOpType(t, repo1, gossip.OpTypeGitPush)

	writeFile(t, filepath.Join(repo1, "temp.txt"), "temp\n")
	if err := cmdStage([]string{"temp.txt"}); err != nil {
		t.Fatalf("cmdStage temp: %v", err)
	}
	if err := cmdUnstage([]string{"temp.txt"}); err != nil {
		t.Fatalf("cmdUnstage temp: %v", err)
	}
	cachedAfterUnstage := strings.TrimSpace(outputInDir(t, repo1, "git", "diff", "--cached", "--name-only"))
	if strings.Contains(cachedAfterUnstage, "temp.txt") {
		t.Fatalf("temp.txt should not be staged after unstage, got staged list: %q", cachedAfterUnstage)
	}
	if err := os.Remove(filepath.Join(repo1, "temp.txt")); err != nil {
		t.Fatalf("remove temp file: %v", err)
	}

	appendFile(t, filepath.Join(repo1, "notes.txt"), "two\n")
	if err := cmdStage([]string{"notes.txt"}); err != nil {
		t.Fatalf("cmdStage second: %v", err)
	}
	if err := cmdCommit([]string{"-m", "second"}); err != nil {
		t.Fatalf("cmdCommit second: %v", err)
	}
	if err := cmdAmend([]string{"--no-edit"}); err != nil {
		t.Fatalf("cmdAmend: %v", err)
	}

	appendFile(t, filepath.Join(repo1, "notes.txt"), "three\n")
	if err := cmdStage([]string{"notes.txt"}); err != nil {
		t.Fatalf("cmdStage third: %v", err)
	}
	if err := cmdCommit([]string{"-m", "third"}); err != nil {
		t.Fatalf("cmdCommit third: %v", err)
	}
	if err := cmdSquash([]string{"--last", "2", "-m", "squashed second+third"}); err != nil {
		t.Fatalf("cmdSquash: %v", err)
	}
	if err := cmdPush([]string{"origin", branch}); err != nil {
		t.Fatalf("cmdPush squashed: %v", err)
	}

	if err := cmdCheckout([]string{"-b", "feature"}); err != nil {
		t.Fatalf("cmdCheckout feature: %v", err)
	}
	writeFile(t, filepath.Join(repo1, "feature.txt"), "feature\n")
	if err := cmdStage([]string{"feature.txt"}); err != nil {
		t.Fatalf("cmdStage feature: %v", err)
	}
	if err := cmdCommit([]string{"-m", "feature commit"}); err != nil {
		t.Fatalf("cmdCommit feature: %v", err)
	}
	if err := cmdCheckout([]string{branch}); err != nil {
		t.Fatalf("cmdCheckout back: %v", err)
	}

	repo2 := filepath.Join(root, "repo2")
	runInDir(t, root, "git", "clone", remoteDir, repo2)
	runInDir(t, repo2, "git", "config", "user.name", "Test User")
	runInDir(t, repo2, "git", "config", "user.email", "test@example.com")

	t.Chdir(repo2)
	if err := cmdPull([]string{"origin", branch}); err != nil {
		t.Fatalf("cmdPull: %v", err)
	}
	assertStoreHasOpType(t, repo2, gossip.OpTypeGitPull)
	if err := cmdRevert([]string{"--no-commit", "HEAD"}); err != nil {
		t.Fatalf("cmdRevert --no-commit: %v", err)
	}
	if err := cmdCommit([]string{"-m", "revert head"}); err != nil {
		t.Fatalf("cmdCommit revert: %v", err)
	}

	commitCountText := strings.TrimSpace(outputInDir(t, repo2, "git", "rev-list", "--count", "HEAD"))
	commitCount, err := strconv.Atoi(commitCountText)
	if err != nil {
		t.Fatalf("parse commit count %q: %v", commitCountText, err)
	}
	if commitCount < 3 {
		t.Fatalf("expected at least 3 commits after flow, got %d", commitCount)
	}
}

func TestCmdPushPendingUntilCertified(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	stubGitFns(t, runGit, runGitOutput)

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	runInDir(t, root, "git", "init", "--bare", remoteDir)

	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Chdir(repo)

	if err := cmdInit(nil); err != nil {
		t.Fatalf("cmdInit: %v", err)
	}
	runInDir(t, repo, "git", "config", "user.name", "Test User")
	runInDir(t, repo, "git", "config", "user.email", "test@example.com")
	runInDir(t, repo, "git", "remote", "add", "origin", remoteDir)

	writeFile(t, filepath.Join(repo, "notes.txt"), "one\n")
	if err := cmdStage([]string{"notes.txt"}); err != nil {
		t.Fatalf("cmdStage: %v", err)
	}
	if err := cmdCommit([]string{"-m", "first"}); err != nil {
		t.Fatalf("cmdCommit: %v", err)
	}

	store, err := gossip.OpenStore(repo)
	if err != nil {
		t.Fatalf("open gossip store: %v", err)
	}
	_, err = store.SaveConsensusConfig(gossip.ConsensusConfig{
		Threshold: 0.5,
		Members:   []string{store.NodeID(), "peer-node-that-has-not-voted"},
	})
	if err != nil {
		t.Fatalf("save consensus config: %v", err)
	}

	branch := strings.TrimSpace(outputInDir(t, repo, "git", "symbolic-ref", "--short", "HEAD"))
	if branch == "" {
		t.Fatalf("expected non-empty branch")
	}

	if err := cmdPush([]string{"origin", branch}); err != nil {
		t.Fatalf("cmdPush should mark pending, got error: %v", err)
	}

	remoteHead := strings.TrimSpace(outputInDir(t, root, "git", "ls-remote", remoteDir, "refs/heads/"+branch))
	if remoteHead != "" {
		t.Fatalf("expected no remote branch update while pending, got: %q", remoteHead)
	}

	storeAfterPush, err := gossip.OpenStore(repo)
	if err != nil {
		t.Fatalf("reopen gossip store: %v", err)
	}

	pushes, err := storeAfterPush.ListPendingPushes()
	if err != nil {
		t.Fatalf("list pending pushes: %v", err)
	}
	if len(pushes) != 1 {
		t.Fatalf("expected 1 pending push, got %d", len(pushes))
	}
	if pushes[0].Status != gossip.PendingPushStatusPending {
		t.Fatalf("expected pending status, got %s", pushes[0].Status)
	}

	status, err := storeAfterPush.ProposalStatus(pushes[0].ProposalID)
	if err != nil {
		t.Fatalf("proposal status: %v", err)
	}
	if status.HasQuorum {
		t.Fatalf("expected proposal to lack quorum")
	}
}

func runInDir(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v in %s failed: %v\noutput:\n%s", name, args, dir, err, output)
	}
}

func outputInDir(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v in %s failed: %v\noutput:\n%s", name, args, dir, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path string, contents string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s for append: %v", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(contents); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func assertStoreHasOpType(t *testing.T, repoRoot string, opType string) {
	t.Helper()
	store, err := gossip.OpenStore(repoRoot)
	if err != nil {
		t.Fatalf("open gossip store %s: %v", repoRoot, err)
	}
	ops := store.Ops(0)
	for _, op := range ops {
		if op.Type == opType {
			return
		}
	}
	t.Fatalf("expected op type %s in %s", opType, repoRoot)
}
