package codegen

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestObjectUnknownFieldPolicyPreservesObjectAPI(t *testing.T) {
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("style%d/allow%v", style, allow), func(t *testing.T) {
				def := typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{
					{Name: "value", Type: "string", GoKind: "string"},
					{Name: "supplied", Type: "string", GoKind: "string", ZodOmit: true},
					{Name: "nested", Type: "{ value: string }", GoKind: "struct", Inline: &typemap.TypeDef{Fields: []typemap.Field{{Name: "value", Type: "string", GoKind: "string"}}}},
				}}
				var output bytes.Buffer
				emitter := zodSchemaEmitter{style: style, allowUnknownFields: allow}
				emitter.writeObject(newErrWriter(&output), def, nil)
				imported := "zod"
				if style == typemap.ZodMini {
					imported = "zod/mini"
				}
				program := fmt.Sprintf(`import * as z from %q;
%s
const valid = {value:'ok',nested:{value:'ok'}};
const supplied = InputSchema.parse({...valid,supplied:{by:'transport'}});
if (!supplied.supplied || !InputSchema.shape.supplied) throw new Error('known omitted field was discarded');
for (const input of [{...valid,extra:1},{...valid,nested:{value:'ok',extra:1}}]) {
 const result = z.safeParse(InputSchema,input);
 if(result.success !== Boolean(%t)) throw new Error('unknown field policy mismatch: '+JSON.stringify(input));
 if(result.success && JSON.stringify(result.data)!==JSON.stringify(input)) throw new Error('unknown fields were discarded');
}
if (!InputSchema.shape.value) throw new Error('object shape API lost');
`, imported, output.String(), allow)
				if style == typemap.ZodStandard {
					program += `if(!InputSchema.safeExtend({additional:z.string()}).safeParse({...valid,additional:'ok'}).success) throw new Error('safeExtend unavailable');`
				}
				runJSONHelperProgram(t, map[string]string{"main.ts": program})
			})
		}
	}
}

func TestPublicGoConstantsRemainOpen(t *testing.T) {
	for _, tc := range []struct{ kind, ts, literal string }{{"string", "string", `"known"`}, {"int8", "number", "1"}} {
		def := typemap.TypeDef{Name: "Value", Kind: typemap.TypeDefUnion, UnionMembers: []string{tc.literal}, Underlying: &typemap.Field{Type: tc.ts, GoKind: tc.kind}}
		var output bytes.Buffer
		writeTypeDef(newErrWriter(&output), def)
		open := tc.ts
		if open == "string" {
			open = "(string & {})"
		}
		expected := "export type Value = " + tc.literal + " | " + open + ";"
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("public type must admit unnamed Go values: %s", output.String())
		}
	}
}
