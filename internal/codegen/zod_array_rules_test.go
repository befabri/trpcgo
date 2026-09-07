package codegen

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestArrayOmissionPreservesWireChecksAndWidensLiterals(t *testing.T) {
	length := int64(2)
	field := typemap.Field{Type: "number[]", GoKind: "array", ArrayLen: &length, Element: &typemap.ElementType{Type: "number", GoKind: "int"}, Validate: []typemap.ValidateRule{{Tag: "omitempty"}}}
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		t.Run(fmt.Sprint(style), func(t *testing.T) {
			var out bytes.Buffer
			imported := "zod"
			if style == typemap.ZodMini {
				imported = "zod/mini"
			}
			fmt.Fprintf(&out, "import * as z from %q;\n", imported)
			writeZodArrayRuleHelpers(newErrWriter(&out), nil, nil)
			fmt.Fprintf(&out, "const schema = %s;\n", applyZodArrayRules("z.array(z.literal(1))", field, style, "z.array(z.int())"))
			out.WriteString(`const zero: number[] = schema.parse([0, 0]);
if(zero.length !== 2) throw new Error('zero output lost');
schema.parse([1,1]);
for(const value of [['0',0],[0.1,0],[2,0]]) {
 if(schema.safeParse(value).success) throw new Error('omission bypassed type or element validation');
}
`)
			runJSONHelperProgram(t, map[string]string{"main.ts": out.String()})
		})
	}
}

func TestArrayZeroValidationDiagnostics(t *testing.T) {
	type omitted struct {
		Hidden string `json:"hidden" zod_omit:"true"`
	}
	for _, tc := range []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeFor[[1]time.Time](), "location identity"},
		{reflect.TypeFor[[1]omitted](), "schema omission"},
		{reflect.TypeFor[[1]*time.Time](), ""},
		{reflect.TypeFor[[1]map[string]int](), ""},
	} {
		length := int64(1)
		f := typemap.Field{GoKind: "array", ArrayLen: &length, Equality: typemap.DescribeReflectEquality(tc.typ), Validate: []typemap.ValidateRule{{Tag: "required"}}}
		err := validateZodArrayRules(f)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v; want %q", tc.typ, err, tc.want)
		}
	}
}
