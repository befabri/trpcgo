package typemap

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
