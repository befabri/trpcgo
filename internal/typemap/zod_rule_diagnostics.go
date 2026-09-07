package typemap

import "fmt"

// ValidateZodFieldRules checks restrictions that require preserved field
// metadata rather than just its top-level Go kind. Callers add their complete
// field/key/element path when reporting the error.
func ValidateZodFieldRules(f Field) error { return validateZodTypedRules(f, f.Validate) }

func validateZodTypedRules(f Field, rules []ValidateRule) error {
	for _, rule := range rules {
		if len(rule.Alternatives) > 0 {
			if err := validateZodTypedRules(f, rule.Alternatives); err != nil {
				return err
			}
			continue
		}
		if rule.Tag == "unique" {
			if _, _, err := zodUniqueType(f, rule.Param); err != nil {
				return fmt.Errorf("unique%s: %w", uniqueParam(rule.Param), err)
			}
		}
	}
	return nil
}

func uniqueParam(param string) string {
	if param == "" {
		return ""
	}
	return "=" + param
}

func zodUniqueType(f Field, param string) (*GoEqualityType, *GoEqualityField, error) {
	if f.GoKind != "slice" && f.GoKind != "array" && f.GoKind != "map" {
		return nil, nil, fmt.Errorf("requires an array, slice or map")
	}
	if f.Element == nil {
		return nil, nil, fmt.Errorf("requires concrete Go comparable-value metadata")
	}
	d := f.Element.Equality
	if d == nil {
		// Compatibility for manually supplied scalar metadata.
		if f.Element.GoKind == "string" || f.Element.GoKind == "bool" || isNumericKind(f.Element.GoKind) {
			d = &GoEqualityType{Kind: f.Element.GoKind, Pointer: f.Element.IsPointer}
		} else {
			return nil, nil, fmt.Errorf("requires concrete Go comparable-value metadata for %s", f.Element.GoKind)
		}
	}
	// The backend ignores the parameter on map values.
	if param != "" && f.GoKind != "map" {
		if d.Kind != "struct" {
			return nil, nil, fmt.Errorf("field selection requires Go struct elements")
		}
		var selected *GoEqualityField
		for index := range d.Fields {
			if d.Fields[index].GoName == param && d.Fields[index].Selectable {
				if selected != nil {
					return nil, nil, fmt.Errorf("field %s is ambiguous in the JSON schema", param)
				}
				selected = &d.Fields[index]
			}
		}
		if selected == nil {
			return nil, nil, fmt.Errorf("field %s is missing, unexported, or unavailable in the JSON schema", param)
		}
		if selected.Type.Error != "" {
			return nil, nil, fmt.Errorf("%s", selected.Type.Error)
		}
		return d, selected, nil
	}
	if d.Error != "" {
		return nil, nil, fmt.Errorf("%s", d.Error)
	}
	return d, nil, nil
}
