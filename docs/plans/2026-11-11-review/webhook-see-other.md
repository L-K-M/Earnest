# Webhook See Other outcome

Written before implementation, following the continuation plan in PR #166.
Baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.

## Candidate

`classifyWebhook` treats HTTP 303 as a definite refusal, so the durable job
is retried and repeated refusals disable its destination. A receiver can
legitimately process a POST before returning 303 with a Location for its
result. Disabling redirect following does not prove the POST was refused.
[RFC 9110 section 15.4.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.4.4)
explicitly describes this POST-result behavior. The documented webhook
contract retries only definite failures, never unknown outcomes.

## Verification before a fix

- Ask an independent reviewer to refute reachability and the contract claim,
  including the existing 302 classification test.
- Add a classification regression for 303.
- Add a real local receiver that consumes a POST, records an effect, and
  returns 303 with a Location. Exercise the outbound client and durable
  Postgres job, advance its clock beyond retry backoff, and verify it sends
  once, never follows the Location, and does not disable the webhook.
- Observe the regression fail before changing production code. A failing
  expectation without the contract evidence is not enough to accept a fix.

## Intended minimal change, if confirmed

Treat HTTP 303 as ambiguous, like a 408 or a lost response. Preserve success,
proven pre-send failures, existing other status behavior, signatures,
redirect prohibition, destination authorization and failure backoff. Do not
claim 303 proves successful delivery. Run webhook regressions, surrounding
service tests, vet and the full Postgres-backed Go race suite before merge.
