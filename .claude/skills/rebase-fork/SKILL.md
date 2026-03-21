---
name: rebase-fork
description: Use when the user wants to update their fork to the latest upstream main, rebase all branches, fix merge conflicts, and verify features are preserved. Triggers on requests like "update from upstream", "rebase onto main", "sync with source repo", or "pull latest changes from upstream".
---

# Rebase Fork onto Latest Upstream

Rebase all fork branches onto the latest upstream main while preserving all fork-specific features. Handles branch dependency ordering, duplicate commit detection, conflict resolution, and post-rebase verification.

## Overview

This workflow updates a fork that maintains multiple feature branches on top of an upstream repository. The core challenge: the fork has unique commits (features, fixes, tests) that must survive the rebase, while duplicate fixes (things upstream now covers) must be dropped to avoid conflicts.

## Prerequisites

- Two remotes: `origin` (the fork) and `upstream` (the source repo)
- All branches must have a clean working tree before starting
- No proxy server instances should be running

## Workflow

1. Preflight checks
2. Fetch upstream & update main
3. Map branch hierarchy
4. Inventory commits (KEEP/DROP)
5. Rebase primary branch (dev)
6. Rebase dependent branches
7. Fix build & test failures
8. Audit features
9. Push all branches

## Phase 1: Preflight

1. **Verify remotes exist:**
   ```bash
   git remote -v
   ```
   Expect `origin` (fork) and `upstream` (source). If `upstream` is missing, add it.

2. **Ensure clean working tree:**
   ```bash
   git status --short
   ```
   Must be empty. Stash or commit any uncommitted work first.

3. **Stop any running server instances:**
   ```bash
   ./start.sh --stop 2>/dev/null || pkill -f cli-proxy-api 2>/dev/null
   ```

4. **Record the current state** of each branch (commit counts, tip SHAs) for rollback if needed:
   ```bash
   for branch in $(git branch --format='%(refname:short)'); do
     echo "$branch $(git rev-parse $branch)"
   done > /tmp/pre-rebase-state.txt
   ```

## Phase 2: Fetch Upstream and Update Main

1. **Fetch upstream:**
   ```bash
   git fetch upstream
   ```

2. **Check how far behind main is:**
   ```bash
   git rev-list --count main..upstream/main
   ```

3. **Fast-forward local main:**
   ```bash
   git checkout main
   git merge --ff-only upstream/main
   ```
   If this fails, local main has divergent commits — investigate before proceeding.

## Phase 3: Map Branch Hierarchy

Determine the dependency order of branches. Some branches are based on other fork branches, not directly on main.

1. **List all local fork branches:**
   ```bash
   branches=$(git branch --format='%(refname:short)' | grep -v '^main$')
   ```

2. **Find direct parent for each branch** (filter out transitive ancestors — only keep the closest ancestor):
   ```bash
   for branch in $branches; do
     direct_parent=""
     for other in main $branches; do
       if [ "$branch" != "$other" ]; then
         if git merge-base --is-ancestor $other $branch 2>/dev/null; then
           # Check if this is closer than current direct_parent
           if [ -z "$direct_parent" ] || git merge-base --is-ancestor $direct_parent $other 2>/dev/null; then
             direct_parent=$other
           fi
         fi
       fi
     done
     echo "$branch -> parent: $direct_parent"
   done
   ```

3. **Establish the rebase order** — from root to leaf. Branches whose direct parent is `main` (or the primary dev branch) go first. Then branches that depend on those, etc.

## Phase 4: Inventory Commits — KEEP vs DROP

For the primary fork branch, list all unique commits above main:

```bash
git log --oneline main..<primary-branch>
```

**Note:** This works correctly only if the branch was previously rebased onto main. If the branch has a disconnected history (different root commit), find the old main equivalent by matching commit messages:
```bash
git log --oneline <branch> --grep="<last known upstream commit message>"
```
Then use `<old-main-equivalent>..<branch>` to see only the fork's unique commits.

### KEEP/DROP Decision Criteria

For each unique commit, compare against upstream changes:

| Keep if... | Drop if... |
|---|---|
| Feature/file doesn't exist in upstream | Upstream fixed the same bug (even differently) |
| Infrastructure the fork relies on (scripts, tests) | Commit was a workaround for a bug upstream now handles |
| New provider, executor, or integration | Build fix or debug artifact from a previous rebase |
| Config additions unique to the fork | Dependency that conflicts with upstream's approach |

**How to check if upstream covers a commit:**
```bash
# Search upstream commits for related changes
git log --oneline main --grep="<relevant keyword>"
# Check if the file was modified by upstream
git diff <old-main>..<new-main> -- <file-path>
# Read the upstream's version of the file
git show main:<file-path> | head -50
```

**Use a subagent** (Agent tool with `subagent_type: general-purpose`) to compare each fork commit against upstream changes. The agent should read both the old fork code and the new upstream code to determine coverage.

## Phase 5: Rebase Primary Branch

Two strategies depending on how many commits to keep vs drop:

### Strategy A: Cherry-pick (when dropping many commits)

```bash
git checkout -b <branch>-rebased main
# Cherry-pick only KEEP commits, oldest first:
git cherry-pick <sha1>
git cherry-pick <sha2>
# ...
```

**IMPORTANT:** Before swapping branch names, verify the rebased branch builds and tests pass:
```bash
CGO_ENABLED=0 go build ./...
go test ./... -count=1 -timeout 5m
```

Only then swap names:
```bash
git branch -m <branch> <branch>-old
git branch -m <branch>-rebased <branch>
```

Keep the `-old` branch until ALL phases complete (including Phase 7 and Phase 8). Delete it only at the very end during Phase 9 cleanup.

### Strategy B: Rebase --onto (when keeping all or most commits)

```bash
git rebase --onto main <old-base-sha> <branch>
```

**Rollback if something goes wrong:** Use the saved state from Phase 1:
```bash
git reflog <branch>  # find pre-rebase SHA
git reset --hard <pre-rebase-sha>
```

### Resolving Merge Conflicts

When a conflict occurs during cherry-pick or rebase:

1. **Identify conflicts:** `grep -rn "<<<<<<" <file>`
2. **Resolution rules:**
   - **Code conflicts:** Keep upstream's version, manually add the fork's unique additions (new cases in switch statements, new functions, new imports)
   - **go.mod conflicts:** Keep upstream dependency versions, only add the fork's genuinely new dependencies
   - **go.sum conflicts:** Take HEAD version, run `go mod tidy` afterward
   - **Model definitions:** Upstream may have restructured (e.g., moved to static data files). Preserve upstream's structure and add fork's additions as new functions
3. **After resolving:** `git add <files> && git cherry-pick --continue`
4. **Check for accidentally staged artifacts:** compiled binaries, test data files, WIP commits — `git rm --cached` them

These same conflict resolution rules apply to dependent branches in Phase 6.

## Phase 6: Rebase Dependent Branches

For each dependent branch (in hierarchy order from Phase 3):

1. **Remove any worktrees first:**
   ```bash
   git worktree list
   git worktree remove --force <path>  # if exists for this branch
   ```

2. **Rebase onto updated parent:**
   ```bash
   git rebase <parent-branch> <dependent-branch>
   ```
   Git will automatically skip commits that already exist on the parent (cherry-pick duplicates). If conflicts occur, follow the same resolution rules from Phase 5.

3. **Run `go mod tidy`** on each branch after rebase if go.mod/go.sum changed. Commit the result.

4. **Verify build:** `CGO_ENABLED=0 go build ./...`

## Phase 7: Fix Build and Test Failures

1. **Build all branches:**
   ```bash
   CGO_ENABLED=0 go build ./...
   ```

2. **Fix interface mismatches.** Upstream may have changed interface signatures. For each custom executor/provider in the fork:
   ```bash
   # Find the interface definition
   grep -A20 "interface {" <interface-file>
   # Compare fork's implementation
   grep "func.*MethodName" <fork-executor>
   # Look at how upstream implements it
   grep -A20 "func.*MethodName" <upstream-executor>
   ```
   Update the fork's implementation to match the new signature.

3. **Run full test suite:**
   ```bash
   go test ./... -count=1 -timeout 5m
   ```

4. **For each failure, determine if it's pre-existing upstream or caused by rebase.** Use a separate worktree to test main without disturbing the working tree:
   ```bash
   git worktree add /tmp/test-main main
   cd /tmp/test-main && go test <failing-package> -run <TestName> -v
   cd - && git worktree remove /tmp/test-main
   ```

5. **Fix genuine regressions** (model name changes, skip condition updates, etc.) and commit fixes.

6. **Propagate test fixes** to all branches that need them (cherry-pick or rebase).

## Phase 8: Audit Features

After all branches are rebased, verify nothing was lost:

1. **List all unique commits per branch:**
   ```bash
   git log --oneline main..<branch>
   ```

2. **For each feature, verify the key files exist:**
   ```bash
   git show <branch>:<expected-file> > /dev/null 2>&1 && echo "EXISTS" || echo "MISSING"
   ```

3. **For each DROPPED commit, verify upstream covers it:**
   - Search for the equivalent functionality in upstream's code
   - Confirm the fix approach is adequate

4. **Check for gaps** — features from dropped commits that upstream does NOT cover. These must be re-applied.

5. **Check for WIP/debug commits** that should be squashed or cleaned up before pushing.

6. **Verify all branches are 0 behind their parent:**
   ```bash
   for branch in <feature-branches>; do
     behind=$(git log --oneline $branch..<parent> | wc -l | tr -d ' ')
     echo "$branch: $behind behind"
   done
   ```

## Phase 9: Push All Branches

```bash
# Dev branch (may need force after rebase)
git push --force-with-lease origin <primary-branch>

# Feature branches (need force — history was rewritten)
git push --force-with-lease origin <branch1> <branch2> <branch3>

# Main (regular push — fast-forwarded cleanly)
git push origin main
```

If `--force-with-lease` is rejected, someone else pushed to the remote branch. Fetch, verify their changes don't conflict, then retry:
```bash
git fetch origin
git log origin/<branch>..<branch>  # see what you have that they don't
git push --force-with-lease origin <branch>  # retry
```

**Verify all synced:**
```bash
for branch in <all-branches>; do
  local=$(git rev-parse $branch)
  remote=$(git rev-parse origin/$branch 2>/dev/null || echo "none")
  [ "$local" = "$remote" ] && echo "$branch: SYNCED" || echo "$branch: NEEDS PUSH"
done
```

**Clean up backup branches:**
```bash
git branch -D <branch>-old  # only after everything is verified
```

## Common Gotchas

| Issue | Solution |
|---|---|
| Worktree blocks checkout/rebase | `git worktree remove --force <path>` |
| `go.sum` conflict | Take HEAD, run `go mod tidy` |
| Interface signature changed upstream | Update fork executors to match new return types |
| Model names renamed upstream | Update test files and budget trackers |
| Accidentally staged binaries | `git rm --cached <file>` |
| Branch has disconnected history | Find old-main-equivalent commit by message matching |
| Pre-existing upstream test failure | Confirm by running on `main`, then exclude from pass/fail gate |
| Feature from dropped commit not in upstream | Re-apply the commit or rewrite the feature |
| `--force-with-lease` rejected | `git fetch origin`, verify, retry |

## Post-Rebase Checklist

- [ ] All branches build with `CGO_ENABLED=0 go build ./...`
- [ ] All branches are 0 behind their parent branch
- [ ] Full test suite passes (except known pre-existing upstream failures)
- [ ] Race detector passes on changed packages: `go test -race ./... -count=1`
- [ ] All fork features verified present (key files exist, functionality intact)
- [ ] All DROPPED commits verified covered by upstream
- [ ] All branches pushed and synced with remote
- [ ] No running server instances or stale worktrees left behind
- [ ] Backup branches (`-old`) cleaned up
