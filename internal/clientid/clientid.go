// Package clientid names this CLI to the OneCLI API: every API call carries
// OneCLI-Client: onecli-cli/<version>, so the server can tell which client
// and version made it.
package clientid

import (
	"net/http"
	"regexp"
	"sync/atomic"
)

// Header is the request header that names the client.
const Header = "OneCLI-Client"

// Name is this client's name in the header.
const Name = "onecli-cli"

var (
	version atomic.Value // string
	safeRE  = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

func init() { version.Store("dev") }

// SetVersion records the CLI's version for the header. A value that is not
// a plain version string (letters, digits and . _ + -, at most 64) is
// replaced by "unknown", so the header never carries odd bytes.
func SetVersion(v string) {
	if !safeRE.MatchString(v) {
		v = "unknown"
	}
	version.Store(v)
}

// Value is the header's value: onecli-cli/<version>.
func Value() string { return Name + "/" + version.Load().(string) }

// Transport sets the header on every request it sends, then hands the
// request to Base (http.DefaultTransport when nil). The caller's request is
// never changed: it is cloned first, as a RoundTripper must.
type Transport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	r := req.Clone(req.Context())
	r.Header.Set(Header, Value())
	return base.RoundTrip(r)
}

// Wrap returns base wrapped so every request names this client.
func Wrap(base http.RoundTripper) http.RoundTripper {
	return &Transport{Base: base}
}
