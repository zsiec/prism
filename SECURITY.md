# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Prism, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please use [GitHub Security Advisories](https://github.com/zsiec/prism/security/advisories/new) to report the vulnerability privately. You will receive a response acknowledging your report within 72 hours.

Please include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Known Security Considerations

Prism is designed for development and local-network use. The following are intentional design choices that require attention in production deployments:

### CORS Policy

All HTTP responses include `Access-Control-Allow-Origin: *`. This allows any origin to access the API and viewer endpoints. Production deployments should enforce origin restrictions at a reverse proxy layer.

### WebTransport Origin Check

The WebTransport server accepts connections from all origins (`CheckOrigin` returns `true` unconditionally). Production deployments behind a reverse proxy should enforce origin checks at the proxy layer.

### SRT Pull Endpoint (SSRF Surface)

The `POST /api/srt-pull` endpoint accepts arbitrary remote addresses and initiates outbound SRT connections to them. This could be used for Server-Side Request Forgery (SSRF) if exposed to untrusted clients. In production, restrict this endpoint to authenticated operators or internal networks.

### Self-Signed Certificates

The server generates a self-signed ECDSA P-256 certificate at startup with a 14-day validity period (the maximum allowed by WebTransport). The certificate fingerprint is available via `/api/cert-hash` for browser certificate pinning. Production deployments should use certificates issued by a trusted CA.

## Supported Versions

Security fixes are applied to the latest release only.
