/**
 * Checks for required browser APIs (WebCodecs, WebTransport, SharedArrayBuffer)
 * and returns a list of missing capabilities. Returns an empty array if all
 * required APIs are available.
 */
export function checkCapabilities() {
    const missing = [];
    if (typeof VideoDecoder === "undefined") {
        missing.push("WebCodecs (VideoDecoder)");
    }
    if (typeof AudioDecoder === "undefined") {
        missing.push("WebCodecs (AudioDecoder)");
    }
    if (typeof WebTransport === "undefined") {
        missing.push("WebTransport");
    }
    if (typeof SharedArrayBuffer === "undefined") {
        missing.push("SharedArrayBuffer");
    }
    return missing;
}
/**
 * Creates and shows an error overlay listing the missing browser APIs.
 * Returns the overlay element for later removal.
 */
export function showCapabilityError(container, missing) {
    const overlay = document.createElement("div");
    overlay.style.cssText =
        "position:absolute;inset:0;display:flex;align-items:center;justify-content:center;" +
            "background:rgba(0,0,0,0.9);color:#fff;font-family:system-ui,sans-serif;z-index:1000;" +
            "flex-direction:column;padding:2rem;text-align:center;";
    const title = document.createElement("h2");
    title.textContent = "Browser Not Supported";
    title.style.margin = "0 0 1rem 0";
    const desc = document.createElement("p");
    desc.textContent = "Prism requires the following APIs that are not available in this browser:";
    desc.style.margin = "0 0 1rem 0";
    desc.style.opacity = "0.8";
    const list = document.createElement("ul");
    list.style.cssText = "list-style:none;padding:0;margin:0 0 1.5rem 0;";
    for (const api of missing) {
        const li = document.createElement("li");
        li.textContent = api;
        li.style.cssText = "padding:0.3rem 0;font-family:monospace;color:#ff6b6b;";
        list.appendChild(li);
    }
    const hint = document.createElement("p");
    hint.style.cssText = "opacity:0.6;font-size:0.9rem;";
    hint.textContent = "Try Chrome 113+, Edge 113+, or Opera 99+ with cross-origin isolation enabled.";
    overlay.appendChild(title);
    overlay.appendChild(desc);
    overlay.appendChild(list);
    overlay.appendChild(hint);
    container.appendChild(overlay);
    return overlay;
}
