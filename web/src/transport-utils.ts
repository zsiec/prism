/** Server connection info returned by fetchServerInfo. */
export interface ServerInfo {
	certHash: Uint8Array;
	/** WebTransport host:port (e.g. ":4443"). */
	addr: string;
}

/**
 * Fetches the server's self-signed certificate hash and WebTransport
 * address from the /api/cert-hash endpoint.
 */
export async function fetchServerInfo(): Promise<ServerInfo> {
	const resp = await fetch("/api/cert-hash");
	const data = await resp.json();
	const hashBase64: string = data.hash;
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
export function wtBaseURL(info: ServerInfo): string {
	// addr is typically ":4443" or "0.0.0.0:4443"
	const parts = info.addr.split(":");
	const port = parts[parts.length - 1] || "4443";
	return `https://${window.location.hostname}:${port}`;
}

/**
 * Creates and connects a WebTransport session pinned to the server's
 * self-signed certificate. Returns the connected transport.
 */
export async function connectWebTransport(url: string, certHash: Uint8Array): Promise<WebTransport> {
	const transport = new WebTransport(url, {
		serverCertificateHashes: [
			{
				algorithm: "sha-256",
				value: certHash.buffer as ArrayBuffer,
			},
		],
	});
	await transport.ready;
	return transport;
}
