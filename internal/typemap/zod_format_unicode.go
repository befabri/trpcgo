package typemap

import (
	"encoding/json"
	"unicode"
)

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
