# Google Reader compatibility

Configure a Google Reader compatible account in NetNewsWire or Capy Reader.
Use `https://<your-host>/api/greader` as its server URL, your Earnest username
as the email or login, and your separate Earnest compatibility password as the
password. Use HTTPS and a host reachable by the client. The compatibility
password is distinct from your normal login password.

The adapter supports ClientLogin, token, user-info, tag/list, rename-tag,
disable-tag, subscription/list, subscription/edit, subscription/quickadd,
subscription/import (OPML), stream/items/ids, stream/items/contents, edit-tag,
mark-all-as-read and unread-count. Subscription and tag lists support ETags.
IDs are decimal in item lists and the Google Reader hexadecimal long form in
contents; both forms are accepted for writes and content requests.

Google Reader presents folders and entry labels in one namespace. Earnest keeps
these as separate resources: subscription edits operate on folders and edit-tag
operates on entry labels. A name can represent both. A label stream includes
entries in the named folder and entries carrying the named entry label. Renaming
or disabling a shared name affects both resources.

Adding read is a passive seen action. Removing read or adding kept-unread is a
deliberate unseen action and clears Done. Starred maps to kept; read means seen
or Done and never changes the shelf to Done. A passive compatibility read can
lose to a deliberate native unread. Google Reader sends no operation timestamp
or base revision, so writes use receipt time and a zero base; a delayed unread
or star action can therefore win over an earlier native action. Mark-all is a
passive seen range, bounded by the highest sequence visible at receipt when no
`ts` is supplied, or by the highest sequence first seen by the supplied time.
