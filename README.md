# Quantum Resistant Decentralized Version Control System

Disclaimer: Almost everything in this project was written or built by Codex. The only sections of this codebase that have been created by hand are the parts of this README above the Current MVP Implementation section, and everything in the `about` directory.

## Purpose
Version control systems are core development tools which allow users to easily switch between different versions of a working product. They allow for fast iteration and minimal conflict in collaborative environments.

Decentralization is sometimes desired due to its key benefits:

- No reliance on a central authority to store the data (very useful if you don't want your code coming into contact with the internet)
- Changes to the code or the way the version control is managed are democratic (very useful if the group you're working with doesn't want to elect an authorized maintainer)
- Malicious actors using the system (even falsely authenticated and authorized as a legitimate) have limited power due to enforcement of democratic consensus

However, decentralization can have significant drawbacks:

- You need a lot more resources to keep track of whats where and how to get it (fingerprints, computation of hashes, etc)
- There is an added complexity which should be avoided if there is no benefit. Simple is better
- The fact that consensus is required can present challenges when trying to develop at pace

Quantum resistance is a cool feature which I think should be standard for any software in which security is important. 
It helps towards preventing attackers from tampering with your version controlled product using quantum computing.
Currently, there is a lot of software that is considered vulnerable to attacks that incorporate quantum computing.
I want to make preventing these vulnerabilities a priority.

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
