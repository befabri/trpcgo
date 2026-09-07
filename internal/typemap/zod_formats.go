package typemap

// These predicates intentionally follow go-playground/validator's grammar.
// A Zod format with the same name can enforce a different contract (UUID
// versions, Base64 padding, MAC encodings, or CIDR network alignment).
var zodStringRegexes = map[string]string{
	"alpha":      `/^[a-zA-Z]+$(?![\s\S])/`,
	"alphanum":   `/^[a-zA-Z0-9]+$(?![\s\S])/`,
	"numeric":    `/^[-+]?[0-9]+(?:\.[0-9]+)?$(?![\s\S])/`,
	"number":     `/^[0-9]+$(?![\s\S])/`,
	"ascii":      `/^[\x00-\x7F]*$(?![\s\S])/`,
	"printascii": `/^[\x20-\x7E]*$(?![\s\S])/`,
}

var zodFormatBases = map[string]string{
	"alphaunicode":     `z.string().check(z.refine((value) => ` + zodGoUnicodePredicate("alphaunicode", ZodGoStringValue("value")) + `))`,
	"alphanumunicode":  `z.string().check(z.refine((value) => ` + zodGoUnicodePredicate("alphanumunicode", ZodGoStringValue("value")) + `))`,
	"email":            "z.string().check(z.refine((" + goEmailValidator + ")))",
	"url":              "z.string().check(z.refine((" + goURLValidator + ")))",
	"uuid":             `z.string().check(z.regex(/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$(?![\s\S])/))`,
	"e164":             `z.string().check(z.regex(/^\+?[1-9][0-9]{7,14}$(?![\s\S])/))`,
	"jwt":              `z.string().check(z.regex(/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*$(?![\s\S])/))`,
	"base64":           `z.string().check(z.regex(/^(?:[A-Za-z0-9+\/]{4})*(?:[A-Za-z0-9+\/]{2}==|[A-Za-z0-9+\/]{3}=|[A-Za-z0-9+\/]{4})$(?![\s\S])/))`,
	"base64url":        `z.string().check(z.regex(/^(?:[A-Za-z0-9_-]{4})*(?:[A-Za-z0-9_-]{2}==|[A-Za-z0-9_-]{3}=|[A-Za-z0-9_-]{4})$(?![\s\S])/))`,
	"base64rawurl":     `z.string().check(z.regex(/^(?:[A-Za-z0-9_-]{4})*(?:[A-Za-z0-9_-]{2,4})$(?![\s\S])/))`,
	"hostname":         `z.string().check(z.regex(/^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$(?![\s\S])/))`,
	"hostname_rfc1123": `z.string().check(z.regex(/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$(?![\s\S])/))`,
	"hexadecimal":      `z.string().check(z.regex(/^(0[xX])?[0-9a-fA-F]+$(?![\s\S])/))`,
	"ulid":             `z.string().check(z.regex(/^[A-HJKMNP-TV-Z0-9\u017f\u212a]{26}$(?![\s\S])/i))`,
	"mac":              `z.string().check(z.regex(/^(?:(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}:){7}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}:){19}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){5}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){7}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{2}-){19}[0-9a-fA-F]{2}|(?:[0-9a-fA-F]{4}\.){2}[0-9a-fA-F]{4}|(?:[0-9a-fA-F]{4}\.){3}[0-9a-fA-F]{4}|(?:[0-9a-fA-F]{4}\.){9}[0-9a-fA-F]{4}|[0-9a-fA-F]{12}|[0-9a-fA-F]{16}|[0-9a-fA-F]{40})$(?![\s\S])/))`,
	"ip":               `z.string().check(z.refine((value) => (` + goIPBytes + `)(value) !== null))`,
	"ipv4":             `z.string().check(z.refine((value) => { const ip = (` + goIPBytes + `)(value); return ip !== null && ip.v4; }))`,
	"ipv6":             `z.string().check(z.refine((value) => { const ip = (` + goIPBytes + `)(value); return ip !== null && !ip.v4; }))`,
	"cidrv4":           `z.string().check(z.refine((value) => { const parts = value.split("/"); if (parts.length !== 2 || !/^[0-9]+$(?![\s\S])/.test(parts[1]!)) return false; const ip = (` + goIPBytes + `)(parts[0]!); if (!ip || !ip.v4) return false; let bits = Number(parts[1]); if (ip.bytes.length === 16) bits -= 96; if (bits < 0 || bits > 32) return false; const bytes = ip.bytes.slice(-4); return bytes.every((byte, index) => { const host = Math.max(0, Math.min(8, (index + 1) * 8 - bits)); return (byte & (2 ** host - 1)) === 0; }); }))`,
	"cidrv6":           `z.string().check(z.refine((value) => { const parts = value.split("/"); if (parts.length !== 2 || !/^[0-9]+$(?![\s\S])/.test(parts[1]!)) return false; const ip = (` + goIPBytes + `)(parts[0]!); return ip !== null && !ip.v4 && Number(parts[1]) <= 128; }))`,
}
