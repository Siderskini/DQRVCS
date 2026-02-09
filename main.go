package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vcs/gossip"
)

var runGitFn = runGit
var runGitOutputFn = runGitOutput

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	var err error
	switch command {
	case "help", "-h", "--help":
		printUsage()
		return
	case "init":
		err = cmdInit(args)
	case "status":
		err = cmdStatus(args)
	case "log":
		err = cmdLog(args)
	case "branch":
		err = cmdBranch(args)
	case "checkout":
		err = cmdCheckout(args)
	case "stage":
		err = cmdStage(args)
	case "unstage":
		err = cmdUnstage(args)
	case "commit":
		err = cmdCommit(args)
	case "amend":
		err = cmdAmend(args)
	case "revert":
		err = cmdRevert(args)
	case "push":
		err = cmdPush(args)
	case "pull":
		err = cmdPull(args)
	case "squash":
		err = cmdSquash(args)
	case "peer":
		err = cmdPeer(args)
	case "op":
		err = cmdOp(args)
	case "sync":
		err = cmdSync(args)
	case "daemon":
		err = cmdDaemon(args)
	case "consensus":
		err = cmdConsensus(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`vcs: minimal Git-compatible CLI

Usage:
  vcs <command> [options]

Core commands:
  init                        Initialize a repository
  status                      Show working tree status
  log [args...]               Show commit logs
  branch [args...]            List/create/delete branches
  checkout <target>           Switch branch or restore files
  stage [path ...]            Stage files (defaults to ".")
  unstage [path ...]          Unstage files (defaults to ".")
  commit [-a] [-m msg]        Create a commit
  amend [--all] [--no-edit]   Amend last commit
  revert [--no-commit] <ref>  Revert a commit
  push [args...]              Push to remote (auto-proposal + pending workflow)
    --process-pending         Try applying certified pending pushes
    --list-pending            Show pending push queue
    --no-auto-proposal        Bypass consensus workflow and run raw git push
  pull [args...]              Pull from remote

Decentralized gossip:
  daemon [flags]              Run sync daemon + gossip loop
  peer add <url>              Add a gossip peer
  peer remove <url>           Remove a gossip peer
  peer list                   List configured peers
  sync [--peer URL]           Sync operation log with peers now
  op append --type T --data J Add a signed local operation
  op list [--limit N]         List recent known operations
  consensus <subcommand>      Proposal/vote/cert workflow

History rewrite:
  squash --last N -m msg
  squash --from <commit> -m msg
    --last N      Squash last N commits into one (N >= 2)
    --from COMMIT Squash everything after COMMIT into one
    --allow-dirty Allow squash even with uncommitted changes

Examples:
  vcs stage .
  vcs commit -m "initial commit"
  vcs amend --no-edit
  vcs squash --last 3 -m "refactor auth flow"
  vcs push origin main
  vcs push --process-pending
  vcs peer add http://127.0.0.1:8787
  vcs daemon --listen 127.0.0.1:8787 --interval 10s
  vcs consensus propose --ref refs/heads/main --new <oid>`)
}

func cmdInit(args []string) error {
	gitArgs := append([]string{"init"}, args...)
	return runGitFn(gitArgs...)
}

func cmdStatus(args []string) error {
	gitArgs := append([]string{"status"}, args...)
	return runGitFn(gitArgs...)
}

func cmdLog(args []string) error {
	gitArgs := append([]string{"log"}, args...)
	return runGitFn(gitArgs...)
}

func cmdBranch(args []string) error {
	gitArgs := append([]string{"branch"}, args...)
	return runGitFn(gitArgs...)
}

func cmdCheckout(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: vcs checkout <target> [additional git checkout args]")
	}
	gitArgs := append([]string{"checkout"}, args...)
	return runGitFn(gitArgs...)
}

func cmdStage(args []string) error {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}
	gitArgs := append([]string{"add", "--"}, paths...)
	return runGitFn(gitArgs...)
}

func cmdUnstage(args []string) error {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	restoreArgs := append([]string{"restore", "--staged", "--"}, paths...)
	if err := runGitFn(restoreArgs...); err == nil {
		return nil
	}

	resetArgs := append([]string{"reset", "HEAD", "--"}, paths...)
	if err := runGitFn(resetArgs...); err == nil {
		return nil
	}

	return errors.New("could not unstage paths (both `git restore --staged` and `git reset HEAD` failed)")
}

func cmdCommit(args []string) error {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	message := fs.String("m", "", "commit message")
	all := fs.Bool("a", false, "stage tracked files before committing")
	allowEmpty := fs.Bool("allow-empty", false, "create an empty commit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	gitArgs := []string{"commit"}
	if *all {
		gitArgs = append(gitArgs, "--all")
	}
	if *allowEmpty {
		gitArgs = append(gitArgs, "--allow-empty")
	}
	if *message != "" {
		gitArgs = append(gitArgs, "-m", *message)
	}

	if err := runGitFn(gitArgs...); err != nil {
		return err
	}

	payload := map[string]any{
		"args": args,
	}
	if *message != "" {
		payload["message"] = *message
	}
	if *all {
		payload["all"] = true
	}
	if *allowEmpty {
		payload["allow_empty"] = true
	}
	if commit := strings.TrimSpace(tryGitOutput("rev-parse", "HEAD")); commit != "" {
		payload["commit"] = commit
	}
	if branch := strings.TrimSpace(tryGitOutput("rev-parse", "--abbrev-ref", "HEAD")); branch != "" {
		payload["branch"] = branch
	}
	recordGitEvent(gossip.OpTypeGitCommit, payload)
	return nil
}

func cmdAmend(args []string) error {
	fs := flag.NewFlagSet("amend", flag.ContinueOnError)
	message := fs.String("m", "", "new commit message")
	noEdit := fs.Bool("no-edit", true, "reuse previous commit message")
	all := fs.Bool("all", false, "stage tracked files before amending")
	allowEmpty := fs.Bool("allow-empty", false, "allow amending to an empty commit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	gitArgs := []string{"commit", "--amend"}
	if *all {
		gitArgs = append(gitArgs, "--all")
	}
	if *allowEmpty {
		gitArgs = append(gitArgs, "--allow-empty")
	}

	if *message != "" {
		gitArgs = append(gitArgs, "-m", *message)
	} else if *noEdit {
		gitArgs = append(gitArgs, "--no-edit")
	}

	return runGitFn(gitArgs...)
}

func cmdRevert(args []string) error {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	noCommit := fs.Bool("no-commit", false, "apply revert without committing")

	if err := fs.Parse(args); err != nil {
		return err
	}

	refs := fs.Args()
	if len(refs) != 1 {
		return errors.New("usage: vcs revert [--no-commit] <commit>")
	}

	gitArgs := []string{"revert"}
	if *noCommit {
		gitArgs = append(gitArgs, "--no-commit")
	}
	gitArgs = append(gitArgs, refs[0])
	return runGitFn(gitArgs...)
}

func cmdPush(args []string) error {
	processPendingOnly, remaining := consumeExactFlag(args, "--process-pending")
	listPendingOnly, remaining := consumeExactFlag(remaining, "--list-pending")
	noAutoProposal, gitArgs := consumeExactFlag(remaining, "--no-auto-proposal")

	store, err := openGossipStore()
	if err != nil {
		if processPendingOnly || listPendingOnly {
			return err
		}
		// Fall back to raw git push outside a repository-aware context.
		if noAutoProposal || strings.Contains(err.Error(), "inside a git repository") {
			return runGitFn(append([]string{"push"}, gitArgs...)...)
		}
		return err
	}

	if listPendingOnly {
		return printPendingPushes(store)
	}

	if processPendingOnly {
		result, processErr := processPendingPushes(store, "")
		fmt.Printf(
			"pending pushes processed: checked=%d executed=%d pending=%d failed=%d\n",
			result.Checked,
			result.Executed,
			result.Pending,
			result.Failed,
		)
		return processErr
	}

	if noAutoProposal {
		gitPushArgs := append([]string{"push"}, gitArgs...)
		if err := runGitFn(gitPushArgs...); err != nil {
			return err
		}
		payload := map[string]any{
			"args": gitArgs,
		}
		if head := strings.TrimSpace(tryGitOutput("rev-parse", "HEAD")); head != "" {
			payload["head"] = head
		}
		if branch := strings.TrimSpace(tryGitOutput("rev-parse", "--abbrev-ref", "HEAD")); branch != "" {
			payload["branch"] = branch
		}
		payload["mode"] = "immediate"
		recordGitEvent(gossip.OpTypeGitPush, payload)
		return nil
	}

	intent, err := resolvePushIntent(gitArgs)
	if err != nil {
		return err
	}

	op, proposal, err := store.ProposeRefUpdate(gossip.ProposeRefInput{
		Ref:    intent.TargetRef,
		OldOID: intent.OldOID,
		NewOID: intent.NewOID,
		Epoch:  0,
		TTL:    24 * time.Hour,
	})
	if err != nil {
		return err
	}
	if _, _, err := store.CastVote(proposal.ProposalID, "yes"); err != nil {
		return fmt.Errorf("auto-vote for proposal %s failed: %w", proposal.ProposalID, err)
	}

	pending, err := store.UpsertPendingPush(gossip.PendingPush{
		ProposalID: proposal.ProposalID,
		Remote:     intent.Remote,
		SourceRef:  intent.SourceRef,
		TargetRef:  intent.TargetRef,
		NewOID:     intent.NewOID,
		GitArgs:    append([]string(nil), gitArgs...),
		Status:     gossip.PendingPushStatusPending,
	})
	if err != nil {
		return err
	}

	result, processErr := processPendingPushes(store, proposal.ProposalID)
	if processErr != nil {
		return processErr
	}
	if result.Executed > 0 {
		fmt.Printf(
			"push applied proposal=%s ref=%s old=%s new=%s op=%s\n",
			proposal.ProposalID,
			proposal.Ref,
			proposal.OldOID,
			proposal.NewOID,
			op.ID,
		)
		return nil
	}

	status, statusErr := store.ProposalStatus(proposal.ProposalID)
	if statusErr != nil {
		return statusErr
	}

	fmt.Printf(
		"push pending proposal=%s ref=%s new=%s yes=%d/%d required=%d\n",
		pending.ProposalID,
		pending.TargetRef,
		pending.NewOID,
		len(status.YesVoters),
		len(status.Members),
		status.RequiredYes,
	)
	fmt.Println("run `vcs sync` or `vcs push --process-pending` after additional votes are gossiped")
	return nil
}

func cmdPull(args []string) error {
	gitArgs := append([]string{"pull"}, args...)
	before := strings.TrimSpace(tryGitOutput("rev-parse", "HEAD"))
	if err := runGitFn(gitArgs...); err != nil {
		return err
	}
	after := strings.TrimSpace(tryGitOutput("rev-parse", "HEAD"))
	payload := map[string]any{
		"args": args,
	}
	if before != "" {
		payload["head_before"] = before
	}
	if after != "" {
		payload["head_after"] = after
	}
	if branch := strings.TrimSpace(tryGitOutput("rev-parse", "--abbrev-ref", "HEAD")); branch != "" {
		payload["branch"] = branch
	}
	recordGitEvent(gossip.OpTypeGitPull, payload)
	return nil
}

type pushIntent struct {
	Remote       string
	SourceRef    string
	TargetRef    string
	TargetBranch string
	NewOID       string
	OldOID       string
}

type pendingPushProcessResult struct {
	Checked  int
	Executed int
	Pending  int
	Failed   int
}

func resolvePushIntent(gitArgs []string) (pushIntent, error) {
	currentBranch := strings.TrimSpace(tryGitOutput("rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch == "" || currentBranch == "HEAD" {
		return pushIntent{}, errors.New("push requires a branch checkout (detached HEAD not supported for auto-proposal)")
	}

	upstream := strings.TrimSpace(tryGitOutput("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
	upstreamRemote, upstreamBranch := parseUpstream(upstream)

	positionals := extractPushPositionals(gitArgs)
	remote := upstreamRemote
	if remote == "" {
		remote = "origin"
	}
	if len(positionals) >= 1 {
		remote = strings.TrimSpace(positionals[0])
	}
	if remote == "" {
		return pushIntent{}, errors.New("could not determine push remote")
	}

	refspec := ""
	if len(positionals) >= 2 {
		refspec = strings.TrimSpace(positionals[1])
	}

	sourceSpec := currentBranch
	targetSpec := currentBranch
	if refspec != "" {
		if strings.Contains(refspec, ":") {
			parts := strings.SplitN(refspec, ":", 2)
			sourceSpec = strings.TrimSpace(parts[0])
			targetSpec = strings.TrimSpace(parts[1])
			if sourceSpec == "" {
				sourceSpec = currentBranch
			}
			if targetSpec == "" {
				targetSpec = sourceSpec
			}
		} else {
			sourceSpec = refspec
			targetSpec = refspec
		}
	} else if upstreamBranch != "" {
		targetSpec = upstreamBranch
	}

	sourceRef := normalizeSourceRef(sourceSpec, currentBranch)
	targetRef, targetBranch := normalizeTargetRef(targetSpec, currentBranch)
	if targetRef == "" {
		return pushIntent{}, errors.New("could not resolve target ref for push")
	}

	newOID := strings.TrimSpace(tryGitOutput("rev-parse", sourceRef))
	if newOID == "" {
		return pushIntent{}, fmt.Errorf("could not resolve source ref %q for push", sourceRef)
	}

	oldOID := ""
	if targetBranch != "" {
		oldOID = strings.TrimSpace(tryGitOutput("rev-parse", "refs/remotes/"+remote+"/"+targetBranch))
	}

	return pushIntent{
		Remote:       remote,
		SourceRef:    sourceRef,
		TargetRef:    targetRef,
		TargetBranch: targetBranch,
		NewOID:       newOID,
		OldOID:       oldOID,
	}, nil
}

func parseUpstream(upstream string) (string, string) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", ""
	}

	if strings.HasPrefix(upstream, "refs/remotes/") {
		trimmed := strings.TrimPrefix(upstream, "refs/remotes/")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return trimmed, ""
	}

	parts := strings.SplitN(upstream, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", upstream
}

func normalizeSourceRef(spec string, currentBranch string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "HEAD" {
		return currentBranch
	}
	return spec
}

func normalizeTargetRef(spec string, currentBranch string) (string, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "HEAD" {
		spec = currentBranch
	}
	if spec == "" {
		return "", ""
	}
	if strings.HasPrefix(spec, "refs/heads/") {
		return spec, strings.TrimPrefix(spec, "refs/heads/")
	}
	if strings.HasPrefix(spec, "refs/") {
		return spec, ""
	}
	if strings.HasPrefix(spec, "heads/") {
		branch := strings.TrimPrefix(spec, "heads/")
		return "refs/heads/" + branch, branch
	}
	return "refs/heads/" + spec, spec
}

func extractPushPositionals(args []string) []string {
	longWithValue := map[string]struct{}{
		"--repo":         {},
		"--receive-pack": {},
		"--exec":         {},
		"--upload-pack":  {},
		"--push-option":  {},
	}
	shortWithValue := map[string]struct{}{
		"-c": {},
		"-o": {},
	}

	positionals := make([]string, 0, len(args))
	expectValue := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if expectValue {
			expectValue = false
			continue
		}
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}

		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}
			if _, ok := longWithValue[arg]; ok {
				expectValue = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			if _, ok := shortWithValue[arg]; ok {
				expectValue = true
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return positionals
}

func printPendingPushes(store *gossip.Store) error {
	pushes, err := store.ListPendingPushes()
	if err != nil {
		return err
	}
	if len(pushes) == 0 {
		fmt.Println("no pending pushes")
		return nil
	}
	for _, push := range pushes {
		fmt.Printf(
			"proposal=%s status=%s remote=%s target=%s new=%s attempts=%d\n",
			push.ProposalID,
			push.Status,
			push.Remote,
			push.TargetRef,
			push.NewOID,
			push.Attempts,
		)
		if strings.TrimSpace(push.LastError) != "" {
			fmt.Printf("  last_error=%s\n", push.LastError)
		}
	}
	return nil
}

func processPendingPushes(store *gossip.Store, onlyProposalID string) (pendingPushProcessResult, error) {
	var result pendingPushProcessResult

	pushes, err := store.ListPendingPushes()
	if err != nil {
		return result, err
	}

	var firstErr error
	for _, push := range pushes {
		if onlyProposalID != "" && push.ProposalID != onlyProposalID {
			continue
		}
		if push.Status == gossip.PendingPushStatusCompleted {
			continue
		}

		result.Checked++
		status, statusErr := store.ProposalStatus(push.ProposalID)
		if statusErr != nil {
			result.Failed++
			_ = store.MarkPendingPushFailed(push.ProposalID, statusErr.Error())
			if firstErr == nil {
				firstErr = statusErr
			}
			continue
		}
		if status.Expired {
			result.Failed++
			expiredErr := errors.New("proposal has expired")
			_ = store.MarkPendingPushFailed(push.ProposalID, expiredErr.Error())
			if firstErr == nil {
				firstErr = expiredErr
			}
			continue
		}

		if !status.Certified {
			if !status.HasQuorum {
				result.Pending++
				waiting := fmt.Sprintf(
					"awaiting quorum yes=%d/%d required=%d",
					len(status.YesVoters),
					len(status.Members),
					status.RequiredYes,
				)
				_ = store.MarkPendingPushPending(push.ProposalID, waiting)
				continue
			}
			if _, _, certErr := store.CertifyProposal(push.ProposalID, false); certErr != nil {
				result.Failed++
				_ = store.MarkPendingPushFailed(push.ProposalID, certErr.Error())
				if firstErr == nil {
					firstErr = certErr
				}
				continue
			}
		}

		pushArgs := append([]string{"push"}, push.GitArgs...)
		if pushErr := runGitFn(pushArgs...); pushErr != nil {
			result.Failed++
			_ = store.MarkPendingPushFailed(push.ProposalID, pushErr.Error())
			if firstErr == nil {
				firstErr = pushErr
			}
			continue
		}

		result.Executed++
		_ = store.MarkPendingPushCompleted(push.ProposalID)
		recordGitEvent(gossip.OpTypeGitPush, map[string]any{
			"args":        push.GitArgs,
			"proposal_id": push.ProposalID,
			"remote":      push.Remote,
			"target_ref":  push.TargetRef,
			"new_oid":     push.NewOID,
			"mode":        "certified",
		})
	}

	return result, firstErr
}

func cmdPeer(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: vcs peer <add|remove|list> [peer-url]")
	}

	store, err := openGossipStore()
	if err != nil {
		return err
	}

	sub := args[0]
	switch sub {
	case "add":
		if len(args) != 2 {
			return errors.New("usage: vcs peer add <peer-url>")
		}
		peer, err := store.AddPeer(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("added peer %s\n", peer)
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: vcs peer remove <peer-url>")
		}
		peer, err := store.RemovePeer(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("removed peer %s\n", peer)
		return nil
	case "list":
		if len(args) != 1 {
			return errors.New("usage: vcs peer list")
		}
		peers, err := store.ListPeers()
		if err != nil {
			return err
		}
		if len(peers) == 0 {
			fmt.Println("no peers configured")
			return nil
		}
		for _, peer := range peers {
			fmt.Println(peer)
		}
		return nil
	default:
		return fmt.Errorf("unknown peer subcommand %q", sub)
	}
}

func cmdOp(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: vcs op <append|list> [options]")
	}

	store, err := openGossipStore()
	if err != nil {
		return err
	}

	switch args[0] {
	case "append":
		fs := flag.NewFlagSet("op append", flag.ContinueOnError)
		opType := fs.String("type", "", "operation type (required)")
		data := fs.String("data", "{}", "operation payload JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		if strings.TrimSpace(*opType) == "" {
			return errors.New("op append requires --type")
		}
		payload := json.RawMessage(*data)
		if !json.Valid(payload) {
			return errors.New("op append --data must be valid JSON")
		}
		op, err := store.AppendLocalOp(*opType, payload)
		if err != nil {
			return err
		}
		fmt.Printf("appended op id=%s type=%s author=%s seq=%d\n", op.ID, op.Type, op.Author, op.Seq)
		return nil

	case "list":
		fs := flag.NewFlagSet("op list", flag.ContinueOnError)
		limit := fs.Int("limit", 20, "max operations to display")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}

		ops := store.Ops(*limit)
		if len(ops) == 0 {
			fmt.Println("no operations found")
			return nil
		}
		for _, op := range ops {
			fmt.Printf("%s seq=%d type=%s id=%s ts=%s\n", op.Author, op.Seq, op.Type, op.ID, op.Timestamp)
		}
		return nil

	default:
		return fmt.Errorf("unknown op subcommand %q", args[0])
	}
}

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	peer := fs.String("peer", "", "sync only a single peer URL")
	limit := fs.Int("limit", 256, "max operations per sync response")
	rounds := fs.Int("rounds", 6, "max anti-entropy rounds per peer")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	store, err := openGossipStore()
	if err != nil {
		return err
	}

	var peers []string
	if strings.TrimSpace(*peer) != "" {
		peers = []string{*peer}
	} else {
		peers, err = store.ListPeers()
		if err != nil {
			return err
		}
	}
	if len(peers) == 0 {
		return errors.New("no peers configured (use `vcs peer add <url>` or pass --peer)")
	}

	var firstErr error
	for _, p := range peers {
		stats, syncErr := gossip.SyncPeer(context.Background(), store, p, *limit, *rounds, nil)
		if syncErr != nil {
			if firstErr == nil {
				firstErr = syncErr
			}
			fmt.Fprintf(os.Stderr, "peer=%s sync failed: %v\n", p, syncErr)
			continue
		}
		fmt.Printf(
			"peer=%s rounds=%d sent=%d pulled=%d accepted=%d rejected=%d dropped=%d\n",
			stats.Peer,
			stats.Rounds,
			stats.Sent,
			stats.Pulled,
			stats.Accepted,
			stats.Rejected,
			stats.Dropped,
		)
	}

	pendingResult, pendingErr := processPendingPushes(store, "")
	if pendingResult.Checked > 0 {
		fmt.Printf(
			"pending pushes: checked=%d executed=%d pending=%d failed=%d\n",
			pendingResult.Checked,
			pendingResult.Executed,
			pendingResult.Pending,
			pendingResult.Failed,
		)
	}
	if firstErr == nil && pendingErr != nil {
		firstErr = pendingErr
	}
	return firstErr
}

func cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8787", "listen address")
	interval := fs.Duration("interval", 15*time.Second, "periodic gossip interval")
	limit := fs.Int("limit", 256, "max operations per sync response")
	rounds := fs.Int("rounds", 6, "max anti-entropy rounds per peer")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	store, err := openGossipStore()
	if err != nil {
		return err
	}

	identity := store.IdentityPublic()
	fmt.Printf("node=%s listen=%s interval=%s\n", identity.NodeID, *listen, interval.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	return gossip.RunDaemon(ctx, store, gossip.DaemonConfig{
		ListenAddr:     *listen,
		GossipInterval: *interval,
		SyncLimit:      *limit,
		MaxSyncRounds:  *rounds,
		Logger:         logger,
	})
}

func cmdConsensus(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: vcs consensus <propose|vote|certify|status|list|config> [options]")
	}

	store, err := openGossipStore()
	if err != nil {
		return err
	}

	switch args[0] {
	case "propose":
		fs := flag.NewFlagSet("consensus propose", flag.ContinueOnError)
		ref := fs.String("ref", "", "ref being proposed (default current branch ref)")
		oldOID := fs.String("old", "", "current/old OID")
		newOID := fs.String("new", "", "proposed new OID (default HEAD)")
		epoch := fs.Uint64("epoch", 0, "membership epoch")
		ttl := fs.Duration("ttl", 24*time.Hour, "proposal TTL")
		proposalID := fs.String("id", "", "explicit proposal ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}

		resolvedRef := strings.TrimSpace(*ref)
		if resolvedRef == "" {
			resolvedRef = strings.TrimSpace(tryGitOutput("symbolic-ref", "-q", "HEAD"))
		}
		if resolvedRef == "" {
			return errors.New("could not determine ref; pass --ref explicitly")
		}

		resolvedNew := strings.TrimSpace(*newOID)
		if resolvedNew == "" {
			resolvedNew = strings.TrimSpace(tryGitOutput("rev-parse", "HEAD"))
		}
		if resolvedNew == "" {
			return errors.New("could not determine new OID; pass --new explicitly")
		}

		resolvedOld := strings.TrimSpace(*oldOID)
		if resolvedOld == "" {
			resolvedOld = strings.TrimSpace(tryGitOutput("rev-parse", resolvedRef))
		}

		op, payload, err := store.ProposeRefUpdate(gossip.ProposeRefInput{
			ProposalID: *proposalID,
			Ref:        resolvedRef,
			OldOID:     resolvedOld,
			NewOID:     resolvedNew,
			Epoch:      *epoch,
			TTL:        *ttl,
		})
		if err != nil {
			return err
		}
		fmt.Printf(
			"proposal=%s ref=%s old=%s new=%s epoch=%d op=%s\n",
			payload.ProposalID,
			payload.Ref,
			payload.OldOID,
			payload.NewOID,
			payload.Epoch,
			op.ID,
		)
		return nil

	case "vote":
		fs := flag.NewFlagSet("consensus vote", flag.ContinueOnError)
		proposalID := fs.String("proposal", "", "proposal ID")
		yes := fs.Bool("yes", false, "vote yes")
		no := fs.Bool("no", false, "vote no")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		if strings.TrimSpace(*proposalID) == "" {
			return errors.New("consensus vote requires --proposal")
		}
		if *yes == *no {
			return errors.New("consensus vote requires exactly one of --yes or --no")
		}
		decision := "no"
		if *yes {
			decision = "yes"
		}
		op, payload, err := store.CastVote(*proposalID, decision)
		if err != nil {
			return err
		}
		fmt.Printf("proposal=%s vote=%s epoch=%d op=%s\n", payload.ProposalID, payload.Decision, payload.Epoch, op.ID)
		return nil

	case "certify":
		fs := flag.NewFlagSet("consensus certify", flag.ContinueOnError)
		proposalID := fs.String("proposal", "", "proposal ID")
		force := fs.Bool("force", false, "allow cert operation without quorum")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		if strings.TrimSpace(*proposalID) == "" {
			return errors.New("consensus certify requires --proposal")
		}
		op, cert, err := store.CertifyProposal(*proposalID, *force)
		if err != nil {
			return err
		}
		fmt.Printf(
			"proposal=%s certified=%t yes=%d/%d required=%d op=%s\n",
			cert.ProposalID,
			cert.Certified,
			len(cert.YesVoters),
			cert.TotalVoters,
			cert.RequiredYes,
			op.ID,
		)
		return nil

	case "status":
		fs := flag.NewFlagSet("consensus status", flag.ContinueOnError)
		proposalID := fs.String("proposal", "", "proposal ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		if strings.TrimSpace(*proposalID) == "" {
			return errors.New("consensus status requires --proposal")
		}
		status, err := store.ProposalStatus(*proposalID)
		if err != nil {
			return err
		}
		fmt.Printf(
			"proposal=%s ref=%s old=%s new=%s epoch=%d quorum=%t certified=%t expired=%t\n",
			status.Proposal.ProposalID,
			status.Proposal.Ref,
			status.Proposal.OldOID,
			status.Proposal.NewOID,
			status.Proposal.Epoch,
			status.HasQuorum,
			status.Certified,
			status.Expired,
		)
		fmt.Printf(
			"threshold=%.2f voters=%d yes=%d no=%d required_yes=%d\n",
			status.Threshold,
			len(status.Members),
			len(status.YesVoters),
			len(status.NoVoters),
			status.RequiredYes,
		)
		return nil

	case "list":
		fs := flag.NewFlagSet("consensus list", flag.ContinueOnError)
		limit := fs.Int("limit", 20, "max proposals to list")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		proposals := store.ProposalSummaries(*limit)
		if len(proposals) == 0 {
			fmt.Println("no proposals found")
			return nil
		}
		for _, proposal := range proposals {
			status, err := store.ProposalStatus(proposal.ProposalID)
			if err != nil {
				return err
			}
			fmt.Printf(
				"%s ref=%s new=%s epoch=%d quorum=%t certified=%t yes=%d/%d\n",
				proposal.ProposalID,
				proposal.Ref,
				proposal.NewOID,
				proposal.Epoch,
				status.HasQuorum,
				status.Certified,
				len(status.YesVoters),
				len(status.Members),
			)
		}
		return nil

	case "config":
		fs := flag.NewFlagSet("consensus config", flag.ContinueOnError)
		threshold := fs.Float64("threshold", -1, "set threshold in [0,1)")
		membersCSV := fs.String("members", "", "comma-separated member node IDs")
		clearMembers := fs.Bool("clear-members", false, "clear explicit member list")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}

		cfg, err := store.ConsensusConfig()
		if err != nil {
			return err
		}

		changed := false
		if *threshold >= 0 {
			cfg.Threshold = *threshold
			changed = true
		}
		if *clearMembers {
			cfg.Members = nil
			changed = true
		}
		if strings.TrimSpace(*membersCSV) != "" {
			cfg.Members = parseCSV(*membersCSV)
			changed = true
		}
		if changed {
			cfg, err = store.SaveConsensusConfig(cfg)
			if err != nil {
				return err
			}
		}

		memberText := "(auto-discovered)"
		if len(cfg.Members) > 0 {
			memberText = strings.Join(cfg.Members, ",")
		}
		fmt.Printf("threshold=%.2f members=%s\n", cfg.Threshold, memberText)
		return nil

	default:
		return fmt.Errorf("unknown consensus subcommand %q", args[0])
	}
}

func cmdSquash(args []string) error {
	fs := flag.NewFlagSet("squash", flag.ContinueOnError)
	last := fs.Int("last", 0, "squash last N commits")
	from := fs.String("from", "", "base commit to keep")
	message := fs.String("m", "", "message for squashed commit")
	allowDirty := fs.Bool("allow-dirty", false, "allow squash with uncommitted changes")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *message == "" {
		return errors.New("squash requires -m <message>")
	}

	usingLast := *last > 0
	usingFrom := strings.TrimSpace(*from) != ""
	if usingLast == usingFrom {
		return errors.New("use exactly one of --last N or --from <commit>")
	}

	if !*allowDirty {
		clean, err := workingTreeClean()
		if err != nil {
			return err
		}
		if !clean {
			return errors.New("working tree is not clean; commit/stash changes or use --allow-dirty")
		}
	}

	var base string
	if usingLast {
		if *last < 2 {
			return errors.New("--last must be >= 2")
		}
		base = "HEAD~" + strconv.Itoa(*last)
	} else {
		base = *from
	}

	if err := verifyCommit(base); err != nil {
		return err
	}

	if err := runGitFn("reset", "--soft", base); err != nil {
		return fmt.Errorf("soft reset failed: %w", err)
	}
	if err := runGitFn("commit", "-m", *message); err != nil {
		return fmt.Errorf("squash commit failed: %w", err)
	}

	return nil
}

func verifyCommit(ref string) error {
	_, err := runGitOutputFn("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return fmt.Errorf("invalid commit reference %q", ref)
	}
	return nil
}

func workingTreeClean() (bool, error) {
	out, err := runGitOutputFn("status", "--porcelain")
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Ignore local decentralized metadata to avoid blocking history actions.
		if strings.HasPrefix(line, "?? ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "?? "))
			if path == ".vcs" || strings.HasPrefix(path, ".vcs/") {
				continue
			}
		}
		return false, nil
	}
	return true, nil
}

func openGossipStore() (*gossip.Store, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	return gossip.OpenStore(root)
}

func repoRoot() (string, error) {
	out, err := runGitOutputFn("rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("decentralized commands must run inside a git repository")
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", errors.New("could not determine repository root")
	}
	return root, nil
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func consumeExactFlag(args []string, flag string) (bool, []string) {
	found := false
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == flag {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return found, out
}

func tryGitOutput(args ...string) string {
	out, err := runGitOutputFn(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func recordGitEvent(opType string, payload map[string]any) {
	root, err := repoRoot()
	if err != nil {
		return
	}
	store, err := gossip.OpenStore(root)
	if err != nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = store.AppendLocalOp(opType, raw)
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runGitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), errText)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
