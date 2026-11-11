# Recover the Android device sign-in form

Written before implementation, following PR #166.
Review baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.

## Confirmed path

LoginScreen stops polling after an expired/denied device code or a network/API
failure, but retains its authorization. That disables both the server field
and Start button indefinitely. There is no retry or cancel control. The normal
server expiry is ten minutes. An independent refutation confirmed the real
server error codes, the sole sign-in surface and the missing reset.

## Intended minimal fix

Clear the current authorization when polling ends with a terminal failure,
retaining the useful error message and entered server. A new click can start
a fresh authorization. Preserve pending and slow-down polling, and rethrow
CancellationException without resetting state or showing a false error.
Do not automatically replay unknown token issuance or change auth policy.

## Regression and validation

- Use the existing Compose/Robolectric scaffolding and an internal defaulted
  API factory seam, with the same production API by default. Add no dependency.
- Observe an expired/denied code leaving Start disabled before the behavior fix.
- Verify the failure returns to an enabled form and a second click starts a new
  code, while pending/slow-down stay active. Exercise cancellation where practical.
- Capture requests using an OkHttp interceptor, not real network/auth accounts.
- Review follow-ups: check that message-less transport errors still explain the
  reset and that one authorization reuses its polling client across pending and
  slow-down responses. Observe those checks fail before changing the paths.
  Keep client construction inside handled error behavior; do not broaden this
  into concurrent-start prevention, local expiry or automatic retry policy.
- Run the focused regression, then core/app unit tests, debug assembly and lint
  with JDK 21 and the installed Android SDK. Do not run another native build
  simultaneously. Merge only after current-head CI and actual review.
