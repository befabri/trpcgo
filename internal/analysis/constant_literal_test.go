package analysis

import (
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

func TestConstToTSLiteralFractionalFloat(t *testing.T) {
	for _, literal := range []string{"0.5", "1.25", "0.1", "-0.125", "1e-3", "1e20", "1e-300", "1.7976931348623157e308"} {
		t.Run(literal, func(t *testing.T) {
			value := constant.MakeFromLiteral(literal, token.FLOAT, 0)
			c := types.NewConst(token.NoPos, nil, "Rate", types.Typ[types.Float64], value)
			generated := constToTSLiteral(c)
			got, err := strconv.ParseFloat(generated, 64)
			if err != nil {
				t.Fatalf("generated %q from %s; want a numeric literal, not a rational expression: %v", generated, literal, err)
			}
			want, _ := strconv.ParseFloat(literal, 64)
			if got != want {
				t.Fatalf("generated literal represents %v, want %v", got, want)
			}
		})
	}
}

func TestConstToTSLiteralFloat32Rounding(t *testing.T) {
	for _, tc := range []struct{ literal, want string }{
		{"0.1", "0.1"},
		{"1.0000000596046447753906250000000001", "1.0000001"},
		{"-1.0000000596046447753906250000000001", "-1.0000001"},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			value := constant.MakeFromLiteral(tc.literal, token.FLOAT, 0)
			c := types.NewConst(token.NoPos, nil, "Rate", types.Typ[types.Float32], value)
			if got := constToTSLiteral(c); got != tc.want {
				t.Fatalf("literal=%q, want %q", got, tc.want)
			}
		})
	}
}
