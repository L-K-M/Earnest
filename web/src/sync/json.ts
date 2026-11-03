// Lossless JSON codec. Protocol integers are signed 64-bit values encoded as
// JSON numbers, so every integer literal decodes to bigint and every bigint
// encodes back as a bare number. Fractional and exponent literals decode to
// number (they are never protocol integers).

export type JsonObject = { [key: string]: JsonValue };
export type JsonValue = null | boolean | number | bigint | string | JsonValue[] | JsonObject;

export class JsonError extends Error {}

const isDigit = (c: string) => c >= "0" && c <= "9";

class Parser {
  private i = 0;
  constructor(private readonly text: string) {}

  parse(): JsonValue {
    this.space();
    const v = this.value();
    this.space();
    if (this.i !== this.text.length) throw new JsonError(`trailing data at ${this.i}`);
    return v;
  }

  private space() {
    while (this.i < this.text.length && " \t\n\r".includes(this.text[this.i])) this.i++;
  }

  private peek(): string {
    if (this.i >= this.text.length) throw new JsonError("unexpected end of input");
    return this.text[this.i];
  }

  private value(): JsonValue {
    const c = this.peek();
    switch (c) {
      case "{":
        return this.object();
      case "[":
        return this.array();
      case '"':
        return this.string();
      case "t":
        this.word("true");
        return true;
      case "f":
        this.word("false");
        return false;
      case "n":
        this.word("null");
        return null;
      default:
        return this.number();
    }
  }

  private word(w: string) {
    if (this.text.slice(this.i, this.i + w.length) !== w) throw new JsonError(`expected ${w} at ${this.i}`);
    this.i += w.length;
  }

  private object(): JsonObject {
    this.i++;
    const out: JsonObject = Object.create(null) as JsonObject;
    this.space();
    if (this.peek() === "}") {
      this.i++;
      return out;
    }
    for (;;) {
      this.space();
      const key = this.string();
      this.space();
      if (this.peek() !== ":") throw new JsonError(`expected : at ${this.i}`);
      this.i++;
      this.space();
      out[key] = this.value();
      this.space();
      const c = this.peek();
      if (c === "}") {
        this.i++;
        return out;
      }
      if (c !== ",") throw new JsonError(`expected , or } at ${this.i}`);
      this.i++;
    }
  }

  private array(): JsonValue[] {
    this.i++;
    const out: JsonValue[] = [];
    this.space();
    if (this.peek() === "]") {
      this.i++;
      return out;
    }
    for (;;) {
      this.space();
      out.push(this.value());
      this.space();
      const c = this.peek();
      if (c === "]") {
        this.i++;
        return out;
      }
      if (c !== ",") throw new JsonError(`expected , or ] at ${this.i}`);
      this.i++;
    }
  }

  private string(): string {
    if (this.peek() !== '"') throw new JsonError(`expected string at ${this.i}`);
    this.i++;
    let out = "";
    for (;;) {
      if (this.i >= this.text.length) throw new JsonError("unterminated string");
      const c = this.text[this.i];
      if (c === '"') {
        this.i++;
        return out;
      }
      if (c === "\\") {
        this.i++;
        const esc = this.peek();
        this.i++;
        switch (esc) {
          case '"':
          case "\\":
          case "/":
            out += esc;
            break;
          case "b":
            out += "\b";
            break;
          case "f":
            out += "\f";
            break;
          case "n":
            out += "\n";
            break;
          case "r":
            out += "\r";
            break;
          case "t":
            out += "\t";
            break;
          case "u": {
            const hex = this.text.slice(this.i, this.i + 4);
            if (hex.length !== 4 || !/^[0-9a-fA-F]+$/.test(hex)) throw new JsonError(`bad \\u at ${this.i}`);
            this.i += 4;
            out += String.fromCharCode(parseInt(hex, 16));
            break;
          }
          default:
            throw new JsonError(`bad escape at ${this.i}`);
        }
        continue;
      }
      if (c < " ") throw new JsonError(`control character in string at ${this.i}`);
      out += c;
      this.i++;
    }
  }

  private number(): JsonValue {
    const start = this.i;
    if (this.peek() === "-") this.i++;
    if (this.peek() === "0") {
      this.i++;
    } else if (isDigit(this.peek())) {
      while (this.i < this.text.length && isDigit(this.text[this.i])) this.i++;
    } else {
      throw new JsonError(`bad number at ${this.i}`);
    }
    let integer = true;
    if (this.i < this.text.length && this.text[this.i] === ".") {
      integer = false;
      this.i++;
      if (this.i >= this.text.length || !isDigit(this.text[this.i])) throw new JsonError(`bad fraction at ${this.i}`);
      while (this.i < this.text.length && isDigit(this.text[this.i])) this.i++;
    }
    if (this.i < this.text.length && (this.text[this.i] === "e" || this.text[this.i] === "E")) {
      integer = false;
      this.i++;
      if (this.i < this.text.length && (this.text[this.i] === "+" || this.text[this.i] === "-")) this.i++;
      if (this.i >= this.text.length || !isDigit(this.text[this.i])) throw new JsonError(`bad exponent at ${this.i}`);
      while (this.i < this.text.length && isDigit(this.text[this.i])) this.i++;
    }
    const literal = this.text.slice(start, this.i);
    if (integer) return BigInt(literal);
    const n = Number(literal);
    if (!Number.isFinite(n)) throw new JsonError(`number out of range at ${start}`);
    return n;
  }
}

export function parseJson(text: string): JsonValue {
  return new Parser(text).parse();
}

function encodeString(s: string): string {
  let out = '"';
  for (const c of s) {
    const code = c.codePointAt(0) ?? 0;
    switch (c) {
      case '"':
        out += '\\"';
        break;
      case "\\":
        out += "\\\\";
        break;
      case "\b":
        out += "\\b";
        break;
      case "\f":
        out += "\\f";
        break;
      case "\n":
        out += "\\n";
        break;
      case "\r":
        out += "\\r";
        break;
      case "\t":
        out += "\\t";
        break;
      default:
        if (code < 0x20) out += `\\u${code.toString(16).padStart(4, "0")}`;
        else out += c;
    }
  }
  return out + '"';
}

export function stringifyJson(v: JsonValue): string {
  if (v === null) return "null";
  switch (typeof v) {
    case "boolean":
      return v ? "true" : "false";
    case "bigint":
      return v.toString();
    case "number":
      if (!Number.isFinite(v)) throw new JsonError("cannot encode non-finite number");
      return String(v);
    case "string":
      return encodeString(v);
    case "object":
      if (Array.isArray(v)) return `[${v.map(stringifyJson).join(",")}]`;
      return `{${Object.keys(v)
        .filter((k) => v[k] !== undefined)
        .map((k) => `${encodeString(k)}:${stringifyJson(v[k])}`)
        .join(",")}}`;
    default:
      throw new JsonError(`cannot encode ${typeof v}`);
  }
}

// Typed accessors used by wire and fixture decoders. Every failure is a
// protocol-level invalid error at the call site.
export function isObj(v: JsonValue): v is JsonObject {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
export function asObj(v: JsonValue, what: string): JsonObject {
  if (!isObj(v)) throw new JsonError(`${what}: expected object`);
  return v;
}
export function asArray(v: JsonValue, what: string): JsonValue[] {
  if (!Array.isArray(v)) throw new JsonError(`${what}: expected array`);
  return v;
}
export function asString(v: JsonValue, what: string): string {
  if (typeof v !== "string") throw new JsonError(`${what}: expected string`);
  return v;
}
export function asBool(v: JsonValue, what: string): boolean {
  if (typeof v !== "boolean") throw new JsonError(`${what}: expected boolean`);
  return v;
}
export function asInt(v: JsonValue, what: string): bigint {
  if (typeof v !== "bigint") throw new JsonError(`${what}: expected integer`);
  return v;
}
export function intIn(v: JsonObject, key: string, what: string): bigint {
  const raw = v[key];
  if (raw === undefined) return 0n;
  return asInt(raw, `${what}.${key}`);
}
export function strIn(v: JsonObject, key: string, what: string): string {
  const raw = v[key];
  if (raw === undefined) return "";
  return asString(raw, `${what}.${key}`);
}
export function boolIn(v: JsonObject, key: string, what: string): boolean {
  const raw = v[key];
  if (raw === undefined) return false;
  return asBool(raw, `${what}.${key}`);
}
