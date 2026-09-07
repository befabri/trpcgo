package typemap

// The validator's URL tag uses net/url.Parse, not the browser WHATWG URL
// parser. This implements its syntax checks and the tag's file/opaque rules;
// it deliberately performs no network or domain-existence checks.
const goURLValidator = `(value: string): boolean => {
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
        if (scheme === "postgres" || scheme === "postgresql") colon = host.lastIndexOf(":");
        if (!/^:[0-9]*$(?![\s\S])/.test(host.slice(colon))) return false;
      }
      if (unescapeHost(host) === null) return false;
    }
  }
  if (!validEscapes(text)) return false;
  const path = text.replace(/%([0-9a-fA-F]{2})/g, (_, pair: string) => String.fromCharCode(parseInt(pair, 16)));
  return scheme === "file" ? path.length > 0 && path !== "/" : host.length > 0 || fragment.length > 0;
}`
