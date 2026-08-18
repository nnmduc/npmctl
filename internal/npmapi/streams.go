package npmapi

import "context"

// Stream field sets differ between create and update, and the difference is real:
// POST /nginx/streams accepts domain_names, PUT /nginx/streams/{id} does NOT.
// Both bodies are additionalProperties:false, so sending domain_names on an update
// is a 400 — which is why these are two lists rather than one shared one.
var (
	// StreamCreateFields is the POST property set. incoming_port, forwarding_host
	// and forwarding_port are required.
	StreamCreateFields = []string{
		"incoming_port", "forwarding_host", "forwarding_port",
		"tcp_forwarding", "udp_forwarding", "certificate_id", "domain_names", "meta",
	}
	// StreamUpdateFields is the PUT property set — note the absence of domain_names.
	StreamUpdateFields = []string{
		"incoming_port", "forwarding_host", "forwarding_port",
		"tcp_forwarding", "udp_forwarding", "certificate_id", "meta",
	}
)

// NewStreamCreatePayload returns a builder for POST /nginx/streams.
func NewStreamCreatePayload() *Payload { return NewPayload("stream (create)", StreamCreateFields) }

// NewStreamUpdatePayload returns a builder for PUT /nginx/streams/{id}.
func NewStreamUpdatePayload() *Payload { return NewPayload("stream (update)", StreamUpdateFields) }

// Stream is a TCP/UDP port forward.
type Stream struct {
	Timestamps
	ID             int      `json:"id"`
	OwnerUserID    int      `json:"owner_user_id,omitempty"`
	IncomingPort   int      `json:"incoming_port"`
	ForwardingHost string   `json:"forwarding_host"`
	ForwardingPort int      `json:"forwarding_port"`
	TCPForwarding  bool     `json:"tcp_forwarding"`
	UDPForwarding  bool     `json:"udp_forwarding"`
	CertificateID  any      `json:"certificate_id,omitempty"`
	DomainNames    []string `json:"domain_names,omitempty"`
	Enabled        bool     `json:"enabled"`
	Meta           Meta     `json:"meta,omitempty"`
	Owner          *Owner   `json:"owner,omitempty"`
}

func (s *Stream) GetID() int            { return s.ID }
func (s *Stream) GetDomains() []string  { return s.DomainNames }
func (s *Stream) GetMeta() Meta         { return s.Meta }
func (s *Stream) GetModifiedOn() string { return s.ModifiedOn }

func (c *Client) streams() resource[Stream] {
	return resource[Stream]{c: c, path: "/nginx/streams"}
}

// ListStreams returns every stream.
func (c *Client) ListStreams(ctx context.Context, expand ...string) ([]Stream, error) {
	return c.streams().list(ctx, expand...)
}

// GetStream returns one stream.
func (c *Client) GetStream(ctx context.Context, id int, expand ...string) (*Stream, error) {
	return c.streams().get(ctx, id, expand...)
}

// CreateStream creates a stream.
func (c *Client) CreateStream(ctx context.Context, body map[string]any) (*Stream, error) {
	return c.streams().create(ctx, body)
}

// UpdateStream applies a partial update.
func (c *Client) UpdateStream(ctx context.Context, id int, body map[string]any) (*Stream, error) {
	return c.streams().update(ctx, id, body)
}

// DeleteStream removes a stream.
func (c *Client) DeleteStream(ctx context.Context, id int) error {
	return c.streams().remove(ctx, id)
}

// SetStreamEnabled enables or disables a stream.
func (c *Client) SetStreamEnabled(ctx context.Context, id int, enabled bool) error {
	return c.streams().setEnabled(ctx, id, enabled)
}

// FindStreamByPort resolves an incoming port to a stream. Streams are addressed by
// port rather than by domain, since a stream need not carry any domain at all.
func (c *Client) FindStreamByPort(ctx context.Context, port int) (*Stream, error) {
	items, err := c.ListStreams(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].IncomingPort == port {
			return &items[i], nil
		}
	}
	return nil, &APIError{
		Status: 404, Code: 404,
		Message: sprintf("no stream listens on incoming port %d", port),
		Method:  "GET", Path: "/nginx/streams",
	}
}
