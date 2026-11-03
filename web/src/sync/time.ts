// RFC 3339 timestamps at millisecond resolution, formatted the way Go's
// time.Time marshals them: "2006-01-02T15:04:05Z" with a fractional part only
// when nonzero. Vector receipts are second-precision; the zero time is
// "0001-01-01T00:00:00Z", matching an unset Go timestamp.

export const ZERO_TIME = Date.parse("0001-01-01T00:00:00Z");

const RFC3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

export function parseTime(text: string): number {
  const m = RFC3339.exec(text);
  if (!m) throw new Error(`invalid RFC 3339 time: ${text}`);
  const frac = m[7] ? Number(`0${m[7]}`) : 0;
  const year = Number(m[1]);
  // Date.UTC maps years 0-99 onto 1900-1999; correct it for the zero time.
  const d = new Date(Date.UTC(year, Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]), Number(m[6])));
  if (year < 100) d.setUTCFullYear(year);
  const offset = m[8] === "Z" ? 0 : sign(m[8]) * (Number(m[8].slice(1, 3)) * 60 + Number(m[8].slice(4, 6))) * 60000;
  return Math.round(d.getTime() - offset + frac * 1000);
}

function sign(zone: string): number {
  return zone.startsWith("-") ? -1 : 1;
}

const pad = (n: number, w: number) => String(Math.trunc(Math.abs(n))).padStart(w, "0");

export function formatTime(ms: number): string {
  const d = new Date(ms);
  const base =
    `${pad(d.getUTCFullYear(), 4)}-${pad(d.getUTCMonth() + 1, 2)}-${pad(d.getUTCDate(), 2)}` +
    `T${pad(d.getUTCHours(), 2)}:${pad(d.getUTCMinutes(), 2)}:${pad(d.getUTCSeconds(), 2)}`;
  const frac = d.getUTCMilliseconds();
  return frac === 0 ? `${base}Z` : `${base}.${pad(frac, 3)}Z`;
}
