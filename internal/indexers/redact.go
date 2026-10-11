package indexers

import (
	"net/url"
	"regexp"
	"strings"
)

// redactURL scrubs credential-bearing query parameters out of any string that
// embeds an http(s) URL. It is the #34 "log redaction" guarantee: indexer
// adapters pass their API key as a query parameter (Torznab/NZB use ?apikey=,
// TorrentRSS uses ?key=), and Go's net/http transport errors echo the full
// request URL back in their Error() text. If that text ever reaches a log line
// or a persisted LastError, the key would leak. redactURL masks the value of
// every sensitive parameter while leaving the host/path (and non-secret params)
// intact, so the result is still useful for debugging.
//
// It scans the whole input string (not just a bare URL) because transport
// errors wrap the URL inside a larger message, e.g.
//
//	Get "https://idx.example/api?apikey=SECRET": dial tcp: i/o timeout
//
// and returns
//
//	Get "https://idx.example/api?apikey=***": dial tcp: i/o timeout
func redactURL(s string) string {
	if s == "" {
		return s
	}
	return urlRe.ReplaceAllStringFunc(s, func(m string) string {
		u, err := url.Parse(m)
		if err != nil {
			return m
		}
		redacted := redactRawQuery(u.RawQuery)
		if redacted == u.RawQuery {
			return m // nothing sensitive; return the input untouched
		}
		u.RawQuery = redacted
		// url.String() writes RawQuery verbatim, so our literal "***" survives
		// and the original parameter order/encoding is preserved.
		return u.String()
	})
}

// redactRawQuery masks the value of every sensitive query parameter in a raw
// (already-encoded) query string, in place and in order. Non-sensitive pairs
// are left byte-for-byte so we never re-encode values that the caller already
// encoded correctly. A sensitive param with no value gains "=***".
func redactRawQuery(raw string) string {
	if raw == "" {
		return raw
	}
	parts := strings.Split(raw, "&")
	for i, part := range parts {
		eq := strings.IndexByte(part, '=')
		var key string
		if eq < 0 {
			key = part
		} else {
			key = part[:eq]
		}
		// Decode the key for the case-insensitive sensitivity check; keep the
		// original encoded form in the output.
		decodedKey, _ := url.QueryUnescape(key)
		if !sensitiveQueryParam(decodedKey) {
			continue
		}
		// Mask the value (or add one). The key is kept verbatim.
		parts[i] = key + "=***"
	}
	return strings.Join(parts, "&")
}

// urlRe matches an http(s) URL up to the first whitespace or quote. It is
// conservative on purpose: it only rewrites things that parse as a URL.
var urlRe = regexp.MustCompile(`https?://[^\s"'` + "`" + `)]+`)

// sensitiveQueryParams are the lowercased query-parameter names that carry
// indexer credentials. The set is a superset of what the bundled adapters use
// (apikey / key) plus the common variants, so a future adapter that uses a
// different param name is still protected.
var sensitiveQueryParams = map[string]struct{}{
	"apikey":   {},
	"api_key":  {},
	"key":      {},
	"token":    {},
	"pass":     {},
	"password": {},
	"secret":   {},
	"auth":     {},
	"api":      {},
}

// sensitiveQueryParam reports whether a (raw) query-param name holds a secret.
func sensitiveQueryParam(name string) bool {
	_, ok := sensitiveQueryParams[strings.ToLower(name)]
	return ok
}

// Redact is the public-facing helper a log line or error can pass a message
// through. It is identical to redactURL and is exported so the API/health
// layers can redact user-supplied strings that may echo a URL.
func Redact(s string) string { return redactURL(s) }
