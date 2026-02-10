# Benchmark Report: git vs vcs

- Timestamp (UTC): `2026-02-10T04:13:37Z`
- Host: `linux/amd64`
- git binary: `/usr/bin/git`
- vcs binary: `<repo>>/vcs`
- Iterations: `40` (warmup `8`)

| Scenario | git mean (ms) | vcs mean (ms) | Overhead (%) | git p95 (ms) | vcs p95 (ms) |
|---|---:|---:|---:|---:|---:|
| `status_short_clean` | 2.648 | 3.951 | 49.24 | 3.010 | 4.529 |
| `log_head_oneline` | 0.817 | 2.166 | 165.23 | 0.894 | 2.345 |
| `branch_list` | 0.747 | 2.108 | 182.43 | 0.796 | 2.237 |
| `checkout_branch` | 2.680 | 3.230 | 20.49 | 3.888 | 3.508 |
| `stage_single_file` | 2.552 | 3.628 | 42.17 | 2.920 | 4.578 |
| `unstage_single_file` | 2.543 | 3.922 | 54.20 | 2.920 | 4.515 |
| `commit_allow_empty` | 3.765 | 10.745 | 185.43 | 4.377 | 13.341 |

## Notes

- `vcs` is measured as the wrapper command path, not raw `git`.
- Separate repositories are used for `git` and `vcs` in each scenario to avoid cross-interference.
- `status`, `log`, `branch`, `checkout`, `stage`, `unstage`, and `commit --allow-empty` are covered.
