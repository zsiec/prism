import { MoQTransport } from "./moq-transport";
/**
 * MoQ multiview transport: manages N independent MoQTransport instances,
 * one per tile. Each tile starts receiving frames as soon as its own
 * transport completes the handshake + catalog exchange — no waiting for
 * slower peers.
 */
export class MoQMultiviewTransport {
    streamKeys;
    callbacks;
    transports = [];
    streamCallbacks = new Map();
    latestStats = new Map();
    latestViewerStats = new Map();
    closed = false;
    statsInterval = null;
    constructor(streamKeys, callbacks) {
        this.streamKeys = streamKeys;
        this.callbacks = callbacks;
    }
    async connect() {
        // One-time setup (AudioContext, compositor, etc.) before any transports start.
        await this.callbacks.onSetup();
        // Track per-stream completion so we know when all are done.
        const perStreamDone = [];
        for (let i = 0; i < this.streamKeys.length; i++) {
            const index = i;
            const key = this.streamKeys[i];
            let resolved = false;
            let onReady;
            const readyPromise = new Promise(resolve => {
                onReady = () => { if (!resolved) {
                    resolved = true;
                    resolve();
                } };
            });
            perStreamDone.push(readyPromise);
            const moqCallbacks = this.buildCallbacks(index, key, async (tracks) => {
                // This stream's catalog arrived — set up its tile immediately.
                const entry = { index, key, tracks };
                await this.callbacks.onStreamReady(entry);
                onReady();
            }, onReady);
            const transport = new MoQTransport(key, moqCallbacks);
            this.transports.push(transport);
        }
        // Fire off all connections in parallel — don't block on connect().
        // Each transport will call onStreamReady independently when its catalog arrives.
        for (const t of this.transports) {
            t.connect().catch(() => { });
        }
        // Wait for all streams to finish setup, then notify.
        await Promise.all(perStreamDone);
        this.callbacks.onAllReady();
        // Start periodic stats aggregation (mirrors muxStatsTickerLoop)
        this.statsInterval = setInterval(() => {
            if (this.closed)
                return;
            if (this.latestStats.size > 0) {
                const stats = {};
                for (const [key, stat] of this.latestStats) {
                    stats[key] = stat;
                }
                const viewerStats = this.latestViewerStats.size > 0
                    ? Object.fromEntries(this.latestViewerStats)
                    : undefined;
                this.callbacks.onMuxStats(stats, viewerStats);
            }
        }, 1000);
    }
    setStreamCallbacks(index, cb) {
        this.streamCallbacks.set(index, cb);
    }
    enableAllAudio() {
        for (const transport of this.transports) {
            transport.subscribeAllAudio();
        }
    }
    disableAllAudio() {
        for (const transport of this.transports) {
            transport.subscribeAudio([]);
        }
    }
    enableAudio(index) {
        const transport = this.transports[index];
        if (transport) {
            transport.subscribeAllAudio();
        }
    }
    disableAudio(index) {
        const transport = this.transports[index];
        if (transport) {
            transport.subscribeAudio([]);
        }
    }
    close() {
        this.closed = true;
        if (this.statsInterval) {
            clearInterval(this.statsInterval);
            this.statsInterval = null;
        }
        for (const transport of this.transports) {
            transport.close();
        }
        this.transports = [];
        this.streamCallbacks.clear();
        this.latestStats.clear();
        this.latestViewerStats.clear();
    }
    buildCallbacks(index, key, onTracks, onReady) {
        return {
            onTrackInfo: async (tracks) => {
                await onTracks(tracks);
            },
            onVideoFrame: (data, isKeyframe, timestamp, groupID, description) => {
                const cb = this.streamCallbacks.get(index);
                if (cb) {
                    cb.onVideoFrame(data, isKeyframe, timestamp, groupID, description);
                }
            },
            onAudioFrame: (data, timestamp, groupID, trackIndex) => {
                const cb = this.streamCallbacks.get(index);
                if (cb) {
                    cb.onAudioFrame(data, timestamp, groupID, trackIndex);
                }
            },
            onCaptionFrame: (caption, timestamp) => {
                const cb = this.streamCallbacks.get(index);
                if (cb?.onCaptionFrame) {
                    cb.onCaptionFrame(caption, timestamp);
                }
            },
            onServerStats: (stats) => {
                this.latestStats.set(key, stats);
            },
            onViewerStats: (vs) => {
                this.latestViewerStats.set(key, {
                    id: vs.id,
                    videoSent: vs.videoSent,
                    audioSent: vs.audioSent,
                    captionSent: vs.captionSent,
                    videoDropped: vs.videoDropped,
                    audioDropped: vs.audioDropped,
                    captionDropped: vs.captionDropped,
                    bytesSent: vs.bytesSent,
                });
            },
            onClose: () => {
                onReady(); // prevent deadlock if transport closes before onTrackInfo
                if (!this.closed) {
                    this.callbacks.onClose();
                }
            },
            onError: (err) => {
                onReady(); // prevent deadlock if transport errors before onTrackInfo
                this.callbacks.onError(`[${key}] ${err}`);
            },
        };
    }
}
