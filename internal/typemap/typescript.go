package typemap

import "strings"

// PropertyDeclaration returns the TypeScript property declaration for f, such
// as "readonly name?: string".
func PropertyDeclaration(f Field) string {
	var b strings.Builder
	if f.Readonly {
		b.WriteString("readonly ")
	}
	b.WriteString(QuotePropName(f.Name))
	if f.Optional {
		b.WriteByte('?')
	}
	b.WriteString(": ")
	b.WriteString(f.Type)
	return b.String()
}

// InlineObjectType returns an inline object type for fields; an empty list
// becomes Record<string, never>.
func InlineObjectType(fields []Field) string {
	if len(fields) == 0 {
		return "Record<string, never>"
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = PropertyDeclaration(f)
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}
