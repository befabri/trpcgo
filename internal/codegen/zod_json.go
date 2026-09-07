package codegen

// writeZodJSONHelpers preserves the information JSON.parse discards when
// multiple JSON property names decode to the same Go map key. Symbol metadata
// lets schemas emitted in separate modules consume the same parsed input.
func writeZodJSONHelpers(ew *errWriter) {
	ew.println(`const $goJSONMetadataKey = Symbol.for("trpcgo.go-json.entries.v1");

type $GoJSONEntries = readonly (readonly [string, unknown])[];

function $goJSONObject(entries: $GoJSONEntries): { [key: string]: unknown } {
  const object: { [key: string]: unknown } = {};
  const freeze = (pairs: $GoJSONEntries): $GoJSONEntries =>
    Object.freeze(pairs.map(([name, value]) => Object.freeze([name, value] as const)));
  for (const [name, value] of entries) {
    Object.defineProperty(object, name, { value, enumerable: true, writable: true, configurable: true });
  }
  Object.defineProperty(object, $goJSONMetadataKey, {
    value: Object.freeze({ entries: freeze(entries), snapshot: freeze(Object.entries(object)) }),
  });
  return object;
}

function $goJSONEntries(value: object): $GoJSONEntries | undefined {
  try {
    const metadata = Object.getOwnPropertyDescriptor(value, $goJSONMetadataKey)?.value;
    if (metadata === null || typeof metadata !== "object" || !Object.isFrozen(metadata)) return undefined;
    const entries = Object.getOwnPropertyDescriptor(metadata, "entries")?.value;
    const snapshot = Object.getOwnPropertyDescriptor(metadata, "snapshot")?.value;
    if (!Array.isArray(entries) || !Object.isFrozen(entries) || !Array.isArray(snapshot) || !Object.isFrozen(snapshot)) return undefined;
    const pair = (item: unknown): readonly [string, unknown] | undefined => {
      if (!Array.isArray(item) || item.length !== 2 || !Object.isFrozen(item)) return undefined;
      const key = Object.getOwnPropertyDescriptor(item, "0");
      const value = Object.getOwnPropertyDescriptor(item, "1");
      if (!key || !("value" in key) || typeof key.value !== "string" || !value || !("value" in value)) return undefined;
      return [key.value, value.value];
    };
    const latest = new Map<string, unknown>();
    for (const item of entries) {
      const entry = pair(item);
      if (!entry) return undefined;
      latest.set(entry[0], entry[1]);
    }
    const keys = Object.keys(value);
    if (snapshot.length !== keys.length || latest.size !== keys.length) return undefined;
    for (let i = 0; i < keys.length; i++) {
      const entry = pair(snapshot[i]);
      if (!entry || entry[0] !== keys[i] || !latest.has(entry[0]) || !Object.is(latest.get(entry[0]), entry[1])) return undefined;
      const current = Object.getOwnPropertyDescriptor(value, entry[0]);
      if (!current || !("value" in current) || !current.enumerable || !Object.is(current.value, entry[1])) return undefined;
    }
    return entries as $GoJSONEntries;
  } catch {
    // Ordinary objects may carry an unrelated symbol or hostile accessors.
    return undefined;
  }
}

/** Parse raw JSON while retaining ordered object entries for Go map decoding. */
export function parseGoJSON(raw: string): unknown {
  // Native parsing validates the complete grammar before the scanner reads it.
  JSON.parse(raw);
  let position = 0;
  const whitespace = () => {
    while (position < raw.length && raw.charCodeAt(position) <= 32) position++;
  };
  const string = (): string => {
    const start = position++;
    while (position < raw.length) {
      const character = raw[position++];
      if (character === "\\") position++;
      else if (character === '"') break;
    }
    return JSON.parse(raw.slice(start, position)) as string;
  };
  const read = (): unknown => {
    whitespace();
    const character = raw[position];
    if (character === '"') return string();
    if (character === "{") {
      position++;
      const entries: [string, unknown][] = [];
      whitespace();
      if (raw[position] !== "}") {
        while (true) {
          whitespace();
          const key = string();
          whitespace();
          position++; // colon
          const value = read();
          entries.push([key, value]);
          whitespace();
          if (raw[position++] === "}") break;
        }
      } else position++;
      return $goJSONObject(entries);
    }
    if (character === "[") {
      position++;
      const array: unknown[] = [];
      whitespace();
      if (raw[position] !== "]") {
        while (true) {
          array.push(read());
          whitespace();
          if (raw[position++] === "]") break;
        }
      } else position++;
      return array;
    }
    const start = position;
    while (position < raw.length && !/[\s,\]}]/.test(raw[position]!)) position++;
    return JSON.parse(raw.slice(start, position));
  };
  return read();
}
`)
}
