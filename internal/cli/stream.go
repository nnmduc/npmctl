package cli

import (
	"context"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// streamFlags mirrors the stream write schema.
type streamFlags struct {
	incomingPort   int
	forwardingHost string
	forwardingPort int
	tcp            bool
	udp            bool
	certificateID  string
	domains        []string
}

func (s *streamFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.IntVar(&s.incomingPort, "incoming-port", 0, "port NPM listens on")
	fl.StringVar(&s.forwardingHost, "forwarding-host", "", "upstream host or IP")
	fl.IntVar(&s.forwardingPort, "forwarding-port", 0, "upstream port")
	fl.BoolVar(&s.tcp, "tcp", true, "forward TCP")
	fl.BoolVar(&s.udp, "udp", false, "forward UDP")
	fl.StringVar(&s.certificateID, "certificate-id", "", `certificate ID, or "new" to order one inline`)
	fl.StringSliceVar(&s.domains, "domain", nil, "domain for TLS termination (create only; PUT rejects this field)")
}

// base fills the fields both POST and PUT accept.
func (s *streamFlags) base(cmd *cobra.Command, p *npmapi.Payload) (*npmapi.Payload, error) {
	fl := cmd.Flags()
	p.SetIf(fl.Changed("incoming-port"), "incoming_port", s.incomingPort)
	p.SetIf(fl.Changed("forwarding-host"), "forwarding_host", s.forwardingHost)
	p.SetIf(fl.Changed("forwarding-port"), "forwarding_port", s.forwardingPort)
	p.SetIf(fl.Changed("tcp"), "tcp_forwarding", s.tcp)
	p.SetIf(fl.Changed("udp"), "udp_forwarding", s.udp)
	if fl.Changed("certificate-id") {
		v, err := certificateValue(s.certificateID)
		if err != nil {
			return nil, err
		}
		p.Set("certificate_id", v)
	}
	return p, nil
}

// updatePayload uses the PUT field set, which does NOT include domain_names.
// Passing --domain to an update is refused here rather than 400'd by the API.
func (s *streamFlags) updatePayload(cmd *cobra.Command) (*npmapi.Payload, error) {
	if cmd.Flags().Changed("domain") {
		return nil, streamDomainsOnUpdateError()
	}
	return s.base(cmd, npmapi.NewStreamUpdatePayload())
}

func (s *streamFlags) createPayload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p, err := s.base(cmd, npmapi.NewStreamCreatePayload())
	if err != nil {
		return nil, err
	}
	if err := requireFlags(map[string]bool{
		"--incoming-port":   s.incomingPort != 0,
		"--forwarding-host": s.forwardingHost != "",
		"--forwarding-port": s.forwardingPort != 0,
	}); err != nil {
		return nil, err
	}
	p.Set("incoming_port", s.incomingPort)
	p.Set("forwarding_host", s.forwardingHost)
	p.Set("forwarding_port", s.forwardingPort)
	if len(s.domains) > 0 {
		p.Set("domain_names", s.domains)
	}
	if !s.tcp && !s.udp {
		return nil, requireFlags(map[string]bool{"--tcp or --udp": false})
	}
	// Always send both protocol flags on create. Omitting them lets NPM default them
	// to false, which produces a stream that listens but forwards nothing — the flag
	// defaults documented by --help would silently not apply.
	p.Set("tcp_forwarding", s.tcp)
	p.Set("udp_forwarding", s.udp)
	return p, nil
}

func newStreamCommand(f *globalFlags) *cobra.Command {
	sf := &streamFlags{}
	return newCRUDCommand(f, crudSpec[npmapi.Stream]{
		use:   "stream",
		short: "Manage TCP/UDP streams",
		long: "Streams forward a raw TCP or UDP port to an upstream host.\n\n" +
			"Accepts a numeric ID or an incoming port number — streams need not carry a\n" +
			"domain, so a port is the natural handle. Writes require NPMCTL_ALLOW_WRITE=1\n" +
			"and --yes.\n\n" +
			"Note: --domain applies to `create` only. The update endpoint rejects that\n" +
			"field, so npmctl refuses it rather than sending a request the API will 400.",
		kind:    "stream",
		path:    "/nginx/streams",
		refHelp: "<id|incoming-port>",
		columns: []output.Column{
			{Header: "ID", Key: "id"},
			{Header: "IN", Key: "incoming_port"},
			{Header: "UPSTREAM", Key: "forwarding_host"},
			{Header: "PORT", Key: "forwarding_port"},
			{Header: "TCP", Key: "tcp_forwarding"},
			{Header: "UDP", Key: "udp_forwarding"},
			{Header: "ENABLED", Key: "enabled"},
			{Header: "ONLINE", Key: "meta.nginx_online"},
		},
		registerFlags: sf.register,
		createPayload: sf.createPayload,
		updatePayload: sf.updatePayload,
		resolve: func(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.Stream, error) {
			n, err := strconv.Atoi(ref)
			if err != nil {
				return nil, streamRefError(ref)
			}
			// Try the ID first, then fall back to matching an incoming port.
			if s, err := c.GetStream(ctx, n); err == nil {
				return s, nil
			}
			return c.FindStreamByPort(ctx, n)
		},
		list: func(ctx context.Context, c *npmapi.Client) ([]npmapi.Stream, error) {
			return c.ListStreams(ctx)
		},
		get: func(ctx context.Context, c *npmapi.Client, id int) (*npmapi.Stream, error) {
			return c.GetStream(ctx, id)
		},
		create: func(ctx context.Context, c *npmapi.Client, b map[string]any) (*npmapi.Stream, error) {
			return c.CreateStream(ctx, b)
		},
		update: func(ctx context.Context, c *npmapi.Client, id int, b map[string]any) (*npmapi.Stream, error) {
			return c.UpdateStream(ctx, id, b)
		},
		remove: func(ctx context.Context, c *npmapi.Client, id int) error {
			return c.DeleteStream(ctx, id)
		},
		setEnabled: func(ctx context.Context, c *npmapi.Client, id int, on bool) error {
			return c.SetStreamEnabled(ctx, id, on)
		},
		idOf:       func(s *npmapi.Stream) int { return s.ID },
		nameOf:     func(s *npmapi.Stream) string { return portLabel(s.IncomingPort) },
		modifiedOf: func(s *npmapi.Stream) string { return s.ModifiedOn },
		metaOf:     func(s *npmapi.Stream) npmapi.Meta { return s.Meta },
	})
}
