package typemap

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
