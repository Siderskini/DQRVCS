# Quantum Resistant Decentralized Version Control System

Disclaimer: Almost everything in this project was written or built by Codex. The only sections of this codebase that have been created by hand are the parts of this README above the Current MVP Implementation section, and everything in the `about` directory.

## Purpose
Today I learned that there's no commercially available git-like version control system that is both decentralized and resistant to quantum attacks.
I also learned that there is a Go implementation of the NIST-approved Quantum Resistant Ledger (QRL) and that there are git-like version control systems that are decentralized.
I believe that the obvious next step is to implement a command line tool which behaves like git as far as the VCS user is concerned, but is backed by a QRL and decentralized.

I think something that would be nice to have is a nice desktop application built in maybe Rust to use the tool.
Another nice to have is some support for git-lfs.

The key benefits to this kind of system include:

- Democratization of development decisions
- Security of the data against quantum attacks

## Plan
1. Make a Go implementation of git. As an experienced git user, I believe that this will not be too difficult.
2. Test the Go implementation of git. I will have codex do this, and fix any errors in my implementation.
3. Make the implementation decentralized. Without worrying about the overall security of the system, just make it decentralized.
4. Test the decentralized implementation on Windows, MacOS, and Ubuntu to identify and minimize cross platform bugs.
5. Have all of the writes containing repository data go through the QRL, performing consensus checks as necessary.
6. Test that consensus works as expected, and that changes cannot be made to the QRL without consensus.
7. Analyze the security of the system. How secure is the system if we treat the QRL as a black box?
8. Tune security of the system and provide reasoning for chosen values.
9. Build the desktop application to interact with the new tool.
10. Test the desktop tool across platforms.

## Design

### The Git Aspect
I'm planning to keep the behavior of git as consistent as possible to the binary that I'm used to using, with the possible addition of a couple of features:

- Squashing commits that aren't the most recent. I want users to be able to squash commits that aren't the most recent commits, with one command. I may even add the ability to squash non-consecutive commits, but this will come with some overhead.
- I may modify the Amend flow. Often times, I forget some last minute detail and this requires me to amend the last commit, and recommit with the new changes. Given that every push is going to require consensus, I can have this flow simplified by adding the ability to just modify the last local commit, and push/forcepush with one command.

### The Decentralized Aspect
This is going to require a lot more thought, but I think it's best to start off simple.
The simple requirements are that the ledger must not need a single place to be stored, and instead, all relevant data must be stored on each user's local computer.
I do not believe that this will cause major changes to the UX, as every git user already has a local copy of some branches within the repository. One unfortunate issue is that instead of having just the user's working branches in local storage, it will have to be the entire ledger, unless I find some way to split it up (one ledger per branch? BlockTree instead of BlockChain?).

The general implementation will look something like:

- On pulls, check if the upstream has any updates. If there are updates, verify the ledger and pull the updates.
- On pushes, check for consensus first. If consensus is reached, push to upstream and mark the ledger as changed for all other users.
- For any other actions, the tool will behave like Git

This already presents some questions:

- What exactly is "upstream"? Since the ledger is decentralized, technically upstream is the local copy stored across all user's machines. The details on which data is stored where still needs some work
- How can the user check if the "upstream" has any updates? Does the user even need to check if the upstream has any updates, or can this be done automatically for all users when a user pushes (make the push a POST that goes out to every other user)? If we make pushing a POST, how do we resolve offline behavior? What if the user who makes the push is the only one online?
- After a user makes a push, and before consensus is reached, where does the data live? When consensus is reached, how is that information propagated to every user?
- How do we establish consensus? Thankfully I've already decided that this process needs to be purely democratic (no user should have any more control than any other user when proposing writes to the ledger or meta properties of the ledger). I think that this part can be relatively simple. Have a default value of unanimous agreement for any changes to be made. Allow the users to unanimously change the required consensus to a number between 0 and 1. Require that the ratio of people who must agree is greater than the configured value. This, by default, gives full power to a sole user and only read power to any user in a group who does not have consensus. It's important that users can only be included in the group explicitly, and that inclusions also require consensus.
- Can we make use of gossip to replicate the data effectively?

### The Security Aspect
My hope is that QRL will take me most of the way. I am planning to treat QRL as a perfect solution, as improving QRL is not within the scope of this project. However, my own code still needs to be 100% secure in order for this project to make any sense. Even though I am treating QRL as a perfect solution, there is still tuning to do to ensure that QRL does what it claims to do, without making messages super long. As part of this tuning, it's important to document clear reasoning as to why parameters were chosen, and what the security impact is.

### The GUI
I really like the general feel of GitHub Desktop, but there are times when it doesn't have features I need, and then I have to switch over to the command line. I think, since the UX isn't so different, I can base my GUI off of GitHub Desktop, but also add a little text input at the bottom to use the tool through CLI. This minimizes context switching.

## Current MVP Implementation

The CLI now includes a decentralized gossip layer on top of the Git-compatible commands.

### New Commands

- `vcs daemon --listen 127.0.0.1:8787 --interval 10s` starts an HTTP sync endpoint and periodic gossip loop
- `vcs peer add http://127.0.0.1:8788` adds a peer
- `vcs peer list` lists peers
- `vcs sync` runs immediate anti-entropy sync against configured peers
- `vcs push` now auto-generates a consensus proposal and auto-casts a local yes vote
- `vcs push --process-pending` attempts to apply certified pending pushes
- `vcs push --list-pending` shows pending/failed/completed push records
- `vcs op append --type git.commit --data '{"hash":"abc123"}'` appends a signed local operation
- `vcs op list --limit 20` lists known operations
- `vcs consensus propose --ref refs/heads/main --new <oid>` creates a proposal
- `vcs consensus vote --proposal <id> --yes` votes on a proposal
- `vcs consensus certify --proposal <id>` creates a certification op when quorum is met
- `vcs consensus status --proposal <id>` prints proposal vote/cert state
- `vcs consensus config --threshold 0.67 --members <id1,id2,...>` sets voting policy

### Storage

Decentralized metadata is stored in:

- OS-specific user config path for node identity (ed25519):
  - Linux: `$XDG_CONFIG_HOME/vcs/gossip/identities/<repo-hash>/identity.json` (or `~/.config/...`)
  - macOS: `~/Library/Application Support/vcs/gossip/identities/<repo-hash>/identity.json`
  - Windows: `%APPDATA%\\vcs\\gossip\\identities\\<repo-hash>\\identity.json`
  - Optional override base directory: `VCS_IDENTITY_DIR`
- `.vcs/gossip/peers.json` (peer list)
- `.vcs/gossip/ops.log` (signed operation log)
- `.vcs/gossip/pending_pushes.json` (local pending push queue)

### Gossip Behavior

- Each node keeps a summary of highest known sequence number per author.
- Sync exchanges summaries and only missing signed operations (anti-entropy).
- Offline peers catch up when they return online and run `daemon` or `sync`.
- `commit`, `push`, and `pull` now emit signed ops automatically (`git.commit`, `git.push`, `git.pull`).
- `push` is now proposal-first: actual `git push` runs only after the proposal is certified.
- `sync` also tries to execute any certified pending pushes.

### Consensus Layer (MVP)

- Proposal type: `consensus.proposal` with `{proposal_id, ref, old_oid, new_oid, epoch, expires_at}`
- Vote type: `consensus.vote` with `{proposal_id, epoch, decision}`
- Certification type: `consensus.cert` with quorum calculation snapshot
- Quorum rule: yes-vote ratio must be strictly greater than configured threshold
- Membership: explicit via config, or auto-discovered from known node IDs when not configured

## Rust GUI (MVP)

A desktop GUI is available in `gui/` (Rust + `eframe/egui`) and is aimed at operating the current CLI workflow.

### What It Shows

- Current repo branch, HEAD, and working tree status
- Node identity and configured peers
- Proposal list with quorum/certification/expiry state
- Pending push queue (pending/failed/completed)
- Operation feed from `.vcs/gossip/ops.log`

### Actions in GUI

- `sync`
- `push --process-pending`
- `push --list-pending`
- `consensus propose`
- `consensus vote`
- `consensus certify`
- `consensus status`

### Run

From repo root:

```bash
cargo run --manifest-path gui/Cargo.toml
```

### Documentation

- `GettingStarted.md`: short decentralized GUI onboarding for Git users
- `Manual.md`: full GUI reference and troubleshooting
- `benchmarking/benchmarking.md`: benchmark guide for measuring `vcs` wrapper overhead vs native `git`

If `cargo build --release` fails with a `rust-lld`/`.eh_frame` linker error, this repo already pins Rust linker flags in:

- `.cargo/config.toml`
- `gui/.cargo/config.toml`
