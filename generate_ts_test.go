package trpcgo_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/trpc"
)

type GenEmbeddedBase struct {
	Value string `json:"value"`
}

type GenShadowedInput struct {
	GenEmbeddedBase
	Value int `json:"value"`
}

type GenExtendedShadow struct {
	GenEmbeddedBase `tstype:",extends"`
	Value           int `json:"value"`
}

func TestGenerateTSGenericConcreteFields(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			r := trpcgo.NewRouter()
			// Registration order decides which instantiation is reflected first;
			// Total and Page must stay int either way.
			intPath, stringPath := "a.ints", "b.strings"
			if reverse {
				intPath, stringPath = "b.ints", "a.strings"
			}
			trpcgo.MustVoidQuery(r, intPath, func(_ context.Context) (GenPage[int], error) {
				return GenPage[int]{}, nil
			})
			trpcgo.MustVoidQuery(r, stringPath, func(_ context.Context) (GenPage[string], error) {
				return GenPage[string]{}, nil
			})
			trpcgo.MustVoidQuery(r, "pair.same", func(context.Context) (GenPair[int, int], error) { return GenPair[int, int]{}, nil })
			trpcgo.MustVoidQuery(r, "pair.different", func(context.Context) (GenPair[string, bool], error) { return GenPair[string, bool]{}, nil })
			trpcgo.MustVoidQuery(r, "nested", func(context.Context) (GenPage[GenPair[string, int]], error) {
				return GenPage[GenPair[string, int]]{}, nil
			})
			checkGeneratedRouterContract(t, r, fmt.Sprintf(`
import type { RouterOutputs } from './trpc';
declare const response: RouterOutputs['%s']['strings'];
const total: number = response.total;
const page: number = response.page;
const items: string[] = response.items;
declare const same: RouterOutputs['pair']['same'];
declare const different: RouterOutputs['pair']['different'];
declare const nested: RouterOutputs['nested'];
const first: number = same.first;
const second: number = same.second;
const text: string = different.first;
const flag: boolean = different.second;
const nestedFirst: string = nested.items[0].first;
const nestedSecond: number = nested.items[0].second;
const nestedTotal: number = nested.total;
void [total, page, items, first, second, text, flag, nestedFirst, nestedSecond, nestedTotal];
`, stringPath[:1]))
		})
	}
}

func TestGenerateTSShadowedEmbeddedField(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "shadow", func(_ context.Context) (GenShadowedInput, error) {
		return GenShadowedInput{}, nil
	})
	trpcgo.MustVoidQuery(r, "extended", func(context.Context) (GenExtendedShadow, error) { return GenExtendedShadow{}, nil })
	trpcgo.MustVoidQuery(r, "inline", func(context.Context) (struct {
		GenEmbeddedBase
		Value int `json:"value"`
	}, error) {
		return struct {
			GenEmbeddedBase
			Value int `json:"value"`
		}{}, nil
	})
	checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
declare const response: RouterOutputs['shadow'];
declare const extended: RouterOutputs['extended'];
declare const inline: RouterOutputs['inline'];
const value: number = response.value;
const extendedValue: number = extended.value;
const inlineValue: number = inline.value;
void [value, extendedValue, inlineValue];
`)
}

func TestOutputParserTransformTypegenDriftTS(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.MustVoidQuery(r, "user.get", func(_ context.Context) (DriftPrivateUser, error) {
		return DriftPrivateUser{ID: "u1", SecretToken: "top-secret"}, nil
	}, trpcgo.OutputParser(func(u DriftPrivateUser) (any, error) {
		return DriftPublicUser{ID: u.ID}, nil
	}))

	// Runtime behavior: parser strips secretToken.
	resp := mustGet(t, newTestServer(t, trpc.NewHandler(r, "/trpc")), "/trpc/user.get")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	result, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatalf("result shape = %T, want object", body["result"])
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data shape = %T, want object", result["data"])
	}
	if _, exists := data["secretToken"]; exists {
		t.Fatalf("runtime leak: secretToken should be stripped, got data=%v", data)
	}

	// Type contract expectation: codegen should follow the post-parser shape and
	// therefore must not expose stripped fields.
	ts := generateTS(t, r)
	if strings.Contains(ts, "secretToken: string") {
		t.Fatalf("generated TS leaked stripped field secretToken; expected output contract to match parser-transformed shape:\n%s", ts)
	}
}

func TestOutputParserTransformTypegenDriftSubscriptionTS(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.MustVoidSubscribe(r, "user.stream", func(_ context.Context) (<-chan DriftPrivateUser, error) {
		ch := make(chan DriftPrivateUser, 1)
		ch <- DriftPrivateUser{ID: "u1", SecretToken: "top-secret"}
		close(ch)
		return ch, nil
	}, trpcgo.OutputParser(func(u DriftPrivateUser) (any, error) {
		return DriftPublicUser{ID: u.ID}, nil
	}))

	// Runtime behavior: parser strips secretToken from emitted subscription items.
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/user.stream")
	if err != nil {
		t.Fatalf("subscription request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if strings.Contains(bodyStr, "secretToken") {
		t.Fatalf("runtime leak: secretToken should be stripped from SSE payload, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"id":"u1"`) {
		t.Fatalf("missing transformed SSE item in body:\n%s", bodyStr)
	}

	// Type contract expectation: codegen should follow parser output shape for
	// subscription items as well.
	ts := generateTS(t, r)
	if strings.Contains(ts, "secretToken: string") {
		t.Fatalf("generated TS leaked stripped field secretToken for subscription output; expected parser-transformed contract:\n%s", ts)
	}
}

func TestGenerateTSGenericInstantiation(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// Must NOT contain Go-style generic bracket syntax in type names.
	// (Array notation like "string[]" is fine — we check specifically for
	// the pattern TypeName[...pkg...] which is the Go reflect name.)
	if regexp.MustCompile(`GenPage\[`).MatchString(ts) {
		t.Error("output contains Go-style generic syntax 'GenPage[...]'; expected TypeScript '<>' syntax")
	}

	// Must NOT contain Go package paths anywhere in the output.
	if strings.Contains(ts, "github.com/") {
		t.Error("output contains Go package paths (github.com/...)")
	}
	if strings.Contains(ts, "trpcgo_test.") {
		t.Error("output contains test package path (trpcgo_test.)")
	}

	for _, leaf := range []string{"GenAlpha", "GenBeta"} {
		if !regexp.MustCompile(`(?s)export interface GenPage_[a-f0-9]+ \{[^}]*items: ` + leaf + `\[\]`).MatchString(ts) {
			t.Errorf("missing concrete page for %s:\n%s", leaf, ts)
		}
	}
}

func TestGenerateTSGenericInterfaceDeduplication(t *testing.T) {
	r := trpcgo.NewRouter()

	// Three different instantiations of GenPage.
	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})
	trpcgo.Query(r, "c.list", func(_ context.Context, _ struct{}) (GenPage[GenGamma], error) {
		return GenPage[GenGamma]{}, nil
	})
	trpcgo.Query(r, "d.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	count := countPattern(ts, `(?m)^export interface GenPage`)
	if count != 3 {
		t.Errorf("expected 3 concrete GenPage declarations, got %d", count)
	}

	// Each concrete type should still have its own interface.
	for _, name := range []string{"GenAlpha", "GenBeta", "GenGamma"} {
		if countPattern(ts, `(?m)^export interface `+name+`\b`) != 1 {
			t.Errorf("expected exactly 1 interface declaration for %s", name)
		}
	}
}

func TestGenerateTSMultiParamGeneric(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "getPair", func(_ context.Context, _ struct{}) (GenPair[GenAlpha, GenBeta], error) {
		return GenPair[GenAlpha, GenBeta]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	if !strings.Contains(ts, "first: GenAlpha") || !strings.Contains(ts, "second: GenBeta") {
		t.Errorf("missing concrete pair fields:\n%s", ts)
	}

	// Must NOT contain Go bracket syntax.
	if regexp.MustCompile(`GenPair\[`).MatchString(ts) {
		t.Error("output contains Go-style generic syntax 'GenPair[...]'")
	}

	if !regexp.MustCompile(`export interface GenPair_[a-f0-9]+ \{`).MatchString(ts) {
		t.Error("expected a specialized GenPair interface")
	}
}

func TestGenerateTSNoDuplicateInterfaceNames(t *testing.T) {
	r := trpcgo.NewRouter()

	// Register several procedures with different types.
	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})
	trpcgo.Query(r, "b.get", func(_ context.Context, _ GenBeta) (GenBeta, error) {
		return GenBeta{}, nil
	})

	ts := generateTS(t, r)

	// Extract all interface names.
	re := regexp.MustCompile(`(?m)^export interface (\w+)`)
	matches := re.FindAllStringSubmatch(ts, -1)

	seen := map[string]int{}
	for _, m := range matches {
		seen[m[1]]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("interface %q declared %d times (must be unique)", name, count)
		}
	}
}

func TestGenerateTSGenericFieldTypes(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// Extract the GenPage interface body.
	re := regexp.MustCompile(`(?s)export interface GenPage_[a-f0-9]+ \{(.+?)\}`)
	m := re.FindStringSubmatch(ts)
	if m == nil {
		t.Fatal("could not find generic GenPage interface in output")
	}

	body := m[1]

	expectedItems := "GenAlpha[]"
	if !strings.Contains(body, "items: "+expectedItems) {
		t.Errorf("expected 'items: %s' in GenPage body, got:\n%s", expectedItems, body)
	}

	// Non-generic fields should remain concrete.
	if !strings.Contains(body, "total: number") {
		t.Errorf("expected 'total: number' in GenPage body, got:\n%s", body)
	}
	if !strings.Contains(body, "page: number") {
		t.Errorf("expected 'page: number' in GenPage body, got:\n%s", body)
	}
}

func TestGenerateTSTypeEdgeCases(t *testing.T) {
	t.Run("json.RawMessage to unknown", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithRawJSON, error) {
			return WithRawJSON{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "data: unknown;") {
			t.Errorf("json.RawMessage should map to unknown:\n%s", ts)
		}
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("missing name field:\n%s", ts)
		}
	})

	t.Run("any field to unknown", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithAnyField, error) {
			return WithAnyField{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "payload: unknown;") {
			t.Errorf("any/interface{} should map to unknown:\n%s", ts)
		}
	})

	t.Run("fixed array to T[]", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithFixedArray, error) {
			return WithFixedArray{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "coords: number[];") {
			t.Errorf("[3]float64 should map to number[]:\n%s", ts)
		}
		if !strings.Contains(ts, "tags: string[];") {
			t.Errorf("[2]string should map to string[]:\n%s", ts)
		}
	})

	t.Run("map with int key", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithIntKeyMap, error) {
			return WithIntKeyMap{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<number, string>") {
			t.Errorf("map[int]string should map to Record<number, string>:\n%s", ts)
		}
	})

	t.Run("double pointer unwrapped and optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithDoublePtr, error) {
			return WithDoublePtr{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "value?: string;") {
			t.Errorf("**string should unwrap to optional string:\n%s", ts)
		}
	})

	t.Run("byte slice to string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithBytes, error) {
			return WithBytes{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "photo: string;") {
			t.Errorf("[]byte should map to string:\n%s", ts)
		}
		if strings.Contains(ts, "number[]") {
			t.Errorf("[]byte should NOT be number[]:\n%s", ts)
		}
	})

	t.Run("nested named struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithNested, error) {
			return WithNested{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "address: CgAddress;") {
			t.Errorf("nested struct should reference CgAddress:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface CgAddress {") {
			t.Errorf("CgAddress interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "street: string;") {
			t.Errorf("CgAddress should have street field:\n%s", ts)
		}
		if !strings.Contains(ts, "city: string;") {
			t.Errorf("CgAddress should have city field:\n%s", ts)
		}
	})

	t.Run("recursive struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (TreeNode, error) {
			return TreeNode{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface TreeNode {") {
			t.Errorf("TreeNode interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "children: TreeNode[];") {
			t.Errorf("recursive field should reference self:\n%s", ts)
		}
		if !strings.Contains(ts, "label: string;") {
			t.Errorf("label field should be present:\n%s", ts)
		}
		if countPattern(ts, `(?m)^export interface TreeNode`) != 1 {
			t.Errorf("TreeNode should appear exactly once:\n%s", ts)
		}
	})

	t.Run("tstype skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSSkip, error) {
			return WithTSSkip{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "visible: string;") {
			t.Errorf("visible field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "hidden") {
			t.Errorf("tstype:\"-\" field should be excluded:\n%s", ts)
		}
	})

	t.Run("tstype override", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSOverride, error) {
			return WithTSOverride{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "raw: Record<string, unknown>;") {
			t.Errorf("tstype override should replace type:\n%s", ts)
		}
	})

	t.Run("tstype readonly", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithReadonly, error) {
			return WithReadonly{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "readonly id: string;") {
			t.Errorf("readonly modifier should be present:\n%s", ts)
		}
		// name should NOT be readonly.
		if strings.Contains(ts, "readonly name") {
			t.Errorf("name should not be readonly:\n%s", ts)
		}
	})

	t.Run("tstype required overrides pointer optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithRequired, error) {
			return WithRequired{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "avatar: string;") {
			t.Errorf("required pointer should not be optional:\n%s", ts)
		}
		if strings.Contains(ts, "avatar?:") {
			t.Errorf("avatar should NOT be optional:\n%s", ts)
		}
		if !strings.Contains(ts, "bio?: string;") {
			t.Errorf("bio pointer should be optional:\n%s", ts)
		}
	})

	t.Run("json skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithJSONSkip, error) {
			return WithJSONSkip{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "public: string;") {
			t.Errorf("public field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "secret") || strings.Contains(ts, "Secret") {
			t.Errorf("json:\"-\" field should be excluded:\n%s", ts)
		}
	})

	t.Run("embedded pointer struct flattened", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithPtrEmbed, error) {
			return WithPtrEmbed{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface WithPtrEmbed {") {
			t.Errorf("WithPtrEmbed interface should be emitted:\n%s", ts)
		}
		// A nil embedded pointer omits all its promoted JSON fields.
		if !strings.Contains(ts, "id?: string;") {
			t.Errorf("embedded PtrBase.ID should be flattened and optional:\n%s", ts)
		}
		if !strings.Contains(ts, "createdAt?: string;") {
			t.Errorf("embedded PtrBase.CreatedAt should be flattened and optional:\n%s", ts)
		}
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("own Name field should be present:\n%s", ts)
		}
		// PtrBase should NOT appear as a separate interface.
		if strings.Contains(ts, "export interface PtrBase") {
			t.Errorf("embedded PtrBase should not be a separate interface:\n%s", ts)
		}
	})

	t.Run("unexported field excluded", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithUnexported, error) {
			return WithUnexported{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "public: string;") {
			t.Errorf("public field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "private") {
			t.Errorf("unexported field should be excluded:\n%s", ts)
		}
	})

	t.Run("all numeric types to number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (AllNumerics, error) {
			return AllNumerics{}, nil
		})
		ts := generateTS(t, r)
		for _, field := range []string{"i", "i8", "i16", "i32", "i64", "u", "u8", "u16", "u32", "u64", "f32", "f64"} {
			if !strings.Contains(ts, field+": number;") {
				t.Errorf("field %q should be number:\n%s", field, ts)
			}
		}
	})

	t.Run("bool types", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithBool, error) {
			return WithBool{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "active: boolean;") {
			t.Errorf("bool should map to boolean:\n%s", ts)
		}
		if !strings.Contains(ts, "deleted?: boolean;") {
			t.Errorf("*bool should map to optional boolean:\n%s", ts)
		}
	})

	t.Run("deeply nested structs", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (CgOuter, error) {
			return CgOuter{}, nil
		})
		ts := generateTS(t, r)
		for _, iface := range []string{"CgOuter", "CgMiddle", "CgInner"} {
			if !strings.Contains(ts, "export interface "+iface+" {") {
				t.Errorf("missing interface %s:\n%s", iface, ts)
			}
		}
		if !strings.Contains(ts, "middle: CgMiddle;") {
			t.Errorf("CgOuter should reference CgMiddle:\n%s", ts)
		}
		if !strings.Contains(ts, "inner: CgInner;") {
			t.Errorf("CgMiddle should reference CgInner:\n%s", ts)
		}
	})

	t.Run("struct reuse emits single interface", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "getAddr", func(_ context.Context, _ CgAddress) (CgAddress, error) {
			return CgAddress{}, nil
		})
		trpcgo.VoidQuery(r, "listAddr", func(_ context.Context) ([]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		count := countPattern(ts, `(?m)^export interface CgAddress\b`)
		if count != 1 {
			t.Errorf("CgAddress should be emitted exactly once, got %d:\n%s", count, ts)
		}
	})

	t.Run("string type", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "ping", func(_ context.Context) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string>") {
			t.Errorf("string output should produce $Query<void, string>:\n%s", ts)
		}
	})

	t.Run("slice of struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "list", func(_ context.Context) ([]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[]>") {
			t.Errorf("[]CgAddress should produce CgAddress[]:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface CgAddress {") {
			t.Errorf("CgAddress interface should be emitted:\n%s", ts)
		}
	})
}

func TestGenerateTSInlineStructs(t *testing.T) {
	t.Run("mixed field types", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name   string `json:"name"`
			Count  int    `json:"count"`
			Active bool   `json:"active"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		// Inline structs are emitted directly in the procedure type.
		if !strings.Contains(ts, "name: string") {
			t.Errorf("inline struct should have name field:\n%s", ts)
		}
		if !strings.Contains(ts, "count: number") {
			t.Errorf("inline struct should have count field:\n%s", ts)
		}
		if !strings.Contains(ts, "active: boolean") {
			t.Errorf("inline struct should have active field:\n%s", ts)
		}
	})

	t.Run("empty struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "noop", func(_ context.Context, _ struct{}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, never>") {
			t.Errorf("empty struct should map to Record<string, never>:\n%s", ts)
		}
	})

	t.Run("with pointer field optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name  string  `json:"name"`
			Label *string `json:"label"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "label?: string") {
			t.Errorf("pointer field in inline struct should be optional:\n%s", ts)
		}
	})

	t.Run("with json skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name   string `json:"name"`
			Secret string `json:"-"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "name: string") {
			t.Errorf("name field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "secret") || strings.Contains(ts, "Secret") {
			t.Errorf("json:\"-\" field should be excluded from inline struct:\n%s", ts)
		}
	})

	t.Run("with omitempty", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name string   `json:"name"`
			Tags []string `json:"tags,omitempty"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "tags?: string[]") {
			t.Errorf("omitempty field should be optional:\n%s", ts)
		}
	})
}

func TestGenerateTSAllProcTypes(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "q", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "vq", func(_ context.Context) (string, error) {
		return "", nil
	})
	trpcgo.Mutation(r, "m", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidMutation(r, "vm", func(_ context.Context) (string, error) {
		return "", nil
	})
	trpcgo.Subscribe(r, "s", func(_ context.Context, _ CgAddress) (<-chan CgAddress, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "vs", func(_ context.Context) (<-chan string, error) {
		return nil, nil
	})

	ts := generateTS(t, r)

	// All three helper types should be present.
	for _, helper := range []string{
		`type $Query<TInput, TOutput>`,
		`type $Mutation<TInput, TOutput>`,
		`type $Subscription<TInput, TOutput>`,
	} {
		if !strings.Contains(ts, helper) {
			t.Errorf("missing helper type %q:\n%s", helper, ts)
		}
	}

	// Check each procedure type.
	checks := []string{
		"q: $Query<CgAddress, CgAddress>",
		"vq: $Query<void, string>",
		"m: $Mutation<CgAddress, CgAddress>",
		"vm: $Mutation<void, string>",
		"s: $Subscription<CgAddress, CgAddress>",
		"vs: $Subscription<void, string>",
	}
	for _, c := range checks {
		if !strings.Contains(ts, c) {
			t.Errorf("missing procedure %q:\n%s", c, ts)
		}
	}
}

func TestGenerateTSSliceVariants(t *testing.T) {
	t.Run("[]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[]string should produce string[]:\n%s", ts)
		}
	})

	t.Run("[]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]int, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number[]>") {
			t.Errorf("[]int should produce number[]:\n%s", ts)
		}
	})

	t.Run("[]bool", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]bool, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, boolean[]>") {
			t.Errorf("[]bool should produce boolean[]:\n%s", ts)
		}
	})

	t.Run("[]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]CgAddress, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[]>") {
			t.Errorf("[]CgAddress should produce CgAddress[]:\n%s", ts)
		}
	})

	t.Run("nested [][]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([][]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[][]>") {
			t.Errorf("[][]string should produce string[][]:\n%s", ts)
		}
	})

	t.Run("[3]string fixed array", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([3]string, error) {
			return [3]string{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[3]string should produce string[]:\n%s", ts)
		}
	})

	t.Run("[]*string pointer elements", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]*string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[]*string should produce string[]:\n%s", ts)
		}
	})
}

func TestGenerateTSMapVariants(t *testing.T) {
	t.Run("map[string]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]int, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<string, number>>") {
			t.Errorf("map[string]int should produce Record<string, number>:\n%s", ts)
		}
	})

	t.Run("map[int]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[int]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<number, string>>") {
			t.Errorf("map[int]string should produce Record<number, string>:\n%s", ts)
		}
	})

	t.Run("map[string]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]CgAddress, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, CgAddress>") {
			t.Errorf("map[string]CgAddress should produce Record<string, CgAddress>:\n%s", ts)
		}
	})

	t.Run("map[string][]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string][]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, string[]>") {
			t.Errorf("map[string][]string should produce Record<string, string[]>:\n%s", ts)
		}
	})

	t.Run("map[string]any", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]any, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, unknown>") {
			t.Errorf("map[string]any should produce Record<string, unknown>:\n%s", ts)
		}
	})
}

func TestGenerateTSKitchenSink(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.Mutation(r, "tree.create", func(_ context.Context, _ WithNested) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidMutation(r, "bool.toggle", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.Subscribe(r, "bytes.stream", func(_ context.Context, _ WithIntKeyMap) (<-chan WithBytes, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "ro.events", func(_ context.Context) (<-chan WithReadonly, error) {
		return nil, nil
	})
	trpcgo.Query(r, "addr.list", func(_ context.Context, _ CgAddress) ([]CgAddress, error) {
		return nil, nil
	})
	trpcgo.Query(r, "raw.get", func(_ context.Context, _ WithRequired) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})
	trpcgo.VoidQuery(r, "any.get", func(_ context.Context) (WithAnyField, error) {
		return WithAnyField{}, nil
	})
	trpcgo.VoidQuery(r, "deep.get", func(_ context.Context) (CgOuter, error) {
		return CgOuter{}, nil
	})

	ts := generateTS(t, r)

	// No Go package paths should leak.
	if strings.Contains(ts, "github.com/") {
		t.Errorf("output contains Go package paths:\n%s", ts)
	}

	// All named interfaces should be emitted exactly once.
	expectedInterfaces := []string{
		"CgAddress", "AllNumerics", "WithNested", "TreeNode", "WithBool",
		"WithIntKeyMap", "WithBytes", "WithReadonly", "WithRequired",
		"WithTSOverride", "WithAnyField", "CgOuter", "CgMiddle", "CgInner",
	}
	for _, name := range expectedInterfaces {
		count := countPattern(ts, `(?m)^export interface `+name+`\b`)
		if count != 1 {
			t.Errorf("interface %q should appear exactly once, got %d", name, count)
		}
	}

	// All three procedure helper types.
	for _, h := range []string{"$Query", "$Mutation", "$Subscription"} {
		if !strings.Contains(ts, "type "+h+"<") {
			t.Errorf("missing helper type %s:\n%s", h, ts)
		}
	}

	// RouterInputs and RouterOutputs.
	if !strings.Contains(ts, "export type RouterInputs =") {
		t.Errorf("missing RouterInputs:\n%s", ts)
	}
	if !strings.Contains(ts, "export type RouterOutputs =") {
		t.Errorf("missing RouterOutputs:\n%s", ts)
	}

	// AppRouter structure.
	if !strings.Contains(ts, "export type AppRouter =") {
		t.Errorf("missing AppRouter:\n%s", ts)
	}
}

func TestGenerateTSNestedCollections(t *testing.T) {
	t.Run("nested map map[string]map[string]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (NestedMapConfig, error) {
			return NestedMapConfig{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "settings: Record<string, Record<string, number>>;") {
			t.Errorf("nested map should produce Record<string, Record<string, number>>:\n%s", ts)
		}
	})

	t.Run("nested slice [][]Struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (Keyboard, error) {
			return Keyboard{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "keys: KeyboardKey[][];") {
			t.Errorf("[][]KeyboardKey should produce KeyboardKey[][]:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface KeyboardKey {") {
			t.Errorf("KeyboardKey interface should be emitted:\n%s", ts)
		}
	})

	t.Run("map with struct values as field", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (APIConfig, error) {
			return APIConfig{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "endpoints: Record<string, Endpoint>;") {
			t.Errorf("map[string]Endpoint field should produce Record<string, Endpoint>:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface Endpoint {") {
			t.Errorf("Endpoint interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "url: string;") {
			t.Errorf("Endpoint should have url field:\n%s", ts)
		}
	})

	t.Run("slice of maps", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (BatchResult, error) {
			return BatchResult{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "results: Record<string, number>[];") {
			t.Errorf("[]map[string]int should produce Record<string, number>[]:\n%s", ts)
		}
	})

	t.Run("map of slices", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (GroupedTags, error) {
			return GroupedTags{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "groups: Record<string, string[]>;") {
			t.Errorf("map[string][]string should produce Record<string, string[]>:\n%s", ts)
		}
	})

	t.Run("deeply nested map[string]map[string][]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (DeeplyNestedMap, error) {
			return DeeplyNestedMap{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "data: Record<string, Record<string, number[]>>;") {
			t.Errorf("deeply nested map should produce Record<string, Record<string, number[]>>:\n%s", ts)
		}
	})

	t.Run("top-level nested slice [][]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([][]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[][]>") {
			t.Errorf("[][]CgAddress should produce CgAddress[][]:\n%s", ts)
		}
	})

	t.Run("top-level slice of maps", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]map[string]string, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<string, string>[]>") {
			t.Errorf("[]map[string]string should produce Record<string, string>[]:\n%s", ts)
		}
	})
}

func TestGenerateTSOmitzero(t *testing.T) {
	t.Run("omitzero makes field optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithOmitzero, error) {
			return WithOmitzero{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("name should be required:\n%s", ts)
		}
		if !strings.Contains(ts, "created_at?: string;") {
			t.Errorf("omitzero field should be optional:\n%s", ts)
		}
	})

	t.Run("all optional strategies", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (OptionalVariants, error) {
			return OptionalVariants{}, nil
		})
		ts := generateTS(t, r)

		// Required: no pointer, no omit*.
		if !strings.Contains(ts, "always: string;") {
			t.Errorf("always should be required:\n%s", ts)
		}
		// omitempty → optional.
		if !strings.Contains(ts, "omitE?: string;") {
			t.Errorf("omitempty should be optional:\n%s", ts)
		}
		// omitzero → optional.
		if !strings.Contains(ts, "omitZ?: string;") {
			t.Errorf("omitzero should be optional:\n%s", ts)
		}
		// pointer → optional.
		if !strings.Contains(ts, "ptr?: string;") {
			t.Errorf("pointer should be optional:\n%s", ts)
		}
		// pointer + omitempty → optional.
		if !strings.Contains(ts, "ptrOmitE?: string;") {
			t.Errorf("pointer+omitempty should be optional:\n%s", ts)
		}
		// pointer + tstype required → required.
		if !strings.Contains(ts, "forcedReq: string;") {
			t.Errorf("tstype required should override pointer optional:\n%s", ts)
		}
		if strings.Contains(ts, "forcedReq?:") {
			t.Errorf("forcedReq should NOT be optional:\n%s", ts)
		}
	})
}

func TestGenerateTSMultipleEmbedded(t *testing.T) {
	t.Run("multiple embedded structs flattened", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (FullEntity, error) {
			return FullEntity{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface FullEntity {") {
			t.Errorf("FullEntity interface should be emitted:\n%s", ts)
		}
		// Fields from Timestamps should be flattened.
		if !strings.Contains(ts, "createdAt: string;") {
			t.Errorf("Timestamps.CreatedAt should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "updatedAt: string;") {
			t.Errorf("Timestamps.UpdatedAt should be flattened:\n%s", ts)
		}
		// Fields from Metadata should be flattened.
		if !strings.Contains(ts, "version: number;") {
			t.Errorf("Metadata.Version should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "source: string;") {
			t.Errorf("Metadata.Source should be flattened:\n%s", ts)
		}
		// Own field.
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("own Name field should be present:\n%s", ts)
		}
		// Embedded types should NOT appear as separate interfaces.
		if strings.Contains(ts, "export interface Timestamps") {
			t.Errorf("Timestamps should not be a separate interface:\n%s", ts)
		}
		if strings.Contains(ts, "export interface Metadata") {
			t.Errorf("Metadata should not be a separate interface:\n%s", ts)
		}
	})
}

func TestGenerateTSFieldsWithoutJSONTag(t *testing.T) {
	t.Run("fields without json tag use Go name", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (NoJSONTags, error) {
			return NoJSONTags{}, nil
		})
		ts := generateTS(t, r)
		// Exported fields should use their Go name.
		if !strings.Contains(ts, "PublicName: string;") {
			t.Errorf("exported field without json tag should use Go name:\n%s", ts)
		}
		if !strings.Contains(ts, "PublicCount: number;") {
			t.Errorf("exported field without json tag should use Go name:\n%s", ts)
		}
		// Unexported field should be excluded.
		if strings.Contains(ts, "privateVal") || strings.Contains(ts, "private") {
			t.Errorf("unexported field should be excluded:\n%s", ts)
		}
	})
}

func TestGenerateTSMiscEdgeCases(t *testing.T) {
	t.Run("inline struct in map value", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]struct {
			Foo uint32 `json:"foo"`
		}, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, { foo: number }>") {
			t.Errorf("map[string]struct should produce Record<string, { foo: number }>:\n%s", ts)
		}
	})

	t.Run("inline struct in slice", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]struct {
			Name string `json:"name"`
		}, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "{ name: string }[]") {
			t.Errorf("[]struct should produce { name: string }[]:\n%s", ts)
		}
	})

	t.Run("rune returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (rune, error) { return 0, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number>") {
			t.Errorf("rune should produce number:\n%s", ts)
		}
	})

	t.Run("byte returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (byte, error) { return 0, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number>") {
			t.Errorf("byte should produce number:\n%s", ts)
		}
	})

	t.Run("json.Number returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithJSONNumber, error) {
			return WithJSONNumber{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "value: number;") {
			t.Errorf("json.Number should map to number (marshals as unquoted JSON number):\n%s", ts)
		}
	})
}

func TestGenerateTSExtends(t *testing.T) {
	t.Run("basic extends", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExUser, error) {
			return ExUser{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		// ExUser should extend ExBase.
		if !strings.Contains(ts, "export interface ExUser extends ExBase {") {
			t.Errorf("should produce extends clause:\n%s", ts)
		}
		// ExBase should be emitted as its own interface.
		if !strings.Contains(ts, "export interface ExBase {") {
			t.Errorf("ExBase should be emitted as separate interface:\n%s", ts)
		}
		if !strings.Contains(ts, "id: string;") {
			t.Errorf("ExBase should have id field:\n%s", ts)
		}
		// ExUser's own fields should be present but not ExBase's.
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("ExUser should have name field:\n%s", ts)
		}
	})

	t.Run("pointer extends wraps with Partial", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExWithPtrExtends, error) {
			return ExWithPtrExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "export interface ExWithPtrExtends extends Partial<ExBase> {") {
			t.Errorf("pointer extends should wrap with Partial:\n%s", ts)
		}
	})

	t.Run("pointer extends with required skips Partial", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExWithPtrReqExtends, error) {
			return ExWithPtrReqExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "export interface ExWithPtrReqExtends extends ExBase {") {
			t.Errorf("required pointer extends should not wrap with Partial:\n%s", ts)
		}
	})

	t.Run("multiple extends", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExMultiExtends, error) {
			return ExMultiExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "extends ExBase, ExAuditFields") {
			t.Errorf("should extend both bases:\n%s", ts)
		}
		// Own field should be present.
		if !strings.Contains(ts, "title: string;") {
			t.Errorf("own field should be present:\n%s", ts)
		}
		// Both base interfaces should be emitted.
		if !strings.Contains(ts, "export interface ExBase {") {
			t.Errorf("ExBase should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface ExAuditFields {") {
			t.Errorf("ExAuditFields should be emitted:\n%s", ts)
		}
	})
}

func TestGenerateTSTsDoc(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSDoc, error) {
		return WithTSDoc{}, nil
	})
	ts := generateTS(t, r)
	t.Log(ts)

	// Fields with ts_doc should have JSDoc comments.
	if !strings.Contains(ts, "/** The hostname to connect to */") {
		t.Errorf("host field should have JSDoc:\n%s", ts)
	}
	if !strings.Contains(ts, "/** Port number (1-65535) */") {
		t.Errorf("port field should have JSDoc:\n%s", ts)
	}
	// Field without ts_doc should have no JSDoc.
	if strings.Contains(ts, "/** name") {
		t.Errorf("name field should not have JSDoc:\n%s", ts)
	}
}

func TestGenerateTSTsc(t *testing.T) {
	tscPath := typeScriptCompiler(t)

	// Build a router exercising many type paths.
	r := trpcgo.NewRouter()
	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.Mutation(r, "tree.create", func(_ context.Context, _ WithNested) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidMutation(r, "bool.toggle", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.Subscribe(r, "bytes.stream", func(_ context.Context, _ WithIntKeyMap) (<-chan WithBytes, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "ro.events", func(_ context.Context) (<-chan WithReadonly, error) {
		return nil, nil
	})
	trpcgo.VoidQuery(r, "raw.get", func(_ context.Context) (WithRawJSON, error) {
		return WithRawJSON{}, nil
	})
	trpcgo.VoidQuery(r, "any.get", func(_ context.Context) (WithAnyField, error) {
		return WithAnyField{}, nil
	})
	trpcgo.VoidQuery(r, "deep.get", func(_ context.Context) (CgOuter, error) {
		return CgOuter{}, nil
	})
	trpcgo.Query(r, "skip.get", func(_ context.Context, _ WithRequired) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})

	ts := generateTS(t, r)

	// Write generated TS and tsconfig to temp dir.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNodeModules(t, dir)
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s", string(output))
	}
}

func TestGenerateEnumsTsc(t *testing.T) {
	tscPath := typeScriptCompiler(t)

	// Covers awkward enum values that still need to compile as TypeScript.
	defs := []typemap.TypeDef{
		{Name: "Role", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"viewer"`, `"admin"`}},
		{Name: "Word", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"default"`, `"in"`, `"class"`}},
		{Name: "Weird", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"3d"`, `"a.b"`, `"a-b"`, `""`, `"__proto__"`}},
		{Name: "Escaped", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"a\"b"`, `"c\\d"`, `"e\tf"`}},
		{Name: "Dup", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"x"`, `"x"`, `"y"`}},
		{Name: "Priority", Kind: typemap.TypeDefUnion, UnionMembers: []string{"1", "2"}},
	}

	var buf bytes.Buffer
	if err := codegen.WriteEnums(&buf, defs); err != nil {
		t.Fatal(err)
	}
	enumsTS := buf.String()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "enums.ts"), []byte(enumsTS), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNodeModules(t, dir)
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["enums.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s\n\nGenerated enums.ts:\n%s", string(output), enumsTS)
	}

	if strings.Contains(enumsTS, "PriorityEnum") {
		t.Errorf("numeric union should be skipped:\n%s", enumsTS)
	}
	if n := strings.Count(enumsTS, `x: "x"`); n != 1 {
		t.Errorf("duplicate value should dedup to one key, found %d:\n%s", n, enumsTS)
	}
}

func TestGenerateTSTscExtended(t *testing.T) {
	// Extended tsc validation covering all new patterns from typescriptify/tygo.
	tscPath := typeScriptCompiler(t)

	r := trpcgo.NewRouter()

	// Nested collections.
	trpcgo.VoidQuery(r, "nested.mapmap", func(_ context.Context) (NestedMapConfig, error) {
		return NestedMapConfig{}, nil
	})
	trpcgo.VoidQuery(r, "nested.keyboard", func(_ context.Context) (Keyboard, error) {
		return Keyboard{}, nil
	})
	trpcgo.VoidQuery(r, "nested.apiconfig", func(_ context.Context) (APIConfig, error) {
		return APIConfig{}, nil
	})
	trpcgo.VoidQuery(r, "nested.batch", func(_ context.Context) (BatchResult, error) {
		return BatchResult{}, nil
	})
	trpcgo.VoidQuery(r, "nested.grouped", func(_ context.Context) (GroupedTags, error) {
		return GroupedTags{}, nil
	})
	trpcgo.VoidQuery(r, "nested.deep", func(_ context.Context) (DeeplyNestedMap, error) {
		return DeeplyNestedMap{}, nil
	})

	// Optional strategies.
	trpcgo.VoidQuery(r, "opt.omitzero", func(_ context.Context) (WithOmitzero, error) {
		return WithOmitzero{}, nil
	})
	trpcgo.VoidQuery(r, "opt.variants", func(_ context.Context) (OptionalVariants, error) {
		return OptionalVariants{}, nil
	})

	// Multiple embedded.
	trpcgo.VoidQuery(r, "entity.full", func(_ context.Context) (FullEntity, error) {
		return FullEntity{}, nil
	})

	// Fields without JSON tags.
	trpcgo.VoidQuery(r, "raw.nojson", func(_ context.Context) (NoJSONTags, error) {
		return NoJSONTags{}, nil
	})

	// Recursive.
	trpcgo.VoidQuery(r, "tree.get", func(_ context.Context) (TreeNode, error) {
		return TreeNode{}, nil
	})

	// Top-level complex types.
	trpcgo.VoidQuery(r, "complex.sliceslice", func(_ context.Context) ([][]CgAddress, error) {
		return nil, nil
	})
	trpcgo.VoidQuery(r, "complex.slicemap", func(_ context.Context) ([]map[string]string, error) {
		return nil, nil
	})

	// Tags: readonly, required, override, skip.
	trpcgo.VoidQuery(r, "tags.readonly", func(_ context.Context) (WithReadonly, error) {
		return WithReadonly{}, nil
	})
	trpcgo.VoidQuery(r, "tags.required", func(_ context.Context) (WithRequired, error) {
		return WithRequired{}, nil
	})
	trpcgo.VoidQuery(r, "tags.override", func(_ context.Context) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})
	trpcgo.VoidQuery(r, "tags.skip", func(_ context.Context) (WithTSSkip, error) {
		return WithTSSkip{}, nil
	})

	ts := generateTS(t, r)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNodeModules(t, dir)
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s\n\nGenerated TypeScript:\n%s", string(output), ts)
	}
}

func TestGenerateTSNodeExecution(t *testing.T) {
	tscPath := typeScriptCompiler(t)

	r := trpcgo.NewRouter()
	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.VoidQuery(r, "tree.get", func(_ context.Context) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidQuery(r, "bool.get", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.VoidQuery(r, "ext.get", func(_ context.Context) (ExUser, error) {
		return ExUser{}, nil
	})
	trpcgo.VoidQuery(r, "doc.get", func(_ context.Context) (WithTSDoc, error) {
		return WithTSDoc{}, nil
	})

	ts := generateTS(t, r)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNodeModules(t, dir)

	// Write a structural check file that validates types via satisfies.
	check := `import type { CgAddress, AllNumerics, TreeNode, WithBool, ExUser, ExBase, WithTSDoc } from "./trpc.js";

// Structural validation — these fail at compile time if types are wrong.
const addr = { street: "Main St", city: "NYC" } satisfies CgAddress;
const nums = {
  i: 1, i8: 2, i16: 3, i32: 4, i64: 5,
  u: 6, u8: 7, u16: 8, u32: 9, u64: 10,
  f32: 1.1, f64: 2.2,
} satisfies AllNumerics;
const tree: TreeNode = { label: "root", children: [{ label: "child", children: [] }] };
const bools = { active: true } satisfies WithBool;
const base = { id: "1" } satisfies ExBase;
const user = { id: "1", name: "Alice" } satisfies ExUser;
const doc = { host: "localhost", port: 8080, name: "test" } satisfies WithTSDoc;

// Suppress unused variable warnings.
void addr; void nums; void tree; void bools; void base; void user; void doc;
`
	if err := os.WriteFile(filepath.Join(dir, "check.ts"), []byte(check), 0o644); err != nil {
		t.Fatal(err)
	}

	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts", "check.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc structural check failed:\n%s\n\nGenerated TypeScript:\n%s", string(output), ts)
	}
}

func TestGenerateTS(t *testing.T) {
	type Base struct {
		ID        string     `json:"id"`
		CreatedAt time.Time  `json:"createdAt"`
		DeletedAt *time.Time `json:"deletedAt,omitempty"`
	}
	type UserInput struct {
		Name  string   `json:"name"`
		Email string   `json:"email"`
		Tags  []string `json:"tags,omitempty"`
	}
	type UserOutput struct {
		Base
		Name  string            `json:"name"`
		Email string            `json:"email"`
		Meta  map[string]string `json:"meta,omitempty"`
	}

	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.getById", func(ctx context.Context, input struct {
		ID string `json:"id"`
	}) (UserOutput, error) {
		return UserOutput{}, nil
	})
	trpcgo.VoidQuery(router, "user.list", func(ctx context.Context) ([]UserOutput, error) {
		return nil, nil
	})
	trpcgo.Mutation(router, "user.create", func(ctx context.Context, input UserInput) (UserOutput, error) {
		return UserOutput{}, nil
	})
	trpcgo.VoidSubscribe(router, "user.events", func(ctx context.Context) (<-chan UserOutput, error) {
		return nil, nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	t.Run("header", func(t *testing.T) {
		if !strings.HasPrefix(output, "// Code generated by trpcgo. DO NOT EDIT.") {
			t.Errorf("missing or wrong header")
		}
	})

	t.Run("UserOutput interface fields", func(t *testing.T) {
		// Must contain the interface with correct field types.
		mustContain := []string{
			"export interface UserOutput {",
			"name: string;",
			"email: string;",
		}
		for _, s := range mustContain {
			if !strings.Contains(output, s) {
				t.Errorf("missing %q in output:\n%s", s, output)
			}
		}
	})

	t.Run("embedded struct fields flattened", func(t *testing.T) {
		// Base fields should appear in UserOutput, not as a separate Base type.
		if !strings.Contains(output, "id: string;") {
			t.Errorf("embedded Base.ID field missing from UserOutput:\n%s", output)
		}
		if !strings.Contains(output, "createdAt: string;") {
			t.Errorf("time.Time should map to string:\n%s", output)
		}
	})

	t.Run("optional fields", func(t *testing.T) {
		// Pointer field and omitempty field should be optional.
		if !strings.Contains(output, "deletedAt?:") {
			t.Errorf("pointer field should be optional:\n%s", output)
		}
		if !strings.Contains(output, "meta?:") {
			t.Errorf("omitempty field should be optional:\n%s", output)
		}
		if !strings.Contains(output, "tags?:") {
			t.Errorf("omitempty tags should be optional:\n%s", output)
		}
	})

	t.Run("UserInput interface", func(t *testing.T) {
		if !strings.Contains(output, "export interface UserInput {") {
			t.Errorf("missing UserInput interface:\n%s", output)
		}
	})

	t.Run("type mappings", func(t *testing.T) {
		// []string → string[]
		if !strings.Contains(output, "string[]") {
			t.Errorf("[]string should map to string[]:\n%s", output)
		}
		// map[string]string → Record<string, string>
		if !strings.Contains(output, "Record<string, string>") {
			t.Errorf("map[string]string should map to Record<string, string>:\n%s", output)
		}
	})

	t.Run("procedure types", func(t *testing.T) {
		if !strings.Contains(output, "$Mutation<UserInput, UserOutput>") {
			t.Errorf("missing mutation type:\n%s", output)
		}
		if !strings.Contains(output, "$Subscription<void, UserOutput>") {
			t.Errorf("missing subscription type:\n%s", output)
		}
	})

	t.Run("nested namespace", func(t *testing.T) {
		if !strings.Contains(output, "user: {") {
			t.Errorf("missing user namespace:\n%s", output)
		}
	})

	t.Run("list returns array type", func(t *testing.T) {
		if !strings.Contains(output, "$Query<void, UserOutput[]>") {
			t.Errorf("list query should return UserOutput[]:\n%s", output)
		}
	})

	t.Run("AppRouter structure", func(t *testing.T) {
		for _, s := range []string{
			"export type AppRouter =",
			"type AppRouterRecord =",
			"TRPCRouterDef<$RootTypes, AppRouterRecord>",
			"TRPCRouterCaller<$RootTypes, AppRouterRecord>",
		} {
			if !strings.Contains(output, s) {
				t.Errorf("missing %q:\n%s", s, output)
			}
		}
	})
}

func TestGenerateTSNoProcedures(t *testing.T) {
	router := trpcgo.NewRouter()

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	// Should still produce valid TypeScript with empty router record.
	if !strings.Contains(output, "export type AppRouter =") {
		t.Errorf("empty router should still produce AppRouter type:\n%s", output)
	}
	// Should not contain any interfaces.
	if strings.Contains(output, "export interface") {
		t.Errorf("empty router should not produce interfaces:\n%s", output)
	}
}

func TestGenerateTSVoidInput(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	if !strings.Contains(output, "$Query<void, string>") {
		t.Errorf("expected $Query<void, string>, got:\n%s", output)
	}
	// Should have no interfaces for primitive types.
	if strings.Contains(output, "export interface") {
		t.Errorf("primitive-only procedures should not generate interfaces:\n%s", output)
	}
}

func TestGenerateTSIdempotent(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	dir := t.TempDir()
	path1 := filepath.Join(dir, "first.ts")
	path2 := filepath.Join(dir, "second.ts")

	if err := router.GenerateTS(path1); err != nil {
		t.Fatal(err)
	}
	if err := router.GenerateTS(path2); err != nil {
		t.Fatal(err)
	}

	data1, _ := os.ReadFile(path1)
	data2, _ := os.ReadFile(path2)

	if string(data1) != string(data2) {
		t.Errorf("GenerateTS is not idempotent:\nfirst:\n%s\nsecond:\n%s", data1, data2)
	}
}

func TestGenerateTSTrackedEventWrapper(t *testing.T) {
	type Notification struct {
		Message string `json:"message"`
	}

	router := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(router, "notifications", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[Notification], error) {
		return nil, nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	// The subscription output must match httpSubscriptionLink's onData value.
	if !strings.Contains(output, "$Subscription<void, { id: string; data: Notification }>") {
		t.Errorf("TrackedEvent should expose the client tracking wrapper:\n%s", output)
	}
	if strings.Contains(output, "TrackedEvent") {
		t.Errorf("TrackedEvent should not appear in output:\n%s", output)
	}
	if !strings.Contains(output, "export interface Notification") {
		t.Errorf("Notification interface should be emitted:\n%s", output)
	}
}

// A generic map declaration constrains its key parameter to what TypeScript
// accepts as a mapped-type key, so the static mapper's generic declarations
// type-check for every instantiation. The reflection mapper emits concrete
// declarations and must agree on the parsed shapes.
func TestGenerateTSGenericMapKeyConstraints(t *testing.T) {
	script := `
import { GenericMapInputSchema } from './schemas';
import type { GenericMapInput } from './trpc';
const input: GenericMapInput = { names: { a: 1 }, regions: { north: true }, counts: { 3: 'x' }, lookup: { k: 2 }, ids: { 7: 'y' }, listed: ['north'] };
const parsed = GenericMapInputSchema.parse(input);
const count: number = parsed.names.a;
const flag: boolean = parsed.regions.north;
const label: string = parsed.counts[3];
const id: number = parsed.lookup.k;
const name: string = parsed.ids[7];
const listed: string[] = parsed.listed;
if (count !== 1 || flag !== true || label !== 'x' || id !== 2 || name !== 'y' || listed[0] !== 'north') throw new Error('generic maps lost values: ' + JSON.stringify(parsed));
`
	runTypeGraphContract(t, []string{"GenericMapInput"}, script)
}
