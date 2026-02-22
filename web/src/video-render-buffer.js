const MAX_ELEMENTS = 90;
/**
 * Ring-buffer backed video frame queue. Uses a head pointer to avoid O(n) array
 * shifts on every dequeue. Compacts only when the dead zone exceeds half the
 * backing array length. Uses binary search for timestamp lookups.
 */
export class VideoRenderBuffer {
    frames = [];
    head = 0;
    len = 0;
    totalDiscarded = 0;
    totalLengthMs = 0;
    get tail() { return this.head + this.len; }
    addFrame(frame) {
        if (this.len >= MAX_ELEMENTS) {
            const oldest = this.frames[this.head];
            this.totalLengthMs -= (oldest.duration ?? 0) / 1000;
            oldest.close();
            this.frames[this.head] = null;
            this.head++;
            this.len--;
            this.totalDiscarded++;
        }
        this.frames[this.tail] = frame;
        this.len++;
        this.totalLengthMs += (frame.duration ?? 0) / 1000;
        return true;
    }
    getFrameByTimestamp(ts) {
        const result = {
            frame: null,
            discarded: 0,
            totalDiscarded: this.totalDiscarded,
            queueSize: this.len,
            queueLengthMs: this.totalLengthMs,
        };
        const end = this.tail;
        // Binary search for the last frame with timestamp <= ts
        let lo = this.head;
        let hi = end;
        while (lo < hi) {
            const mid = (lo + hi) >>> 1;
            if (this.frames[mid].timestamp <= ts) {
                lo = mid + 1;
            }
            else {
                hi = mid;
            }
        }
        const lastInPast = lo;
        const discardEnd = lastInPast - 1;
        for (let i = this.head; i < discardEnd; i++) {
            const f = this.frames[i];
            this.totalLengthMs -= (f.duration ?? 0) / 1000;
            f.close();
            this.frames[i] = null;
            result.discarded++;
        }
        if (lastInPast > this.head) {
            const idx = discardEnd >= this.head ? discardEnd : this.head;
            result.frame = this.frames[idx];
            this.frames[idx] = null;
            this.totalLengthMs -= (result.frame.duration ?? 0) / 1000;
            this.head = idx + 1;
            this.len = end - this.head;
        }
        else {
            this.head = discardEnd >= this.head ? discardEnd : this.head;
            this.len = end - this.head;
        }
        this.totalDiscarded += result.discarded;
        result.totalDiscarded = this.totalDiscarded;
        result.queueSize = this.len;
        result.queueLengthMs = this.totalLengthMs;
        this.maybeCompact();
        return result;
    }
    peekFirstFrame() {
        return this.len > 0 ? this.frames[this.head] : null;
    }
    takeNextFrame() {
        if (this.len === 0)
            return null;
        const frame = this.frames[this.head];
        this.frames[this.head] = null;
        this.totalLengthMs -= (frame.duration ?? 0) / 1000;
        this.head++;
        this.len--;
        this.maybeCompact();
        return frame;
    }
    getStats() {
        return {
            queueSize: this.len,
            queueLengthMs: this.totalLengthMs,
            totalDiscarded: this.totalDiscarded,
        };
    }
    clear() {
        const end = this.tail;
        for (let i = this.head; i < end; i++) {
            this.frames[i].close();
        }
        this.frames.length = 0;
        this.head = 0;
        this.len = 0;
        this.totalLengthMs = 0;
        this.totalDiscarded = 0;
    }
    maybeCompact() {
        if (this.head > 0 && this.head > this.frames.length / 2) {
            this.frames = this.frames.slice(this.head, this.tail);
            this.head = 0;
        }
    }
}
