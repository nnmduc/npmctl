# Configuration and Profiles

## Profiles

npmctl supports multiple environments using profiles stored in `~/.config/npmctl/config.yaml`.

### Example Configuration

```yaml
default_profile: prod
profiles:
  prod:
    url: https://npm.example.com
    identity: me@example.com
  lab:
    url: https://127.0.0.1:18181
    identity: lab-admin@npmctl.test
    ca_cert: ~/.config/npmctl/lab-ca.pem
```

### Credential Scoping

Stored tokens are keyed by the combination of `(profile, url, identity)`. Changing a profile's URL will invalidate the cached token rather than sending it to the new endpoint.

## TLS and HTTPS Settings

You can connect to instances with self-signed or internal certificates without turning off TLS verification completely:

### Custom CA Certificate

```bash
npmctl --ca-cert ~/.config/npmctl/npm-ca.pem host list
```

### Public Key Pinning

```bash
npmctl --pin-sha256 <base64-sha256-of-public-key> host list
```

### Insecure Connections

The `--insecure` flag skips certificate verification for environments where standard verification is not possible. For security, `--insecure` is disabled during `auth login` to prevent sending credentials over unverified connections.
