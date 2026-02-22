/**
 * Fetches the server's self-signed certificate hash and WebTransport
 * address from the /api/cert-hash endpoint.
 */
export async function fetchServerInfo() {
    const resp = await fetch("/api/cert-hash");
    const data = await resp.json();
    const hashBase64 = data.hash;
    const hashBinary = atob(hashBase64);
    const hashBuffer = new Uint8Array(hashBinary.length);
    for (let i = 0; i < hashBinary.length; i++) {
        hashBuffer[i] = hashBinary.charCodeAt(i);
    }
    return {
        certHash: hashBuffer,
        addr: data.addr ?? ":4443",
    };
}
/**
 * Derives the WebTransport base URL from the server info, using the
 * current page hostname and the server's configured port.
 */
export function wtBaseURL(info) {
    // addr is typically ":4443" or "0.0.0.0:4443"
    const parts = info.addr.split(":");
    const port = parts[parts.length - 1] || "4443";
    return `https://${window.location.hostname}:${port}`;
}
/**
 * Creates and connects a WebTransport session pinned to the server's
 * self-signed certificate. Returns the connected transport.
 */
export async function connectWebTransport(url, certHash) {
    const transport = new WebTransport(url, {
        serverCertificateHashes: [
            {
                algorithm: "sha-256",
                value: certHash.buffer,
            },
        ],
    });
    await transport.ready;
    return transport;
}
