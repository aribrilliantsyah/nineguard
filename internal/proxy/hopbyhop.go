package proxy

import (
	"net/http"
	"strings"
)

// hopByHopHeaders lists headers that are defined as hop-by-hop per RFC 7230 section 6.1
// and common proxy conventions (such as Proxy-Connection).
var hopByHopHeaders = []string{
	"connection",
	"proxy-connection",
	"keep-alive",
	"proxy-authenticate",
	"proxy-authorization",
	"te",
	"trailer",
	"transfer-encoding",
	"upgrade",
}

// RemoveHopByHopHeaders removes hop-by-hop HTTP headers per RFC 7230 section 6.1
// from the given http.Header in place. It removes the static hop-by-hop headers
// (Connection, Proxy-Connection, Keep-Alive, Proxy-Authenticate, Proxy-Authorization,
// Te, Trailer, Transfer-Encoding, Upgrade) as well as any header listed in any
// Connection header value (comma-separated, case-insensitive).
func RemoveHopByHopHeaders(h http.Header) {
	if h == nil {
		return
	}

	toRemove := make(map[string]struct{}, len(hopByHopHeaders)+4)
	for _, header := range hopByHopHeaders {
		toRemove[header] = struct{}{}
	}

	// RFC 7230 section 6.1: any header name listed as a connection-option
	// in the Connection header field must also be treated as hop-by-hop.
	for k, values := range h {
		if strings.EqualFold(k, "connection") {
			for _, v := range values {
				for _, token := range strings.Split(v, ",") {
					token = strings.Trim(strings.TrimSpace(token), "\"")
					if token != "" {
						toRemove[strings.ToLower(token)] = struct{}{}
					}
				}
			}
		}
	}

	for k := range h {
		if _, found := toRemove[strings.ToLower(k)]; found {
			delete(h, k)
		}
	}
}
