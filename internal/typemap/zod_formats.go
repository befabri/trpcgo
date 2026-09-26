package typemap

import (
	"encoding/json"
	"net/url"
	"regexp/syntax"
	"strconv"
	"unicode"
)

// zodFormat is a string format rule. Its predicate follows
// go-playground/validator's grammar: a Zod format with the same name can
// enforce a different contract (UUID versions, Base64 padding, MAC encodings,
// or CIDR network alignment). A failure raises Zod's invalid_format issue, so
// Zod words the message for its own format names and applications can
// translate it.
type zodFormat struct {
	issue string // Issue format name; the validator tag where Zod has no name.
	// base formats select the field's schema. Every one rejects the empty
	// string, which makes a required rule on the same field redundant.
	base      bool
	predicate func(value string) string // JavaScript test of a Go-decoded string.
}

func zodRegexFormat(issue string, base bool, pattern string) zodFormat {
	return zodFormat{issue: issue, base: base, predicate: func(value string) string { return pattern + ".test(" + value + ")" }}
}

func zodFunctionFormat(issue, function string) zodFormat {
	return zodFormat{issue: issue, base: true, predicate: func(value string) string { return "(" + function + ")(" + value + ")" }}
}

// zodBlockFormat evaluates a function body once for the tested string.
func zodBlockFormat(issue, body string) zodFormat {
	return zodFormat{issue: issue, base: true, predicate: func(value string) string { return "((value: string): boolean => { " + body + " })(" + value + ")" }}
}

func zodUnicodeFormat(tag string) zodFormat {
	return zodFormat{issue: tag, base: true, predicate: func(value string) string {
		return "((value: string) => " + zodGoUnicodePredicate(tag, "value") + ")(" + value + ")"
	}}
}

var zodFormats = map[string]zodFormat{
	"alpha":            zodRegexFormat("alpha", false, `/^[a-zA-Z]+$(?![\s\S])/`),
	"alphanum":         zodRegexFormat("alphanum", false, `/^[a-zA-Z0-9]+$(?![\s\S])/`),
	"numeric":          zodRegexFormat("numeric", false, `/^[-+]?[0-9]+(?:\.[0-9]+)?$(?![\s\S])/`),
	"number":           zodRegexFormat("number", false, `/^[0-9]+$(?![\s\S])/`),
	"ascii":            zodRegexFormat("ascii", false, `/^[\x00-\x7F]*$(?![\s\S])/`),
	"printascii":       zodRegexFormat("printascii", false, `/^[\x20-\x7E]*$(?![\s\S])/`),
	"alphaunicode":     zodUnicodeFormat("alphaunicode"),
	"alphanumunicode":  zodUnicodeFormat("alphanumunicode"),
	"email":            zodFunctionFormat("email", goEmailValidator),
	"url":              zodFunctionFormat("url", goURLValidator),
	"uuid":             zodRegexFormat("uuid", true, `/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$(?![\s\S])/`),
	"e164":             zodRegexFormat("e164", true, `/^\+?[1-9][0-9]{7,14}$(?![\s\S])/`),
	"jwt":              zodRegexFormat("jwt", true, `/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*$(?![\s\S])/`),
	"base64":           zodRegexFormat("base64", true, `/^(?:[A-Za-z0-9+\/]{4})*(?:[A-Za-z0-9+\/]{2}==|[A-Za-z0-9+\/]{3}=|[A-Za-z0-9+\/]{4})$(?![\s\S])/`),
	"base64url":        zodRegexFormat("base64url", true, `/^(?:[A-Za-z0-9_-]{4})*(?:[A-Za-z0-9_-]{2}==|[A-Za-z0-9_-]{3}=|[A-Za-z0-9_-]{4})$(?![\s\S])/`),
	"base64rawurl":     zodRegexFormat("base64rawurl", true, `/^(?:[A-Za-z0-9_-]{4})*(?:[A-Za-z0-9_-]{2,4})$(?![\s\S])/`),
	"hostname":         zodRegexFormat("hostname", true, `/^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$(?![\s\S])/`),
	"hostname_rfc1123": zodRegexFormat("hostname_rfc1123", true, `/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$(?![\s\S])/`),
	"hexadecimal":      zodRegexFormat("hexadecimal", true, `/^(0[xX])?[0-9a-fA-F]+$(?![\s\S])/`),
	"ulid":             zodRegexFormat("ulid", true, `/^[A-HJKMNP-TV-Z0-9ſK]{26}$(?![\s\S])/i`),
	"mac":              zodRegexFormat("mac", true, `/^(?:(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}:){7}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}:){19}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){5}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){7}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){19}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{4}\.){2}[0-9a-fA-F]{4}|(?:[0-9a-fA-F]{4}\.){3}[0-9a-fA-F]{4}|(?:[0-9a-fA-F]{4}\.){9}[0-9a-fA-F]{4}|[0-9a-fA-F]{12}|[0-9a-fA-F]{16}|[0-9a-fA-F]{40})$(?![\s\S])/`),
	"ip":               zodBlockFormat("ip", `return (`+goIPBytes+`)(value) !== null;`),
	"ipv4":             zodBlockFormat("ipv4", `const ip = (`+goIPBytes+`)(value); return ip !== null && ip.v4;`),
	"ipv6":             zodBlockFormat("ipv6", `const ip = (`+goIPBytes+`)(value); return ip !== null && !ip.v4;`),
	"cidrv4":           zodBlockFormat("cidrv4", `const parts = value.split("/"); if (parts.length !== 2 || !/^[0-9]+$(?![\s\S])/.test(parts[1]!)) return false; const ip = (`+goIPBytes+`)(parts[0]!); if (!ip || !ip.v4) return false; let bits = Number(parts[1]); if (ip.bytes.length === 16) bits -= 96; if (bits < 0 || bits > 32) return false; const bytes = ip.bytes.slice(-4); return bytes.every((byte, index) => { const host = Math.max(0, Math.min(8, (index + 1) * 8 - bits)); return (byte & (2 ** host - 1)) === 0; });`),
	"cidrv6":           zodBlockFormat("cidrv6", `const parts = value.split("/"); if (parts.length !== 2 || !/^[0-9]+$(?![\s\S])/.test(parts[1]!)) return false; const ip = (`+goIPBytes+`)(parts[0]!); return ip !== null && !ip.v4 && Number(parts[1]) <= 128;`),
}

// zodFormatBase returns the schema a base format selects for a string field.
func zodFormatBase(tag string) string {
	format, ok := zodFormats[tag]
	if !ok || !format.base {
		return ""
	}
	return "z.string().check(" + zodFormatCheck(format).functional() + ")"
}

func zodFormatCheck(format zodFormat) zodCheck {
	return zodIssueCheck(format.predicate(ZodGoStringValue("value")), `{ code: "invalid_format", format: `+ZodStringLiteral(format.issue)+` }`)
}

// Pattern from go-playground/validator v10.30.4. It is compiled to a finite
// state machine so matching keeps Go regexp semantics and linear input cost.
const goEmailPattern = "^(?:(?:(?:(?:[a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+(?:\\.([a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+)*)|(?:(?:\\x22)(?:(?:(?:(?:\\x20|\\x09)*(?:\\x0d\\x0a))?(?:\\x20|\\x09)+)?(?:(?:[\\x01-\\x08\\x0b\\x0c\\x0e-\\x1f\\x7f]|\\x21|[\\x23-\\x5b]|[\\x5d-\\x7e]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[\\x01-\\x09\\x0b\\x0c\\x0d-\\x7f]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}]))))*(?:(?:(?:\\x20|\\x09)*(?:\\x0d\\x0a))?(\\x20|\\x09)+)?(?:\\x22))))@(?:(?:(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])(?:[a-zA-Z]|\\d|-|\\.|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.)+(?:(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])(?:[a-zA-Z]|\\d|-|\\.|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.?$"

var goEmailValidator = buildGoEmailValidator()

func buildGoEmailValidator() string {
	expression, err := syntax.Parse(goEmailPattern, syntax.Perl)
	if err != nil {
		panic(err)
	}
	program, err := syntax.Compile(expression.Simplify())
	if err != nil {
		panic(err)
	}
	rows := make([][]any, len(program.Inst))
	for i, instruction := range program.Inst {
		operation := 5
		runes := make([]rune, 0)
		switch instruction.Op {
		case syntax.InstRune, syntax.InstRune1:
			operation = 0
			runes = append(runes, instruction.Rune...)
			if len(runes) == 1 {
				runes = append(runes, runes[0])
			}
		case syntax.InstRuneAny:
			operation, runes = 0, []rune{0, 0x10ffff}
		case syntax.InstRuneAnyNotNL:
			operation, runes = 0, []rune{0, 9, 11, 0x10ffff}
		case syntax.InstAlt, syntax.InstAltMatch:
			operation = 1
		case syntax.InstCapture, syntax.InstNop:
			operation = 2
		case syntax.InstEmptyWidth:
			operation = 3
		case syntax.InstMatch:
			operation = 4
		}
		rows[i] = []any{operation, instruction.Out, instruction.Arg, runes}
	}
	encoded, _ := json.Marshal(rows)
	start, _ := json.Marshal(program.Start)
	return `(value: string): boolean => {
  const at = value.lastIndexOf("@");
  if (at <= 0) return false;
  const local = value.slice(0, at), domain = value.slice(at + 1);
  // The anchored validator pattern only admits bare addr-specs. Within that
  // language, these are net/mail's additional quoted-string/dot-atom checks.
  if (domain.startsWith(".") || domain.endsWith(".") || domain.includes("..")) return false;
  if (local.startsWith('"')) {
    if (!local.endsWith('"')) return false;
    let escaped = false, count = 0;
    for (const rune of local.slice(1, -1)) {
      const point = rune.codePointAt(0)!;
      if (!(point >= 33 && point <= 126 || point >= 128 || point === 32 || point === 9)) return false;
      if (escaped) { escaped = false; count++; }
      else if (rune === "\\") escaped = true;
      else if (rune === '"') return false;
      else count++;
    }
    if (escaped || count === 0) return false;
  }
  const machine: readonly (readonly [number, number, number, readonly number[]])[] = ` + string(encoded) + `;
  const symbols = Array.from(value);
  const add = (start: number, position: number, states: Set<number>): void => {
    const pending = [start];
    while (pending.length) {
      const index = pending.pop()!;
      if (states.has(index)) continue;
      states.add(index);
      const [operation, out, arg] = machine[index]!;
      if (operation === 1) pending.push(out, arg);
      else if (operation === 2) pending.push(out);
      else if (operation === 3 && (!(arg & 4) || position === 0) && (!(arg & 8) || position === symbols.length)) pending.push(out);
    }
  };
  let active = new Set<number>();
  add(` + string(start) + `, 0, active);
  for (let position = 0; position < symbols.length; position++) {
    const point = symbols[position]!.codePointAt(0)!;
    const next = new Set<number>();
    for (const index of active) {
      const [operation, out, , ranges] = machine[index]!;
      if (operation !== 0) continue;
      for (let i = 0; i < ranges.length; i += 2) {
        if (point >= ranges[i]! && point <= ranges[i + 1]!) { add(out, position + 1, next); break; }
      }
    }
    if (next.size === 0) return false;
    active = next;
  }
  return Array.from(active).some((index) => machine[index]![0] === 4);
 }`
}

// The validator's URL tag uses net/url.Parse, not the browser WHATWG URL
// parser. This implements its syntax checks and the tag's file/opaque rules;
// it deliberately performs no network or domain-existence checks. Where Go
// releases parse hosts differently, the generator's url.Parse decides.
var goURLValidator = `(value: string): boolean => {
  // validator lowercases before parsing. These are Go's only non-ASCII
  // runes that become ASCII; all other case changes preserve URL grammar.
  value = value.replace(/\u0130/g, "i").replace(/\u212a/g, "k");
  const hash = value.indexOf("#");
  const fragment = hash < 0 ? "" : value.slice(hash + 1);
  let text = hash < 0 ? value : value.slice(0, hash);
  const validEscapes = (input: string): boolean => !/%(?![0-9a-fA-F]{2})/.test(input);
  if (!validEscapes(fragment) || /[\x00-\x1f\x7f]/.test(text)) return false;
  const schemeMatch = /^([a-zA-Z][a-zA-Z0-9+.-]*):/.exec(text);
  if (!schemeMatch) return false;
  const scheme = schemeMatch[1]!.toLowerCase();
  text = text.slice(schemeMatch[0].length);
  const query = text.indexOf("?");
  if (query >= 0) text = text.slice(0, query);
  if (!text.startsWith("/")) return scheme !== "file" && (text.length > 0 || fragment.length > 0);
  const hostAllowed = (point: number): boolean => point >= 128 || point >= 65 && point <= 90 || point >= 97 && point <= 122 || point >= 48 && point <= 57 || "!$&'()*+,;=:[]<>\"-_.~".includes(String.fromCharCode(point));
  const unescapeHost = (input: string, zone = false): string | null => {
    let output = "";
    for (let i = 0; i < input.length; i++) {
      const point = input.charCodeAt(i);
      if (input[i] === "%") {
        const pair = input.slice(i + 1, i + 3);
        if (!/^[0-9a-fA-F]{2}$(?![\s\S])/.test(pair)) return null;
        const byte = parseInt(pair, 16);
        if (!zone && byte < 128 && byte !== 37) return null;
        if (zone && byte !== 37 && byte !== 32 && (byte >= 128 || !hostAllowed(byte))) return null;
        output += String.fromCharCode(byte); i += 2;
      } else {
        if (point < 128 && !hostAllowed(point)) return null;
        output += input[i];
      }
    }
    return output;
  };
  let host = "";
  if (text.startsWith("//")) {
    const slash = text.indexOf("/", 2);
    const authority = slash < 0 ? text.slice(2) : text.slice(2, slash);
    text = slash < 0 ? "" : text.slice(slash);
    const at = authority.lastIndexOf("@");
    host = authority.slice(at + 1);
    if (at >= 0) {
      const user = authority.slice(0, at);
      if (!/^[a-zA-Z0-9\-._:~!$&'()*+,;=%@]*$(?![\s\S])/.test(user) || !validEscapes(user)) return false;
    }
    const open = host.lastIndexOf("[");
    if (open > 0 && ` + strconv.FormatBool(goURLBracketLeads) + `) return false;
    if (open >= 0) {
      const close = host.lastIndexOf("]");
      if (close < open || !/^(?::[0-9]*)?$(?![\s\S])/.test(host.slice(close + 1))) return false;
      const inside = host.slice(open + 1, close), zone = inside.indexOf("%25");
      let address: string | null;
      if (zone < 0) address = unescapeHost(inside);
      else {
        const prefix = unescapeHost(inside.slice(0, zone)), suffix = unescapeHost(inside.slice(zone), true);
        address = prefix === null || suffix === null ? null : prefix + suffix;
      }
      if (address === null) return false;
      const percent = address.indexOf("%");
      if (percent >= 0 && percent === address.length - 1) return false;
      const ip = (` + goIPBytes + `)(percent < 0 ? address : address.slice(0, percent));
      if (!ip || ip.bytes.length !== 16) return false;
    } else {
      let colon = host.indexOf(":");
      if (colon >= 0) {
        if (` + goURLHostColons() + `) colon = host.lastIndexOf(":");
        if (!/^:[0-9]*$(?![\s\S])/.test(host.slice(colon))) return false;
      }
      if (unescapeHost(host) === null) return false;
    }
  }
  if (!validEscapes(text)) return false;
  const path = text.replace(/%([0-9a-fA-F]{2})/g, (_, pair: string) => String.fromCharCode(parseInt(pair, 16)));
  return scheme === "file" ? path.length > 0 && path !== "/" : host.length > 0 || fragment.length > 0;
}`

// goURLBracketLeads reports that url.Parse accepts a bracketed IP literal
// only at the start of the host. Earlier releases took the last bracket.
var goURLBracketLeads = !ParsesURL("http://a[::1]")

// goURLHostColons returns a TypeScript expression over scheme reporting
// whether url.Parse lets the host hold extra colons, with the port after the
// last. Go 1.26 allows it for PostgreSQL only, Go 1.27 for every scheme but
// HTTP, and GODEBUG urlstrictcolons=0 for HTTP too.
func goURLHostColons() string {
	http, postgres, other := ParsesURL("http://a:1:2"), ParsesURL("postgres://a:1:2"), ParsesURL("x://a:1:2")
	return `(scheme === "http" || scheme === "https" ? ` + strconv.FormatBool(http) + ` : scheme === "postgres" || scheme === "postgresql" ? ` + strconv.FormatBool(postgres) + ` : ` + strconv.FormatBool(other) + `)`
}

// ParsesURL reports whether net/url, which validator's url tag uses, parses
// value. Its host rules changed between Go releases.
func ParsesURL(value string) bool {
	_, err := url.Parse(value)
	return err == nil
}

// Parse net.ParseIP's address language directly. Browser/Zod URL or IP parsers
// have different acceptance rules, especially zones and mapped addresses.
const goIPBytes = `(value: string): { bytes: number[]; v4: boolean } | null => {
  const ipv4 = (input: string): number[] | null => {
    const parts = input.split(".");
    if (parts.length !== 4) return null;
    const bytes: number[] = [];
    for (const part of parts) {
      if (!/^(?:0|[1-9][0-9]{0,2})$(?![\s\S])/.test(part) || Number(part) > 255) return null;
      bytes.push(Number(part));
    }
    return bytes;
  };
  if (!value.includes(":")) {
    const bytes = ipv4(value);
    return bytes === null ? null : { bytes, v4: true };
  }
  if (value.includes("%")) return null;
  if (value.includes(".")) {
    const last = value.lastIndexOf(":"), tail = ipv4(value.slice(last + 1));
    if (!tail) return null;
    value = value.slice(0, last + 1) + ((tail[0]! << 8) | tail[1]!).toString(16) + ":" + ((tail[2]! << 8) | tail[3]!).toString(16);
  }
  const halves = value.split("::");
  if (halves.length > 2) return null;
  const left = halves[0] === "" ? [] : halves[0]!.split(":");
  const right = halves.length < 2 || halves[1] === "" ? [] : halves[1]!.split(":");
  const length = left.length + right.length;
  if (halves.length === 1 ? length !== 8 : length >= 8) return null;
  const words = [...left, ...Array<number>(halves.length === 2 ? 8 - length : 0).fill(0), ...right];
  const bytes: number[] = [];
  for (const word of words) {
    if (typeof word === "string" && !/^[0-9a-fA-F]{1,4}$(?![\s\S])/.test(word)) return null;
    const number = typeof word === "number" ? word : parseInt(word, 16);
    bytes.push(number >> 8, number & 255);
  }
  return { bytes, v4: bytes.slice(0, 10).every((part) => part === 0) && bytes[10] === 255 && bytes[11] === 255 };
}`

// ZodGoTimeParts parses the decoded RFC3339 language used by time.Time's JSON
// decoder. Seconds are exact within its four-digit year range; nanoseconds are
// kept separate so comparisons do not discard sub-millisecond precision.
func ZodGoTimeParts(value string) string {
	return "(" + goTimeParts + ")(" + value + ")"
}

const goTimeParts = `(value: string): [number, number] | null => {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$(?![\s\S])/.exec(value);
  if (!match) return null;
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]);
  const hour = Number(match[4]), minute = Number(match[5]), second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59) return null;
  const zoneHour = Number(match[10] ?? 0), zoneMinute = Number(match[11] ?? 0);
  // Go's RFC3339 fallback parser accepts offsets of exactly 24h or 60m.
  if (zoneHour > 24 || zoneMinute > 60) return null;
  const adjustedYear = year - (month <= 2 ? 1 : 0);
  const era = Math.floor(adjustedYear / 400), yearOfEra = adjustedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  const unixDay = era * 146097 + dayOfEra - 719468;
  const offset = (zoneHour * 60 + zoneMinute) * 60 * (match[9] === "-" ? -1 : 1);
  const seconds = unixDay * 86400 + hour * 3600 + minute * 60 + second - offset;
  const nanos = Number((match[7] ?? "").slice(0, 9).padEnd(9, "0"));
  return [seconds, nanos];
}`

// ZodGoTimeIsZero tests reflect.Value.IsZero on a decoded time.Time. Only the
// zero instant spelled with the Z designator leaves the Location nil; any
// explicit offset, including +00:00, records a Location and is not zero.
func ZodGoTimeIsZero(value string) string {
	return `((parts, text) => parts !== null && parts[0] === -62135596800 && parts[1] === 0 && text.endsWith("Z"))(` + ZodGoTimeParts(value) + ", String(" + value + "))"
}

// The tables come from the generator's Go toolchain, matching the backend's
// Unicode rules instead of inheriting the browser's independently updated ICU.
var (
	goUnicodeLetter       = goUnicodeMembership(unicode.L)
	goUnicodeNumber       = goUnicodeMembership(unicode.N)
	goUnicodeLowerChanges = goUnicodeCaseMembership(unicode.ToLower)
	goUnicodeUpperChanges = goUnicodeCaseMembership(unicode.ToUpper)
)

func goUnicodeMembership(table *unicode.RangeTable) string {
	ranges := make([][3]uint32, 0, len(table.R16)+len(table.R32))
	for _, r := range table.R16 {
		ranges = append(ranges, [3]uint32{uint32(r.Lo), uint32(r.Hi), uint32(r.Stride)})
	}
	for _, r := range table.R32 {
		ranges = append(ranges, [3]uint32{r.Lo, r.Hi, r.Stride})
	}
	return goUnicodeRangeMembership(ranges)
}

func goUnicodeCaseMembership(mapping func(rune) rune) string {
	var points []uint32
	for point := rune(0); point <= unicode.MaxRune; point++ {
		if mapping(point) != point {
			points = append(points, uint32(point))
		}
	}
	var ranges [][3]uint32
	for i := 0; i < len(points); {
		start, end, stride := points[i], points[i], uint32(1)
		if i+1 < len(points) {
			stride = points[i+1] - points[i]
			end = points[i+1]
			i += 2
			for i < len(points) && points[i]-end == stride {
				end = points[i]
				i++
			}
		} else {
			i++
		}
		ranges = append(ranges, [3]uint32{start, end, stride})
	}
	return goUnicodeRangeMembership(ranges)
}

func goUnicodeRangeMembership(ranges [][3]uint32) string {
	encoded, _ := json.Marshal(ranges)
	// Hoisting this IIFE constructs the table once, rather than on every rune.
	return `(() => {
  // Go Unicode ` + unicode.Version + `.
  const ranges: readonly (readonly [number, number, number])[] = ` + string(encoded) + `;
  return (point: number): boolean => {
    let low = 0, high = ranges.length;
    while (low < high) {
      const middle = (low + high) >>> 1, range = ranges[middle]!;
      if (point < range[0]) high = middle;
      else if (point > range[1]) low = middle + 1;
      else return (point - range[0]) % range[2] === 0;
    }
    return false;
  };
})()`
}

func zodGoUnicodePredicate(tag, value string) string {
	runeCheck := ""
	switch tag {
	case "alphaunicode":
		runeCheck = "(" + goUnicodeLetter + ")(point)"
	case "alphanumunicode":
		runeCheck = "(" + goUnicodeLetter + ")(point) || (" + goUnicodeNumber + ")(point)"
	case "lowercase":
		runeCheck = "!(" + goUnicodeLowerChanges + ")(point)"
	case "uppercase":
		runeCheck = "!(" + goUnicodeUpperChanges + ")(point)"
	}
	return value + `.length > 0 && Array.from(` + value + `).every((rune) => { const point = rune.codePointAt(0)!; return ` + runeCheck + `; })`
}
