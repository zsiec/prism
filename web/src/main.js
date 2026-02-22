import { PrismPlayer } from "./player";
import { MultiviewManager } from "./multiview";
const streamKeyInput = document.getElementById("streamKey");
const connectBtn = document.getElementById("connectBtn");
const statusEl = document.getElementById("status");
const streamListEl = document.getElementById("streamList");
const singleModeEl = document.getElementById("singleMode");
const multiModeEl = document.getElementById("multiMode");
const playerContainer = document.getElementById("player");
const tabs = document.querySelectorAll(".tab");
const controlsEl = document.querySelector(".controls");
const headerEl = document.querySelector("header");
const srtPullBtn = document.getElementById("srtPullBtn");
const srtPullModal = document.getElementById("srtPullModal");
const srtAddress = document.getElementById("srtAddress");
const srtStreamKey = document.getElementById("srtStreamKey");
const srtStreamId = document.getElementById("srtStreamId");
const srtPullError = document.getElementById("srtPullError");
const srtPullCancel = document.getElementById("srtPullCancel");
const srtPullConnect = document.getElementById("srtPullConnect");
let currentMode = "single";
let singlePlayer = null;
let multiview = null;
let cachedStreams = [];
const params = new URLSearchParams(window.location.search);
const urlStreamKey = params.get("stream");
const urlMode = params.get("mode");
if (urlStreamKey) {
    streamKeyInput.value = urlStreamKey;
}
function initSingleMode() {
    if (singlePlayer)
        return;
    const maxDim = Math.max(window.innerWidth, window.innerHeight);
    const cap = Math.min(maxDim, 1920);
    singlePlayer = new PrismPlayer(playerContainer, {
        onStreamConnected: (key) => {
            statusEl.textContent = `Connected to "${key}"`;
            connectBtn.disabled = false;
            connectBtn.textContent = "Disconnect";
        },
        onStreamDisconnected: (key) => {
            statusEl.textContent = `Disconnected from "${key}". Reconnecting...`;
            connectBtn.textContent = "Watch";
        },
    });
    singlePlayer.setMaxResolution(cap);
}
function initMultiview() {
    if (multiview)
        return;
    multiview = new MultiviewManager(multiModeEl);
    if (cachedStreams.length > 0) {
        multiview.connectAll(cachedStreams);
        statusEl.textContent = `Multiview: ${Math.min(cachedStreams.length, 9)} streams connected`;
    }
    else {
        statusEl.textContent = "Multiview: waiting for streams...";
        fetchStreams().then(() => {
            if (multiview && cachedStreams.length > 0) {
                multiview.connectAll(cachedStreams);
                statusEl.textContent = `Multiview: ${Math.min(cachedStreams.length, 9)} streams connected`;
            }
        });
    }
}
function switchMode(mode) {
    if (mode === currentMode)
        return;
    currentMode = mode;
    tabs.forEach(tab => {
        tab.classList.toggle("active", tab.dataset.mode === mode);
    });
    if (mode === "single") {
        singleModeEl.style.display = "flex";
        multiModeEl.style.display = "none";
        streamListEl.style.display = "flex";
        statusEl.style.display = "block";
        controlsEl.style.display = "flex";
        headerEl.style.height = "40px";
        if (multiview) {
            multiview.destroy();
            multiview = null;
        }
        initSingleMode();
        statusEl.textContent = "Enter a stream key and click Watch to connect.";
        connectBtn.textContent = "Watch";
    }
    else {
        singleModeEl.style.display = "none";
        multiModeEl.style.display = "block";
        streamListEl.style.display = "none";
        statusEl.style.display = "none";
        controlsEl.style.display = "none";
        headerEl.style.height = "32px";
        if (singlePlayer) {
            singlePlayer.disconnect();
        }
        initMultiview();
    }
}
tabs.forEach(tab => {
    tab.addEventListener("click", () => {
        const mode = tab.dataset.mode;
        if (mode)
            switchMode(mode);
    });
});
connectBtn.addEventListener("click", () => {
    if (currentMode !== "single")
        return;
    if (singlePlayer?.isConnected()) {
        singlePlayer.disconnect();
        statusEl.textContent = "Disconnected.";
        connectBtn.textContent = "Watch";
        connectBtn.disabled = false;
        return;
    }
    const streamKey = streamKeyInput.value.trim();
    if (!streamKey) {
        statusEl.textContent = "Please enter a stream key.";
        return;
    }
    initSingleMode();
    connectBtn.disabled = true;
    statusEl.textContent = `Connecting to "${streamKey}"...`;
    singlePlayer.connect(streamKey);
});
srtPullBtn.addEventListener("click", () => {
    srtPullError.classList.remove("visible");
    srtPullModal.classList.add("visible");
    srtAddress.focus();
});
srtPullCancel.addEventListener("click", () => {
    srtPullModal.classList.remove("visible");
});
srtPullModal.addEventListener("click", (e) => {
    if (e.target === srtPullModal) {
        srtPullModal.classList.remove("visible");
    }
});
async function waitForStream(key, timeoutMs = 15000) {
    const start = Date.now();
    while (Date.now() - start < timeoutMs) {
        try {
            const resp = await fetch("/api/streams");
            const streams = await resp.json();
            if (streams.some(s => s.key === key))
                return true;
        }
        catch { /* retry */ }
        await new Promise(r => setTimeout(r, 500));
    }
    return false;
}
srtPullConnect.addEventListener("click", async () => {
    const address = srtAddress.value.trim();
    const streamKey = srtStreamKey.value.trim();
    const streamId = srtStreamId.value.trim();
    if (!address || !streamKey) {
        srtPullError.textContent = "Address and stream key are required.";
        srtPullError.classList.add("visible");
        return;
    }
    srtPullConnect.disabled = true;
    srtPullConnect.textContent = "Connecting...";
    srtPullError.classList.remove("visible");
    try {
        const resp = await fetch("/api/srt-pull", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ address, streamKey, streamId: streamId || undefined }),
        });
        if (!resp.ok) {
            const body = await resp.json().catch(() => ({ error: "Request failed" }));
            throw new Error(body.error || `HTTP ${resp.status}`);
        }
        srtPullModal.classList.remove("visible");
        statusEl.textContent = `Pulling SRT from ${address}... waiting for stream`;
        const found = await waitForStream(streamKey);
        if (!found) {
            statusEl.textContent = `Stream "${streamKey}" not yet available — try watching manually.`;
            streamKeyInput.value = streamKey;
            return;
        }
        await fetchStreams();
        if (currentMode === "single") {
            streamKeyInput.value = streamKey;
            initSingleMode();
            singlePlayer.connect(streamKey);
            statusEl.textContent = `Connected to SRT pull from ${address}`;
        }
        else {
            statusEl.textContent = `SRT pull from ${address} active`;
        }
    }
    catch (err) {
        srtPullError.textContent = err.message || "Failed to start SRT pull.";
        srtPullError.classList.add("visible");
    }
    finally {
        srtPullConnect.disabled = false;
        srtPullConnect.textContent = "Connect & Watch";
    }
});
document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && srtPullModal.classList.contains("visible")) {
        srtPullModal.classList.remove("visible");
    }
});
async function fetchStreams() {
    try {
        const resp = await fetch("/api/streams");
        const streams = await resp.json();
        const oldKeySet = new Set(cachedStreams.map(s => s.key));
        const newKeySet = new Set(streams.map(s => s.key));
        const keysChanged = oldKeySet.size !== newKeySet.size ||
            streams.some(s => !oldKeySet.has(s.key));
        cachedStreams = streams;
        streamListEl.innerHTML = "";
        for (const stream of streams) {
            const tag = document.createElement("span");
            tag.className = "stream-tag";
            const label = stream.description
                ? `${stream.key} — ${stream.description}`
                : stream.key;
            tag.textContent = label;
            tag.title = `${stream.viewers} viewer${stream.viewers !== 1 ? "s" : ""}`;
            tag.addEventListener("click", () => {
                if (currentMode === "single") {
                    streamKeyInput.value = stream.key;
                    initSingleMode();
                    singlePlayer.connect(stream.key);
                }
            });
            streamListEl.appendChild(tag);
        }
        if (currentMode === "multi" && multiview && keysChanged && streams.length > 0) {
            multiview.connectAll(streams);
        }
    }
    catch {
        // ignore
    }
}
if (urlMode === "multi") {
    switchMode("multi");
}
else {
    initSingleMode();
    if (urlStreamKey) {
        singlePlayer.connect(urlStreamKey);
    }
}
fetchStreams();
setInterval(fetchStreams, 5000);
