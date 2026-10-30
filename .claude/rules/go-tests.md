# Go Test Structure'

> **Current and planned.** This file describes tests as they will be once the project has grown (protobuf messages, an HTTP API, MySQL-backed services) as well as how they are written today. A rule marked **(planned)** does not apply yet: do not act on it, and do not import or invent the tools it names (`errchecks`, `protocmp`, `dbtest`). Today Thaler is a SQLite app with no protobuf and no HTTP API; its tests use in-memory or temporary SQLite databases.

Go tests use the Given-When-Then (GTW) structure; DB-Backed service tests run migrations against a real MySQL container via `dbtest` **(planned)**.

## Idiomatic Test Structure (GWT)

- Every test must be table-driven, always. Declare cases in a slice of structs and iterate with `t.run(tc.name, ...)`, even wiht a single case today.
- Don't write a bare sequence of actions in a `Test*`function that could take multiple inputs.
- Use the subtest-scoped `t.Context()`for all service/client operations under `//given` and `//when`- test-level context, setup isolation, automatic cancellation.
- Saving it locally isnot required; pass `t.Context()` directly where possible.
- Organize each subtest with `//given`, `//when`, `//then`.
- Run tests with `make test` (today at the repo root; from the service directory once there are services).

## Standalone, decoupled table-driven tests

- Don't test env vars in serivce handlers.
- **(planned: no `errchecks` package yet; today `wantErr` is the error text, compared with `cmp.Diff`)** `wantErr`assertions in table-driven tests should use only standard `errchecks.Check` validators (e.g. `errchecks.IsInvalidArgument(...)`), not complex custom closures inside the test case slice.
- Keep the `t.Run` loop body unbranched, minimal, and linear.

## Isolated Unit Tests and Assertion Hygiene

- Dedicated tests for standalone helpers. Date/time converters, timezone shifters, string parsers, math, state-transition preconditon evaluators, etc. get their own table-driven unit tests - don't rely on indirect coverage via a high level integration/service test.
- Strict, non-redundant comparions (**the `protocmp` example is planned**): never use ignore-field filters (`protocmp.IgnoreFields` or structural bypasses) to dodge asserting a newly-introducted or optional field. Populate and assert every schema field in fixtures/expectations.
- **(planned: MySQL)** Keep test times at whoel seconds (`.Truncate(time.Second)`) since MySQL `TIMESTAMP`discards fractional seconds, so fractional values never round-trip.
- Prefer Go's `new(expr)`for pointers in fixtures (e.g. `new(time.Date(...))`) instead of writing a `timePtr`-style helper.
-Deterministic fixed test times: use a static package-level `fixedTestTime`in fixtures/mock data/list-filter params. Never call `time.Now()` inside fixtures or assertions since it causes flaky, DST/timezone-dependent failures.

## Idempotency, Databse Count, Flat Cleanups

- Unconditinal flat cleanups: Register `t.Cleanup` DB-cleanup statemnets unconditionally inside `t.Run` - no `if got.GetRole() != nil`-style guards, which panic on nil when a case fails. Group clean up statements flatly.
- Assert count via `wantCount`: define `wantCount int`on the test case, query DB, and assert against `tc.wantCount` rather than hardcoding counts.
- Unconditional idempotency testing for idempotency operations (creation via unique keys/upserts, deletion), unconditionally re-run the operation a second time whenever `tc.wantErr == nil`; assert no error and an unchanged DB event.

## In this repo

- Name the test after the method: `Test_Method` (`Test_Prune`, `Test_ListBackups`). Several scenarios of one thing are fine as `Test_Method_scenario`. Even a test with one scenario today is a table with one case.
- Contexts come from `t.Context()`; there is no package-level `ctx` any more. Where a helper has no `t` yet, give it one.
- The fixed time of the rule above is `fixedNow` in the TUI tests; core tests set explicit times on what they create.
- Compare with `cmp.Diff(want, got)` (go-cmp, with `cmpopts` where needed), including for errors; failures read `-want +got`.
- **No `TestMain`, no package-wide init hooks.** The maintainer dislikes them. Use a per-test helper instead (`cheapScrypt(t)` lowers the scrypt cost for one test and restores it with `t.Cleanup`). Keep each test self-contained.
- Tests must not depend on the wall clock or on timing: the TUI model takes a clock (`fixedNow`), files get explicit modification times, and a name that carries a time or a collision suffix is normalised before comparing (see `replacedName` in the restore tests). A flaky test is a bug in the test.
