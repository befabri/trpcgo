package typemap

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

// These are independent standard-library oracles for the exact operations used
// by validator v10.30.4, plus time.Time's JSON decoding contract.
func TestGoFormatHelpers(t *testing.T) {
	emailPattern := regexp.MustCompile(goEmailPattern)
	emailOracle := func(value string) bool {
		_, err := mail.ParseAddress(value)
		return err == nil && emailPattern.MatchString(value)
	}
	urlOracle := func(value string) bool {
		parsed, err := url.Parse(strings.ToLower(value))
		if err != nil || parsed.Scheme == "" {
			return false
		}
		if parsed.Scheme == "file" {
			return parsed.Path != "" && parsed.Path != "/"
		}
		return parsed.Host != "" || parsed.Fragment != "" || parsed.Opaque != ""
	}
	type vector struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
		Want  any    `json:"want"`
	}
	vectors := make([]vector, 0)
	emails := []string{"", "a@b.co", "a+b@example.com", "a..b@example.com", "a.@example.com", "a@b..co", "a@example.com.", "a@-example.com", "a@foo~bar.example", "Name <a@example.com>", "a(comment)@example.com", "a@example.com (comment)", "\"\"@example.com", "\" \"@example.com", "\"a\\\"b\"@example.com", "\"a\\ b\"@example.com", "\"a\\\"@example.com", "\"a\t b\"@example.com", "\"a\r\n b\"@example.com", "\"a@b\"@example.com", "é@例.example", "𝒜@example.com"}
	for _, local := range []string{"a", "a.b", "a..b", ".a", "a.", "\"a\"", "\"a b\"", "\"a\\b\"", "\"a\"b\"", "a(b)", "a b", "\"\""} {
		for _, domain := range []string{"a.b", "a.b.", "a..b", "a~b.c", "a_b.c", "a-.b", "a.-b", "a.1", "a.b1", "[127.0.0.1]", "例.测试"} {
			emails = append(emails, local+"@"+domain)
		}
	}
	for point := rune(0); point <= 255; point++ {
		emails = append(emails, "a"+string(point)+"@example.com", "\"a"+string(point)+"b\"@example.com", "\"a\\"+string(point)+"b\"@example.com", "a@x"+string(point)+"x.co")
	}
	for _, value := range emails {
		vectors = append(vectors, vector{"email", value, emailOracle(value)})
	}
	urls := []string{"", "example.com", "http://example.com", "mailto:a@example.com", "urn:abc:def", "http:x y", "http:x%zz", "http:", "http:#fragment", "http:?x#f", "file:a", "file:/", "file://host", "file:///tmp", "file:/%2f", "http:///path", "http:///path#f", "http://a b", "http://a/%zz", "http://a/?%zz", "http://a/#%zz", "http://a/#x\n", "http://a/\n#x", "http://[::1]", "http://[127.0.0.1]", "http://[::ffff:127.0.0.1]", "http://[fe80::1%25eth0]", "http://[fe80::1%25]", "http://[fe80::1%25a%20b]", "http://[v1.test]", "http://prefix[::1]", "http://[:::]", "http://host:99999999", "http://host:", "http://a:b:12", "postgres://a:b:12", "http://é@example.com", "http://%ff@example.com", "http://%ff/", "http://%41/", "http://a%25b/", "HTTP://例.test/"}
	for _, scheme := range []string{"http:", "file:", "postgresql:", "x:", "1:", "+x:"} {
		for _, host := range []string{"a", "a:123", "a:12:3", "[::]", "[::%25x]", "foo[::]", "a b", "a\\b", "a%25b", "a%7fb", "a%ffb", "", "user:pass@a", "@a"} {
			for _, suffix := range []string{"", "/", "/x", "?%zz", "#f", "#%zz"} {
				urls = append(urls, scheme+"//"+host+suffix)
			}
		}
	}
	for point := rune(0); point <= 255; point++ {
		urls = append(urls, "http://x"+string(point)+"x/", "http://a/a"+string(point)+"b", "http://a/#a"+string(point)+"b", "http://a"+string(point)+"b@host/")
	}
	for point := rune(128); point <= unicode.MaxRune; point++ {
		if unicode.ToLower(point) != point {
			urls = append(urls, string(point)+":opaque", "http://"+string(point)+"@example.com", "http://"+string(point)+".example/")
		}
	}
	for _, value := range urls {
		vectors = append(vectors, vector{"url", value, urlOracle(value)})
	}
	ips := []string{"", "127.0.0.1", "01.2.3.4", "0.0.0.0", "256.1.2.3", "127.1", "::", "::1", "fe80::1%eth0", "::ffff:192.0.2.1", "::192.0.2.1", "1:2:3:4:5:6:7:8", "1:2:3:4:5:6:7::8", ":::1", "1::2::3", "1:2:3:4:5:192.0.2.1", "1:2:3:4:5:6:192.0.2.1", "1:2:3:4:5::192.0.2.1"}
	rng := rand.New(rand.NewPCG(0x49505636, 0x474f464d54))
	for range 300 {
		var bytes [16]byte
		for i := range bytes {
			bytes[i] = byte(rng.Uint32())
		}
		ips = append(ips, net.IP(bytes[:]).String())
	}
	for _, value := range ips {
		ip := net.ParseIP(value)
		var want any
		if ip != nil {
			bytes := ip
			if !strings.Contains(value, ":") {
				bytes = ip.To4()
			}
			points := make([]int, len(bytes))
			for i, b := range bytes {
				points[i] = int(b)
			}
			want = map[string]any{"bytes": points, "v4": ip.To4() != nil}
		}
		vectors = append(vectors, vector{"ip", value, want})
	}
	times := []string{"", "2024-01-02T03:04:05Z", "2024-01-02T3:04:05Z", "2024-01-02T03:04:05,123456789123Z", "2024-01-02T03:04:05+24:60", "2024-01-02T03:04:05-24:60", "2024-01-02T03:04:05+25:00", "2024-01-02T03:04:05+23:61", "2024-02-29T00:00:00Z", "2023-02-29T00:00:00Z", "0000-02-29T00:00:00Z", "0000-01-01T00:00:00+24:60", "9999-12-31T23:59:59-24:60", "2024-01-02T24:00:00Z", "2024-01-02T03:04:60Z", "2024-01-02T03:04:05.Z", "2024-01-02t03:04:05z", "2024-01-02T03:04Z", "2024-01-02T 3:04:05Z"}
	for _, year := range []int{0, 1, 4, 100, 400, 1600, 1900, 1970, 2000, 2024, 9999} {
		for month := 0; month <= 13; month++ {
			for _, day := range []int{0, 1, 28, 29, 30, 31, 32} {
				times = append(times, fmt.Sprintf("%04d-%02d-%02dT3:04:05.12345678901-24:60", year, month, day))
			}
		}
	}
	for _, value := range times {
		encoded, _ := json.Marshal(value)
		var parsed time.Time
		var want any
		if json.Unmarshal(encoded, &parsed) == nil {
			want = []int64{parsed.Unix(), int64(parsed.Nanosecond())}
		}
		vectors = append(vectors, vector{"time", value, want})
	}
	encoded, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	body := "const implementations = { email: (" + goEmailValidator + "), url: (" + goURLValidator + "), ip: (" + goIPBytes + "), time: (" + goTimeParts + ") };\n"
	declarations, body := HoistZodRuntimeHelpers(body)
	script := declarations + body + "const vectors = " + string(encoded) + `; const failures: unknown[] = []; for (const test of vectors) { const got = implementations[test.kind as keyof typeof implementations](test.value); if (JSON.stringify(got) !== JSON.stringify(test.want)) failures.push({...test, got}); } if (failures.length) throw new Error(JSON.stringify(failures));`
	runTypeScript(t, script)
	t.Logf("verified %d format syntax and parsed-value vectors against Go", len(vectors))
}

func runTypeScript(t *testing.T, script string) {
	t.Helper()
	tsx, err := filepath.Abs("../../testdata/zodruntime/node_modules/.bin/tsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tsx); err != nil {
		if os.Getenv("CI") != "" || !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Skip("run npm ci --prefix testdata/zodruntime to execute runtime contracts")
	}
	file := filepath.Join(t.TempDir(), "contract.ts")
	if err := os.WriteFile(file, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), tsx, file).CombinedOutput(); err != nil {
		t.Fatalf("runtime helper differs from Go: %v\n%s", err, output)
	}
}

// Cover the entire scalar range, including unassigned characters where a newer
// JavaScript engine's Unicode categories would otherwise change validation.
func TestGoUnicodeHelpers(t *testing.T) {
	var runs [][3]int
	for point := rune(0); point <= unicode.MaxRune; point++ {
		mask := 0
		if unicode.IsLetter(point) {
			mask |= 1
		}
		if unicode.IsNumber(point) {
			mask |= 2
		}
		if strings.ToLower(string(point)) != string(point) {
			mask |= 4
		}
		if strings.ToUpper(string(point)) != string(point) {
			mask |= 8
		}
		if len(runs) > 0 && runs[len(runs)-1][2] == mask {
			runs[len(runs)-1][1] = int(point)
		} else {
			runs = append(runs, [3]int{int(point), int(point), mask})
		}
	}
	encoded, _ := json.Marshal(runs)
	body := "const predicates = [(" + goUnicodeLetter + "),(" + goUnicodeNumber + "),(" + goUnicodeLowerChanges + "),(" + goUnicodeUpperChanges + ")];\n"
	declarations, body := HoistZodRuntimeHelpers(body)
	script := declarations + body + "const runs = " + string(encoded) + `; for (const [start,end,want] of runs) { for (let point=start!;point<=end!;point++) { let got=0; predicates.forEach((test,index)=>{if(test(point)) got|=1<<index;}); if(got!==want) throw new Error(JSON.stringify({point,want,got})); } }`
	runTypeScript(t, script)
	t.Logf("Go Unicode %s: complete scalar range verified; emitted letter=%d bytes, number=%d bytes, lowercase=%d bytes, uppercase=%d bytes; all helpers=%d bytes", unicode.Version, len(goUnicodeLetter), len(goUnicodeNumber), len(goUnicodeLowerChanges), len(goUnicodeUpperChanges), len(declarations))
}

func TestFormatHelperHoisting(t *testing.T) {
	source := zodFormatBases["url"] + ";" + zodFormatBases["url"] + ";" + zodFormatBases["ip"] + ";" + zodFormatBases["alphaunicode"]
	declarations, body := HoistZodRuntimeHelpers(source)
	for _, name := range []string{"$trpcgoURL", "$trpcgoIPBytes", "$trpcgoUnicodeLetter"} {
		if strings.Count(declarations, "const "+name+" = ") != 1 {
			t.Errorf("helper %s was not emitted exactly once", name)
		}
	}
	if strings.Contains(declarations, "const $trpcgoUnicodeNumber") || strings.Contains(declarations, "const $trpcgoEmail") {
		t.Fatal("unreferenced helpers were emitted")
	}
	if strings.Contains(body, goIPBytes) || strings.Count(declarations, goIPBytes) != 1 {
		t.Fatal("URL dependency retained an inline IP decoder")
	}
	if _, again := HoistZodRuntimeHelpers(body); again != body {
		t.Fatal("hoisted body was not stable")
	}
}
