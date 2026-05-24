package trpc

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/befabri/trpcgo"
)

// HandlerOption configures the HTTP tRPC handler.
type HandlerOption func(*handlerOptions)

// CORSConfig configures Cross-Origin Resource Sharing for the tRPC handler.
// CORS is disabled unless WithCORS is supplied. CORS origins control browser
// read access only; use WithTrustedOrigins for cross-origin POST trust.
type CORSConfig struct {
	// AllowedOrigins contains exact scheme+host origins such as
	// "https://app.example.com". The wildcard "*" is allowed only for
	// non-credentialed CORS responses and does not grant CSRF trust.
	AllowedOrigins []string
	// AllowedMethods defaults to GET and POST when empty.
	AllowedMethods []string
	// AllowedHeaders defaults to Authorization, Content-Type, Last-Event-Id,
	// and trpc-accept when empty. When set, it replaces that default list.
	AllowedHeaders []string
	// ExposedHeaders lists response headers visible to browser JavaScript.
	ExposedHeaders []string
	// AllowCredentials controls Access-Control-Allow-Credentials. Do not use it
	// with wildcard origins.
	AllowCredentials bool
	// MaxAge controls Access-Control-Max-Age for preflight responses.
	MaxAge time.Duration
}

type handlerOptions struct {
	enforceContentType      bool
	checkSubscriptionOrigin bool
	cors                    corsOptions
	csrf                    csrfOptions
}

type corsOptions struct {
	enabled          bool
	allowedOrigins   map[string]bool
	allowWildcard    bool
	allowedMethods   []string
	allowedHeaders   []string
	exposedHeaders   []string
	allowCredentials bool
	maxAge           time.Duration
}

type csrfOptions struct {
	enabled        bool
	requireOrigin  bool
	publicOrigins  map[string]bool
	trustedOrigins map[string]bool
}

func defaultHandlerOptions() handlerOptions {
	return handlerOptions{
		enforceContentType: true,
		cors: corsOptions{
			allowedMethods: []string{http.MethodGet, http.MethodPost},
			allowedHeaders: []string{"Authorization", "Content-Type", "Last-Event-Id", "trpc-accept"},
		},
		csrf: csrfOptions{
			enabled:        true,
			publicOrigins:  map[string]bool{},
			trustedOrigins: map[string]bool{},
		},
	}
}

// WithContentTypeEnforcement controls whether POST requests with bodies must
// use Content-Type: application/json. Charset parameters are accepted and
// empty-body POST requests are not checked. Enforcement is enabled by default.
func WithContentTypeEnforcement(enabled bool) HandlerOption {
	return func(o *handlerOptions) {
		o.enforceContentType = enabled
	}
}

// WithCSRFProtection controls the built-in Origin/Referer CSRF protection for
// POST requests. Protection is enabled by default. Same-origin requests and
// origins added with WithTrustedOrigins are allowed.
func WithCSRFProtection(enabled bool) HandlerOption {
	return func(o *handlerOptions) {
		o.csrf.enabled = enabled
	}
}

// WithSubscriptionOriginCheck controls an opt-in Origin/Referer check for
// subscription requests. It targets browser GET/SSE subscriptions, which are
// not covered by the POST-only CSRF check. When enabled, a subscription request
// carrying an Origin or Referer must be same-origin, a configured public or
// trusted origin, or allowed by CORS. Cookie-bearing subscription requests
// without those headers are rejected; non-cookie requests without those headers
// are allowed for non-browser clients. Subscriptions sent via POST pass through
// the normal CSRF check first, which honors same-origin, public, and trusted
// origins.
func WithSubscriptionOriginCheck(enabled bool) HandlerOption {
	return func(o *handlerOptions) {
		o.checkSubscriptionOrigin = enabled
	}
}

// WithCSRFRequireOrigin controls whether CSRF protection rejects every POST
// request that lacks both Origin and Referer. By default, missing Origin and
// Referer are allowed for non-browser API clients, but cookie-bearing POSTs are
// still rejected when both headers are absent.
func WithCSRFRequireOrigin(enabled bool) HandlerOption {
	return func(o *handlerOptions) {
		o.csrf.requireOrigin = enabled
	}
}

// WithPublicOrigin adds public origins that should be considered same-origin
// for CSRF and subscription origin checks. This is useful behind
// TLS-terminating reverse proxies where the Go server receives internal http
// requests while browsers use a public https origin. Origins must be exact
// scheme+host values such as
// "https://api.example.com".
func WithPublicOrigin(origin string) HandlerOption {
	return WithPublicOrigins(origin)
}

// WithPublicOrigins adds public origins that should be considered same-origin
// for CSRF and subscription origin checks. Invalid origins are ignored.
func WithPublicOrigins(origins ...string) HandlerOption {
	return func(o *handlerOptions) {
		ensurePublicOrigins(o)
		for _, origin := range origins {
			if normalized, ok := parseConfigOrigin(origin); ok {
				o.csrf.publicOrigins[normalized] = true
			}
		}
	}
}

// WithTrustedOrigins adds origins that may send cross-origin POST requests.
// Origins must be exact scheme+host values such as "https://app.example.com".
// Invalid origins are ignored.
func WithTrustedOrigins(origins ...string) HandlerOption {
	return func(o *handlerOptions) {
		ensureTrustedOrigins(o)
		for _, origin := range origins {
			if normalized, ok := parseConfigOrigin(origin); ok {
				o.csrf.trustedOrigins[normalized] = true
			}
		}
	}
}

// WithCORS enables CORS for the configured origins. CORS does not grant CSRF
// trust; use WithTrustedOrigins for cross-origin POSTs. If AllowedHeaders is
// set, it replaces the default CORS header allow-list.
func WithCORS(config CORSConfig) HandlerOption {
	return func(o *handlerOptions) {
		o.cors.enabled = true
		if o.cors.allowedOrigins == nil {
			o.cors.allowedOrigins = map[string]bool{}
		}
		for _, origin := range config.AllowedOrigins {
			origin = strings.TrimSpace(origin)
			if origin == "*" {
				o.cors.allowWildcard = true
				continue
			}
			if normalized, ok := parseConfigOrigin(origin); ok {
				o.cors.allowedOrigins[normalized] = true
			}
		}
		if len(config.AllowedMethods) > 0 {
			o.cors.allowedMethods = config.AllowedMethods
		}
		if len(config.AllowedHeaders) > 0 {
			o.cors.allowedHeaders = config.AllowedHeaders
		}
		o.cors.exposedHeaders = config.ExposedHeaders
		o.cors.allowCredentials = config.AllowCredentials
		o.cors.maxAge = config.MaxAge
	}
}

func ensurePublicOrigins(o *handlerOptions) {
	if o.csrf.publicOrigins == nil {
		o.csrf.publicOrigins = map[string]bool{}
	}
}

func ensureTrustedOrigins(o *handlerOptions) {
	if o.csrf.trustedOrigins == nil {
		o.csrf.trustedOrigins = map[string]bool{}
	}
}

func (h *Handler) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	if !h.opts.cors.enabled {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		addVary(w.Header(), "Origin")
	}
	allowedOrigin, ok := h.corsAllowedOrigin(origin)
	if ok {
		h.applyCORSHeaders(w.Header(), allowedOrigin)
	}
	if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
		return false
	}
	addVary(w.Header(), "Access-Control-Request-Method", "Access-Control-Request-Headers")
	if !ok {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "CORS origin not allowed"), "", nil, "")
		return true
	}
	method := r.Header.Get("Access-Control-Request-Method")
	if !containsToken(h.opts.cors.allowedMethods, method) {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeMethodNotSupported, "CORS method not allowed"), "", nil, "")
		return true
	}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(h.opts.cors.allowedMethods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(h.opts.cors.allowedHeaders, ", "))
	if h.opts.cors.maxAge > 0 {
		// Round up to whole seconds: a positive MaxAge means the caller wants
		// preflight caching, so a sub-second value must not truncate to 0 ("do
		// not cache"). int64 avoids overflow on 32-bit builds.
		seconds := int64(h.opts.cors.maxAge / time.Second)
		if h.opts.cors.maxAge%time.Second != 0 {
			seconds++
		}
		w.Header().Set("Access-Control-Max-Age", strconv.FormatInt(seconds, 10))
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

func (h *Handler) applyCORSHeaders(header http.Header, allowedOrigin string) {
	header.Set("Access-Control-Allow-Origin", allowedOrigin)
	if h.opts.cors.allowCredentials {
		header.Set("Access-Control-Allow-Credentials", "true")
	}
	if len(h.opts.cors.exposedHeaders) > 0 {
		header.Set("Access-Control-Expose-Headers", strings.Join(h.opts.cors.exposedHeaders, ", "))
	}
}

func (h *Handler) corsAllowedOrigin(origin string) (string, bool) {
	normalized, ok := originHeaderValue(origin)
	if !ok {
		return "", false
	}
	if h.opts.cors.allowedOrigins[normalized] {
		return strings.TrimSpace(origin), true
	}
	if h.opts.cors.allowWildcard && !h.opts.cors.allowCredentials {
		return "*", true
	}
	return "", false
}

func (h *Handler) rejectInvalidContentType(w http.ResponseWriter, r *http.Request) bool {
	if !h.opts.enforceContentType || r.Method != http.MethodPost || !requestHasBody(r) {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeUnsupportedMedia, "POST requests with a body must use Content-Type: application/json"), "", nil, "")
		return true
	}
	return false
}

func requestHasBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	return r.ContentLength != 0
}

func (h *Handler) rejectCSRF(w http.ResponseWriter, r *http.Request) bool {
	if !h.opts.csrf.enabled || r.Method != http.MethodPost {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		if h.trustedRequestOrigin(r, origin, false) {
			return false
		}
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "CSRF origin not allowed"), "", nil, "")
		return true
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		if h.trustedRequestOrigin(r, referer, true) {
			return false
		}
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "CSRF referer not allowed"), "", nil, "")
		return true
	}
	if h.opts.csrf.requireOrigin || r.Header.Get("Cookie") != "" {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "CSRF origin required"), "", nil, "")
		return true
	}
	return false
}

func (h *Handler) rejectSubscriptionOrigin(w http.ResponseWriter, r *http.Request, calls []parsedRequest) bool {
	if !h.opts.checkSubscriptionOrigin || !h.hasSubscriptionCall(calls) {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		addVary(w.Header(), "Origin")
		if h.allowedSubscriptionOrigin(r, origin, false) {
			return false
		}
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "subscription origin not allowed"), "", nil, "")
		return true
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		addVary(w.Header(), "Referer")
		if h.allowedSubscriptionOrigin(r, referer, true) {
			return false
		}
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "subscription referer not allowed"), "", nil, "")
		return true
	}
	if r.Header.Get("Cookie") != "" {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeForbidden, "subscription origin required"), "", nil, "")
		return true
	}
	return false
}

func (h *Handler) hasSubscriptionCall(calls []parsedRequest) bool {
	for _, call := range calls {
		proc, ok := h.procedures.Lookup(call.path)
		if ok && proc.Type() == trpcgo.ProcedureSubscription {
			return true
		}
	}
	return false
}

func (h *Handler) allowedSubscriptionOrigin(r *http.Request, value string, allowExtraParts bool) bool {
	origin, ok := parseOrigin(value, allowExtraParts)
	if !ok {
		return false
	}
	if reqOrigin, ok := requestOrigin(r); ok && origin == reqOrigin {
		return true
	}
	if h.opts.csrf.publicOrigins[origin] {
		return true
	}
	// Trusted origins are explicitly allowed to send cross-origin POSTs, a
	// stronger trust signal than CORS read access, so they may subscribe too.
	// This also lets users with external CORS middleware (no trpc.WithCORS)
	// enable the check via WithTrustedOrigins.
	if h.opts.csrf.trustedOrigins[origin] {
		return true
	}
	return h.corsAllowsOrigin(origin, r.Header.Get("Cookie") != "")
}

func (h *Handler) corsAllowsOrigin(origin string, hasCookie bool) bool {
	if !h.opts.cors.enabled {
		return false
	}
	if h.opts.cors.allowedOrigins[origin] {
		return true
	}
	// Wildcard CORS only means "any origin may read non-credentialed
	// responses". It must not let a cookie-bearing cross-site request reach a
	// subscription resolver, since the side effect would run before the browser
	// blocks the response read.
	if hasCookie {
		return false
	}
	return h.opts.cors.allowWildcard && !h.opts.cors.allowCredentials
}

func (h *Handler) trustedRequestOrigin(r *http.Request, value string, allowExtraParts bool) bool {
	origin, ok := parseOrigin(value, allowExtraParts)
	if !ok {
		return false
	}
	if reqOrigin, ok := requestOrigin(r); ok && origin == reqOrigin {
		return true
	}
	if h.opts.csrf.publicOrigins[origin] {
		return true
	}
	return h.opts.csrf.trustedOrigins[origin]
}

func requestOrigin(r *http.Request) (string, bool) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return canonicalOrigin(scheme, r.Host)
}

func originHeaderValue(value string) (string, bool) {
	return parseOrigin(value, false)
}

func parseConfigOrigin(value string) (string, bool) {
	return parseOrigin(value, false)
}

func parseOrigin(value string, allowExtraParts bool) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.User != nil {
		return "", false
	}
	if !allowExtraParts && originHasExtraParts(u) {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	return canonicalOrigin(u.Scheme, u.Host)
}

func originHasExtraParts(u *url.URL) bool {
	return u.Path != "" ||
		u.RawPath != "" ||
		u.RawQuery != "" ||
		u.ForceQuery ||
		u.Fragment != "" ||
		u.RawFragment != ""
}

func canonicalOrigin(scheme, hostport string) (string, bool) {
	scheme = strings.ToLower(scheme)
	u, err := url.Parse(scheme + "://" + hostport)
	if err != nil || u.Host == "" || u.User != nil || originHasExtraParts(u) {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	host = strings.ToLower(host)
	port := u.Port()
	if port == defaultPort(scheme) {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, true
}

func defaultPort(scheme string) string {
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func containsToken(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func addVary(header http.Header, values ...string) {
	existing := header.Get("Vary")
	seen := map[string]bool{}
	if existing != "" {
		for _, value := range strings.Split(existing, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if value == "*" {
				return
			}
			seen[strings.ToLower(value)] = true
		}
	}
	parts := make([]string, 0, len(seen)+len(values))
	if existing != "" {
		for _, value := range strings.Split(existing, ",") {
			value = strings.TrimSpace(value)
			if value != "" {
				parts = append(parts, value)
			}
		}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		parts = append(parts, value)
	}
	if len(parts) > 0 {
		header.Set("Vary", strings.Join(parts, ", "))
	}
}
