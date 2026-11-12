# Send articles to Karakeep and Hister

In the web client, open **Settings → Article integrations**. Configure the
base URL of each service you want to use. The reader then offers **Send to
Karakeep** and **Send to Hister**. Sending is explicit, requires a connection,
and does not change the article's reading state.

## Karakeep

- Enter your Karakeep base URL, without `/api/v1`, and an API key created in
  Karakeep's settings. With scoped keys, grant `bookmarks:readwrite` and,
  for a default list, `lists:readwrite`.
- Optionally enter a **default list ID**. Copy the ID from the URL of a
  manual list in Karakeep. Leave it blank to save without assigning a list.
  Smart lists are not writable destinations.
- Earnest sends the original article URL. Karakeep retrieves the page itself.
  Existing bookmarks are reused. Adding a bookmark to a list is idempotent.
- If saving succeeds but list assignment fails, Earnest reports the partial
  failure. Correct the list ID and send again; it reuses the bookmark.

## Hister

- Enter your Hister base URL and, if authentication is enabled, its access
  token. Earnest uses a Bearer token and Hister's documented `Origin: hister://`
  header for non-browser API clients.
- Hister does not have folders. Set a **default label** instead, or leave it
  blank. Earnest submits it with the document.
- Earnest sends the article's original URL, title and stored plain text.
  This indexes the copy available to Earnest, not a newly fetched full page.
  Hister's URL indexing rules still apply. Its sensitive-content scanner
  checks HTML, not this plain-text payload. Send only articles you want
  stored there.

## Credentials and private networks

Tokens are encrypted on the Earnest server with the deployment secret. They
are never returned by the settings API or included in sync events. Leave a
saved token field blank to keep it. Changing a destination URL never carries
the old token to the new URL: enter it again if needed. Disabling an
integration clears its token. Keep `EARNEST_SECRET_KEY` with your backups.

Use HTTPS unless HTTP is appropriate for your trusted local or tailnet
connection. Requests originate on the Earnest server, not in your browser.
CORS configuration on the destination is not needed.

The existing outbound address checks also apply to these integrations. For
LAN, Docker-network or tailnet destinations, explicitly allow each exact
host and port in `EARNEST_OUTBOUND_ALLOW_PRIVATE`, for example:

```dotenv
EARNEST_OUTBOUND_ALLOW_PRIVATE=karakeep.example.ts.net:443,hister:8080
```

Restart Earnest after changing the allowlist. Use the destination's final
base URL: redirects are rejected rather than forwarding credentials or
mistaking a login page for a successful save. On a connection failure,
Earnest cannot guarantee whether the destination saved the article; check
there before retrying. No automatic retries send articles in the background.

## API

These endpoints require a human session or human device token. Agent tokens
cannot read or change integration settings or initiate sends. Session writes
require the usual CSRF header.

- `GET /v1/integrations` returns both configurations with `token_set`, never
  the tokens.
- `PUT /v1/integrations/{provider}` accepts `url`, optional `token`,
  `default_list_id` (Karakeep only), `default_label` (Hister only), and `op_id`.
  Omitted tokens are preserved only when the URL is unchanged. An explicit
  empty token clears it; an enabled Karakeep configuration requires a token.
- `POST /v1/entries/{id}/send/{provider}` accepts `op_id`. A successful repeat
  of the same operation returns its recorded result without sending again.

The supported providers are `karakeep` and `hister`. See `api/openapi.json`
for the complete schemas.

Protocol references:
[Karakeep OpenAPI](https://github.com/karakeep-app/karakeep/blob/main/packages/open-api/karakeep-openapi-spec.json)
and [Hister API definitions](https://github.com/asciimoo/hister/blob/master/server/api.go).
