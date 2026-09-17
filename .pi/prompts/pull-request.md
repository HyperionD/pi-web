---
description: "PR: create, CI, review, merge, cleanup"
argument-hint: "[base-branch]"
---
Create/update PR from current work → CI + review loop → merge → cleanup.

Base: `$1` or `main`.

Rules:
- Title/body ← actual diff + commits.
- Body sections: `Intent`, `Changes`.
- Use `gh`.
- No unverified success.
- Merge only after Actions pass + no blocking review findings.

Account:
- Start by recording the current `gh` user as the PR owner and merger.
- Use the current authenticated account for all review, PR, and merge actions.

Flow:
1. Inspect branch, diff, commits → title/body.
2. Uncommitted changes? verify → commit.
3. Push branch.
4. PR missing? create. PR exists? update title/body.
5. Loop until pass:
   1. Wait for GitHub Actions → inspect result.
      - Failed? logs → fix → verify → commit → push → loop start.
   2. Context-free subagent review.
      - Input only: PR URL, `gh pr diff`, review criteria, account rule above.
      - Must comment on PR: verdict + findings.
   3. Blocking findings? fix → verify → commit → push → loop start.
6. Passed? merge PR → delete local/remote work branches.
7. Final only: PR URL, merge commit, checks run.
