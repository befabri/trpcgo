package validationcontract

import (
	"time"
)

type FormatULID struct {
	Value string `json:"value" validate:"ulid"`
}

type FormatEmail struct {
	Value string `json:"value" validate:"email"`
}

type FormatURL struct {
	Value string `json:"value" validate:"url"`
}

type FormatTime struct {
	Value time.Time `json:"value"`
}

type FormatIP struct {
	Value string `json:"value" validate:"ip"`
}

type FormatIPv4 struct {
	Value string `json:"value" validate:"ipv4"`
}

type FormatIPv6 struct {
	Value string `json:"value" validate:"ipv6"`
}

type FormatAlphaUnicode struct {
	Value string `json:"value" validate:"alphaunicode"`
}

type FormatAlphanumUnicode struct {
	Value string `json:"value" validate:"alphanumunicode"`
}

type FormatLowercase struct {
	Value string `json:"value" validate:"lowercase"`
}

type FormatUppercase struct {
	Value string `json:"value" validate:"uppercase"`
}

type FormatTimeEqual struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"eqfield=A"`
}

type FormatTimeGreater struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"gtfield=A"`
}

// Tag fixtures give each supported tag that other families exercise only
// incidentally a type of its own, so accepted and rejected controls isolate
// that tag. Expected results follow go-playground/validator v10.30.4.
type TagAlphanum struct {
	Value string `json:"value" validate:"alphanum"`
}

type TagBase64 struct {
	Value string `json:"value" validate:"base64"`
}

type TagBase64RawURL struct {
	Value string `json:"value" validate:"base64rawurl"`
}

type TagCIDRv6 struct {
	Value string `json:"value" validate:"cidrv6"`
}

type TagE164 struct {
	Value string `json:"value" validate:"e164"`
}

type TagExcludesAll struct {
	Value string `json:"value" validate:"excludesall=!?"`
}

type TagHostnameRFC1123 struct {
	Value string `json:"value" validate:"hostname_rfc1123"`
}

type TagJWT struct {
	Value string `json:"value" validate:"jwt"`
}

type TagLessThanNumber struct {
	Value int `json:"value" validate:"lt=10"`
}

type TagLessThanLength struct {
	Value string `json:"value" validate:"lt=3"`
}

type TagNumber struct {
	Value string `json:"value" validate:"number"`
}

type TagPrintASCII struct {
	Value string `json:"value" validate:"printascii"`
}

type TagULID struct {
	Value string `json:"value" validate:"ulid"`
}
