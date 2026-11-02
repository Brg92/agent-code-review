# S5: CI state on the head commit

Two pull requests in `acr-plugin-sandbox/sample-app`, both targeting `main`. Criteria come from the `.agent-code-review/` overlay on `main`.

## S5 A: failing, passing and running checks

| | |
| --- | --- |
| Branch | `ci/build-s5-lint-failure` |
| Pull request | `{{PR:sample-app:S5A}}` |

The change adds trailing whitespace in `app/src/main/java/com/example/sample/ui/HomeScreen.kt`. It also adds an `instrumented` job to `.github/workflows/checks.yml` that runs `sleep 2700`.

Expected check runs on the head commit, when recorded within 45 minutes of the push:

| Check | Status | Conclusion |
| --- | --- | --- |
| `lint` | completed | failure |
| `test` | completed | success |
| `instrumented` | in_progress | none |

Expected:
- The summary reports that CI is failing (`lint`) and still running (`instrumented`).
- The review still runs.
- No findings in the code. The whitespace is caught by CI, not by review.

## S5 B: no checks on the head

| | |
| --- | --- |
| Branch | `docs/readme-s5-no-checks` |
| Pull request | `{{PR:sample-app:S5B}}` |

The head commit message ends with `[skip ci]`, so the head has no check runs and the combined status is empty.

Expected:
- The summary says the head has no checks.
- This is reported apart from "checks pending", and is not treated as passing.
- The change only touches `README.md`, so there are no findings.
