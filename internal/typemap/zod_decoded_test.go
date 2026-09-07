package typemap

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func TestHoistZodRuntimeHelpers(t *testing.T) {
	t.Run("unused", func(t *testing.T) {
		source := "export const InputSchema = z.string();\n"
		declarations, body := HoistZodRuntimeHelpers(source)
		if declarations != "" || body != source {
			t.Fatalf("unused helper changed output: declarations=%q body=%q", declarations, body)
		}
	})
	t.Run("shared across kinds and occurrences", func(t *testing.T) {
		source := ZodQuotedFloatValue("left", "float64") + " === " + ZodQuotedFloatValue("right", "float32")
		declarations, body := HoistZodRuntimeHelpers(source)
		if count := strings.Count(declarations, goQuotedFloatDecoder); count != 1 {
			t.Fatalf("helper declaration count=%d; want 1", count)
		}
		if strings.Contains(body, goQuotedFloatDecoder) {
			t.Fatal("body retains an inline helper implementation")
		}
		if want := "$trpcgoQuotedFloat(left, 64) === $trpcgoQuotedFloat(right, 32)"; body != want {
			t.Fatalf("body=%q; want %q", body, want)
		}
		if again, unchanged := HoistZodRuntimeHelpers(body); again != "" || unchanged != body {
			t.Fatal("hoisting an already processed body changed it")
		}
	})
	t.Run("exact expression only", func(t *testing.T) {
		// A similar handwritten function must not be treated as our generated
		// helper. Hoisting has no heuristic matching or JavaScript rewriting.
		source := "(" + strings.Replace(goQuotedFloatDecoder, "return 0;", "return 1;", 1) + ")(input, 64)"
		declarations, body := HoistZodRuntimeHelpers(source)
		if declarations != "" || body != source {
			t.Fatal("a different function expression was replaced")
		}
	})
}

// Test the wire decoder against encoding/json, including exact float bits. A
// schema acceptance test alone cannot detect a one-ULP error away from a bound.
func TestQuotedFloatDecoderMatchesGo(t *testing.T) {
	inputs := []string{
		"", "null", "null\n", "NaN", "-NaN", "Inf", "+Inf", "-Inf", "-iNfiNitY", "-Inf\n",
		"0", "-0", "00", "+0", ".1", "-.1", "1.", "1e2", "1e+2", "1e-2", "1e", "1e+",
		"1_000", "1__000", "1_", "1_.0", "1._0", "1.0_1", "1e1_0", "1e_10", "1e+_10",
		"0x1p0", "-0X1P2", "0x_1p0", "0x.8p0", "0x_.8p0", "0x1.p0", "0x1_ffp+2", "0x1",
		"0x1p1_0", "0x1p_10", "0x1_p0", "0x1._1p0", "0x1.1_p0", "0x1.1p0\n",
		"1e999999999999999999999999", "-1e-999999999999999999999999", "0e999999999999999999999999",
		"0x1p999999999999999999999999", "-0x1p-999999999999999999999999", "0x0p999999999999999999999999",
		"1.000000059604644775390625", "1.0000000596046447753906250000000000001",
		"0x1.000001p0", "0x1.0000010000000000000001p0", "0x1.00000000000008p0", "0x1.0000000000000800001p0",
		"0x1p-1074", "0x1p-1075", "0x1.00000001p-1075", "0x1.fffffffffffff7ffp1023", "0x1.fffffffffffff8p1023",
		"0x1p-149", "0x1p-150", "0x1.000001p-150", "0x1.ffffffp127", "0x1.fffffeffffffp127",
	}
	rng := rand.New(rand.NewPCG(0x46504c4f4154, 0x474f5a4f44))
	for range 300 {
		value := math.Float64frombits(rng.Uint64())
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		inputs = append(inputs, strconv.FormatFloat(value, 'x', -1, 64), strconv.FormatFloat(value, 'g', -1, 64))
	}
	type vector struct {
		Input string `json:"input"`
		Bits  int    `json:"bits"`
		Valid bool   `json:"valid"`
		Value string `json:"value"`
	}
	var vectors []vector
	for _, input := range inputs {
		wire, err := json.Marshal(map[string]string{"value": input})
		if err != nil {
			t.Fatal(err)
		}
		for _, bits := range []int{32, 64} {
			var value float64
			var err error
			if bits == 32 {
				var target struct {
					Value float32 `json:"value,string"`
				}
				err = json.Unmarshal(wire, &target)
				value = float64(target.Value)
			} else {
				var target struct {
					Value float64 `json:"value,string"`
				}
				err = json.Unmarshal(wire, &target)
				value = target.Value
			}
			vectors = append(vectors, vector{input, bits, err == nil, fmt.Sprintf("%016x", math.Float64bits(value))})
		}
	}
	encoded, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	script := "const decode = " + goQuotedFloatDecoder + ";\nconst vectors = " + string(encoded) + `;
const bytes = new DataView(new ArrayBuffer(8));
const failures: unknown[] = [];
for (const test of vectors) {
  const value = decode(test.input, test.bits as 32 | 64);
  const valid = !Number.isNaN(value);
  bytes.setFloat64(0, value);
  const encoded = bytes.getBigUint64(0).toString(16).padStart(16, "0");
  if (valid !== test.valid || (valid && encoded !== test.value)) failures.push({ ...test, actualValid: valid, actualValue: encoded });
}
if (failures.length) throw new Error(JSON.stringify(failures));
`
	runTypeScript(t, script)
	t.Logf("verified %d syntax and exact-rounding vectors against encoding/json", len(vectors))
}
