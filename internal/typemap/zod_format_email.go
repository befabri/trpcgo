package typemap

import (
	"encoding/json"
	"regexp/syntax"
)

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
