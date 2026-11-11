# Reconnect Mac event hints after a clean EOF

Written before implementation, following PR #166.
Review baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.

## Confirmed path

AppModel.startEvents returns after its async event stream finishes normally.
EventConnection explicitly represents a clean HTTP completion with no error;
a graceful server close can therefore stop live hints for the entire session.
The two-minute poll still converges data, but never restarts the hint task.
An independent refutation checked the transport, server close path, session
owners and the unused coordinator reconnect loop.

## Intended minimal fix

Treat clean EOF like a retryable interruption using the existing ten-second
backoff. Share the reconnect path without changing cancellation, session or
unauthorized exits. Do not reconnect in a busy loop, alter polling or add a
new public API solely for testing.

## Regression and validation

- Extend the existing local NWListener event-server test seam to return a
  valid empty text/event-stream response and count connections.
- Drive the public model sign-in path. Observe no second connection before
  the fix, then a reconnect after the existing delay. Confirm no immediate
  tight loop and preserve the error/401/cancellation tests.
- Run focused App tests and the full affected Mac suites/build. Serialize
  native builds with the Android work. No real server/account is used.
- Inspect current-head CI and completed review feedback before merging.
