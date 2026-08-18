package undo

// init registers the kinds npmctl can restore.
//
// Writable lists mirror each resource's write schema exactly. ReadOnly lists the
// response-only fields a GET returns, so they are dropped rather than mistaken
// for schema drift.
func init() {
	Register(&Spec{
		Kind: "proxy-host",
		Writable: []string{
			"domain_names", "forward_scheme", "forward_host", "forward_port",
			"certificate_id", "ssl_forced", "hsts_enabled", "hsts_subdomains", "http2_support",
			"block_exploits", "caching_enabled", "allow_websocket_upgrade", "trust_forwarded_proto",
			"access_list_id", "advanced_config", "enabled", "locations", "meta",
		},
		ReadOnly: []string{
			"id", "created_on", "modified_on", "owner_user_id", "owner",
			"certificate", "access_list", "use_default_location", "ipv6",
		},
	})
}
