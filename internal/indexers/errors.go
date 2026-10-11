package indexers

import (
	"errors"
	"fmt"
	"net/http"
)

// Typed error model for indexer adapters. A raw HTTP status is turned into one
// of these so (a) the health layer can bucket failures by cause (#31 "error
// mapping: 4xx/5xx → typed errors + health stats"), (b) callers can react
// differently (e.g. an ErrUnauthorized means the API key is wrong, so retrying
// is pointless), and (c) the message is safe to log because it never carries
// credentials.
//
// Every error here is a *sentinel* (ErrXXX) that wraps an optional detail. Use
// errors.Is to match the class and .Error() for a human string.
type Error struct {
	// Op names the failing operation (e.g. "torznab search", "torrent-rss poll").
	Op string
	// Err is the sentinel this instance represents (one of the ErrXXX vars).
	Err error
	// Detail is a short, credential-free explanation (e.g. "status 401").
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s: %s", e.Op, e.Err.Error(), e.Detail)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Err.Error())
}

func (e *Error) Unwrap() error { return e.Err }

// Sentinels.
var (
	// ErrUnauthorized means the indexer rejected the credentials (HTTP 401/403).
	ErrUnauthorized = errors.New("indexer: unauthorized")
	// ErrNotFound means the requested resource does not exist (HTTP 404).
	ErrNotFound = errors.New("indexer: not found")
	// ErrRateLimited means the indexer asked us to slow down (HTTP 429).
	ErrRateLimited = errors.New("indexer: rate limited")
	// ErrBadRequest means our request was malformed or unsupported (HTTP 4xx
	// other than 401/403/404/429).
	ErrBadRequest = errors.New("indexer: bad request")
	// ErrServerError means the indexer failed on its side (HTTP 5xx).
	ErrServerError = errors.New("indexer: server error")
	// ErrBadResponse means the response was well-formed HTTP but not the format
	// we expected (e.g. a 200 with a non-XML body, or a corrupt feed).
	ErrBadResponse = errors.New("indexer: bad response")
	// ErrTimeout means the request exceeded its deadline.
	ErrTimeout = errors.New("indexer: timeout")
	// ErrTransport means the request never reached a usable HTTP response: a
	// connection error, a DNS failure, a refused connection, or a mid-flight
	// timeout. It is distinct from ErrBadRequest/ErrServerError (which require
	// an actual HTTP response) so the health layer can bucket "the indexer is
	// unreachable" separately from "the indexer answered with an error". The
	// wrapped detail is always redacted (#34) because net/http transport
	// errors embed the request URL, which carries the API key.
	ErrTransport = errors.New("indexer: transport error")
)

// ClassifyError maps an HTTP status code to the matching sentinel, for use by
// the adapters when a request returns a non-2xx status. 2xx/3xx map to nil
// (not an error); anything else is classified.
func ClassifyError(status int) error {
	switch {
	case status >= 200 && status < 400:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrUnauthorized
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status >= 400 && status < 500:
		return ErrBadRequest
	default:
		return ErrServerError
	}
}

// newError builds an *Error for the given operation. detail may be empty.
func newError(op string, cause error, detail string) *Error {
	return &Error{Op: op, Err: cause, Detail: detail}
}
