package npmapi

import (
	"context"
	"sort"
	"strings"
	"time"
)

// IssuanceState is the outcome of waiting for a certificate order.
//
// There are exactly three, and "may have succeeded" is not one of them. An
// ambiguous answer is what drives a human or an agent to retry, and a retry spends
// another of Let's Encrypt's five duplicate certificates per week.
type IssuanceState string

const (
	// StateIssued means a row exists AND carries expires_on: certbot finished.
	StateIssued IssuanceState = "ISSUED"
	// StateNotPresent means no row exists. NPM deletes the row when certbot fails,
	// so this is a definite failure, not an unknown.
	StateNotPresent IssuanceState = "NOT PRESENT"
	// StateIndeterminate means a row exists without expires_on when the deadline
	// passed: the order may still be running server-side. Do NOT retry.
	StateIndeterminate IssuanceState = "INDETERMINATE"
)

// IssuanceResult reports the outcome of a poll.
type IssuanceResult struct {
	State IssuanceState `json:"state"`
	// Detail explains the state in one sentence, including when a retry becomes
	// safe for the indeterminate case.
	Detail      string       `json:"detail"`
	Certificate *Certificate `json:"certificate,omitempty"`
}

// pollInterval is how often the certificate list is re-read while waiting. It is a
// variable so tests can shorten it; nothing in production changes it.
var pollInterval = 5 * time.Second

// matchesDomains reports whether a certificate covers exactly this domain set.
func matchesDomains(cert *Certificate, want []string) bool {
	if len(cert.DomainNames) != len(want) {
		return false
	}
	a := normalizedDomains(cert.DomainNames)
	b := normalizedDomains(want)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizedDomains(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		out = append(out, strings.ToLower(strings.TrimSpace(d)))
	}
	sort.Strings(out)
	return out
}

// PollIssuance waits for a certificate covering domains to appear with an expiry.
//
// progress, when non-nil, receives human status lines. Callers must send those to
// stderr so stdout stays valid JSON.
func (c *Client) PollIssuance(ctx context.Context, domains []string, deadline time.Time, progress func(string)) (*IssuanceResult, error) {
	note := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}
	sawRow := false

	for {
		certs, err := c.ListCertificates(ctx)
		if err != nil {
			return nil, err
		}
		var found *Certificate
		for i := range certs {
			if matchesDomains(&certs[i], domains) {
				found = &certs[i]
				break
			}
		}
		switch {
		case found != nil && found.ExpiresOn != "":
			return &IssuanceResult{
				State:       StateIssued,
				Detail:      "certificate is present and carries an expiry date",
				Certificate: found,
			}, nil
		case found != nil:
			// The row exists but certbot has not finished. This is precisely the
			// window in which `cert list` alone is misleading.
			sawRow = true
			note("certificate row exists; waiting for issuance to complete")
		case sawRow:
			// The row was there and is now gone: NPM deletes it when certbot fails.
			return &IssuanceResult{
				State:  StateNotPresent,
				Detail: "the certificate row was created and then removed, which means issuance failed server-side",
			}, nil
		default:
			note("waiting for the certificate row to appear")
		}

		if !time.Now().Before(deadline) {
			if sawRow || found != nil {
				retryAfter := time.Now().Add(Window7d).UTC().Format(time.RFC3339)
				return &IssuanceResult{
					State: StateIndeterminate,
					Detail: "a certificate row exists without an expiry and the deadline passed; " +
						"the order may still be running. Do not retry before " + retryAfter +
						" — check `npmctl cert list` instead",
					Certificate: found,
				}, nil
			}
			return &IssuanceResult{
				State:  StateNotPresent,
				Detail: "no certificate row appeared before the deadline",
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Window7d mirrors the ACME duplicate-certificate window, used to tell the operator
// when a retry stops being risky.
const Window7d = 7 * 24 * time.Hour

// DNSChallengeTimeout returns the deadline budget for a DNS-01 order.
//
// DNS-01 must wait for propagation before validation even begins, so the budget is
// the configured propagation delay plus a fixed allowance for the order itself.
// A flat 180s — fine for HTTP-01 — aborts a DNS-01 order that is working normally.
func DNSChallengeTimeout(propagationSeconds int) time.Duration {
	return time.Duration(propagationSeconds)*time.Second + 240*time.Second
}
