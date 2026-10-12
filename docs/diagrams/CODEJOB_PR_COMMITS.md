# codejob — reviewing a PR commit by commit

Every diamond has an id. The traceability table in [`docs/PLAN.md`](../PLAN.md) §6 maps each
branch to its test. A branch without a test is a defect.

## 1. Building the commit report (`PRCommitReport`)

```mermaid
flowchart TD
    A[PRCommitReport PR URL, reviewed commit] --> V1{V1: gh pr view base and head branch fails?}
    V1 -- yes --> E1[error: could not read PR]
    V1 -- no --> F[git fetch origin base head]
    F --> V2{V2: fetch fails?}
    V2 -- yes --> E2[error: fetch failed]
    V2 -- no --> L[git log --reverse origin/base..origin/head]
    L --> V3{V3: log fails?}
    V3 -- yes --> E3[error: could not list commits]
    V3 -- no --> EACH[for each commit: numstat + deleted files]
    EACH --> V4{V4: show fails?}
    V4 -- yes --> E4[error: could not read commit]
    V4 -- no --> K{K1: subject has CodejobCommitPrefix?}
    K -- yes --> MACH[machine commit: listed apart, not counted]
    K -- no --> AG[agent commit: counted]
    AG --> N{N1: reviewed commit empty, or commit after it?}
    N -- yes --> NEW[marked NEW]
    N -- no --> OLD[not marked]
    AG --> D{D1: commit deletes files?}
    D -- yes --> WD[warning: deletes …]
    D -- no --> OKD[no warning]
    EACH --> NET[git diff --numstat origin/base...origin/head]
    NET --> R{R1: sum of agent additions > net additions?}
    R -- yes --> WR[warning: N lines added earlier were removed by later commits]
    R -- no --> OKR[no warning]
```

## 2. Where the report is printed and recorded

The review mark is the **last agent commit** (`REVIEWED_COMMIT`), never the branch head: codejob's
own commits (status transitions, this mark itself) move the head and must not count as news.

```mermaid
flowchart TD
    P1[codejob pull, PR just became ready] --> REP[print report]
    P2[codejob pull, plan in review: fast-forward] --> REP
    REP --> M1{M1: last agent commit != REVIEWED_COMMIT?}
    M1 -- yes --> SAVE[write REVIEWED_COMMIT; commit chore codejob, push<br>P1: in the same commit as the status transition]
    M1 -- no --> SKIP[nothing to record]

    C[codejob close] --> RC[PRCommitReport]
    RC --> C1{C1: REVIEWED_COMMIT empty?}
    C1 -- yes --> EC1[error: run codejob pull and review the commits first]
    C1 -- no --> C2{C2: last agent commit != REVIEWED_COMMIT?}
    C2 -- yes --> EC2[error: N new agent commits since your last review; run codejob pull]
    C2 -- no --> MERGE[merge and publish as today]
```
