// Checks the schema generated from this package with the test configuration:
// each alias must shape validation, and unknown fields must be allowed.
import { InputSchema } from "./schemas";
import { minimum } from "./minimum";

const name = "x".repeat(minimum);
const valid = { start: 1, end: 2, child: { name }, values: ["a"], anonymous: { name } };
InputSchema.parse(valid);
InputSchema.parse({ ...valid, unknownField: true });
for (const invalid of [
  { ...valid, child: { name: name.slice(1) } },
  { ...valid, anonymous: { name: name.slice(1) } },
  { ...valid, end: 1 },
  { ...valid, values: [] },
  { ...valid, values: [""] },
]) {
  if (InputSchema.safeParse(invalid).success) throw new Error("configured rule not applied: " + JSON.stringify(invalid));
}
