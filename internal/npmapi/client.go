package npmapi

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

func asErr(err error, target any) bool { return errors.As(err, target) }

// DefaultTimeout applies to ordinary operations. Certificate issuance overrides
// it per call: an ACME order legitimately outlives any sane default.

// DefaultTimeout applies to ordinary operations. Certificate issuance overrides
// it per call: an ACME order legitimately outlives any sane default.
const DefaultTimeout = 30 * time.Second

// CertificateTimeout bounds an operation that triggers ACME issuance, including
// a proxy-host write carrying certificate_id:"new" — NPM runs the whole order
// before answering, so the default would abort a request that is still working.

// CertificateTimeout bounds an operation that triggers ACME issuance, including
// a proxy-host write carrying certificate_id:"new" — NPM runs the whole order
// before answering, so the default would abort a request that is still working.
const CertificateTimeout = 180 * time.Second

// Options configure a Client.

// Options configure a Client.
type Options struct {
	BaseURL   string
	Token     string
	Insecure  bool   // skip TLS verification entirely — last resort
	CACert    string // path to a PEM bundle: solves self-signed WITHOUT Insecure
	PinSHA256 string // base64 or hex SHA-256 of the server cert's public key
	Timeout   time.Duration
	Verbose   bool
	VerboseTo io.Writer
}

// Client talks to one NPM instance.

// Client talks to one NPM instance.
type Client struct {
	base    *url.URL
	token   string
	http    *http.Client
	verbose bool
	vw      io.Writer
}

// New builds a Client. TLS trust is deliberately layered so the common homelab
// case (a self-signed NPM cert) is solvable with --ca-cert or --pin-sha256
// instead of switching verification off wholesale.

// New builds a Client. TLS trust is deliberately layered so the common homelab
// case (a self-signed NPM cert) is solvable with --ca-cert or --pin-sha256
// instead of switching verification off wholesale.
func New(o Options) (*Client, error) {
	if strings.TrimSpace(o.BaseURL) == "" {
		return nil, exitcode.New(exitcode.Usage, "no NPM url configured: run `npmctl auth login --url <url>` or set a profile")
	}
	u, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "invalid url %q", o.BaseURL)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, exitcode.New(exitcode.Usage, "url %q must include a scheme and host, e.g. https://npm.example.com", o.BaseURL)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.Insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	if o.CACert != "" {
		pem, err := os.ReadFile(expandHome(o.CACert))
		if err != nil {
			return nil, exitcode.Wrap(exitcode.Usage, err, "read --ca-cert")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, exitcode.New(exitcode.Usage, "no certificates found in %s", o.CACert)
		}
		tlsCfg.RootCAs = pool
	}
	if o.PinSHA256 != "" {
		want, err := normalizePin(o.PinSHA256)
		if err != nil {
			return nil, err
		}
		// Pinning verifies the leaf ourselves, so chain verification is bypassed
		// in favour of an exact key match — stricter than the default, not looser.
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = pinVerifier(want)
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	vw := o.VerboseTo
	if vw == nil {
		vw = os.Stderr
	}
	return &Client{
		base:  u,
		token: o.Token,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &retryTransport{next: &http.Transport{TLSClientConfig: tlsCfg}},
		},
		verbose: o.Verbose,
		vw:      vw,
	}, nil
}

// BaseURL returns the configured instance URL.

// BaseURL returns the configured instance URL.
func (c *Client) BaseURL() string { return c.base.String() }

// WithTimeout returns a shallow copy using a different timeout, for operations
// like ACME issuance that need far longer than the default.

// WithTimeout returns a shallow copy using a different timeout, for operations
// like ACME issuance that need far longer than the default.
func (c *Client) WithTimeout(d time.Duration) *Client {
	cp := *c
	hc := *c.http
	hc.Timeout = d
	cp.http = &hc
	return &cp
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func normalizePin(s string) (string, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "sha256//"))
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == sha256.Size {
		return base64.StdEncoding.EncodeToString(b), nil
	}
	var raw []byte
	if _, err := fmt.Sscanf(strings.ReplaceAll(strings.ToLower(s), ":", ""), "%x", &raw); err == nil && len(raw) == sha256.Size {
		return base64.StdEncoding.EncodeToString(raw), nil
	}
	return "", exitcode.New(exitcode.Usage, "--pin-sha256 must be a base64 or hex SHA-256 digest")
}

func pinVerifier(want string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		for _, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				continue
			}
			sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			if base64.StdEncoding.EncodeToString(sum[:]) == want {
				return nil
			}
		}
		return errors.New("server certificate does not match --pin-sha256")
	}
}

// request is one API call.
