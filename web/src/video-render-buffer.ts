const MAX_ELEMENTS = 90;

/** Result of a timestamp-based frame lookup, including the selected frame and discard statistics. */
interface VideoRenderResult {
	frame: VideoFrame | null;
	discarded: number;
	totalDiscarded: number;
	queueSize: number;
	queueLengthMs: number;
}

/**
 * Ring-buffer backed video frame queue. Uses a head pointer to avoid O(n) array
 * shifts on every dequeue. Compacts only when the dead zone exceeds half the
 * backing array length. Uses binary search for timestamp lookups.
 */
export class VideoRenderBuffer {
	private frames: (VideoFrame | null)[] = [];
	private head = 0;
	private len = 0;
	private totalDiscarded = 0;
	private totalLengthMs = 0;

	private get tail(): number { return this.head + this.len; }

	addFrame(frame: VideoFrame): boolean {
		if (this.len >= MAX_ELEMENTS) {
			const oldest = this.frames[this.head]!;
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

	getFrameByTimestamp(ts: number): VideoRenderResult {
		const result: VideoRenderResult = {
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
			if (this.frames[mid]!.timestamp <= ts) {
				lo = mid + 1;
			} else {
				hi = mid;
			}
		}
		const lastInPast = lo;

		const discardEnd = lastInPast - 1;
		for (let i = this.head; i < discardEnd; i++) {
			const f = this.frames[i]!;
			this.totalLengthMs -= (f.duration ?? 0) / 1000;
			f.close();
			this.frames[i] = null;
			result.discarded++;
		}

		if (lastInPast > this.head) {
			const idx = discardEnd >= this.head ? discardEnd : this.head;
			result.frame = this.frames[idx]!;
			this.frames[idx] = null;
			this.totalLengthMs -= (result.frame.duration ?? 0) / 1000;
			this.head = idx + 1;
			this.len = end - this.head;
		} else {
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

	peekFirstFrame(): VideoFrame | null {
		return this.len > 0 ? this.frames[this.head] : null;
	}

	takeNextFrame(): VideoFrame | null {
		if (this.len === 0) return null;
		const frame = this.frames[this.head]!;
		this.frames[this.head] = null;
		this.totalLengthMs -= (frame.duration ?? 0) / 1000;
		this.head++;
		this.len--;
		this.maybeCompact();
		return frame;
	}

	getStats(): { queueSize: number; queueLengthMs: number; totalDiscarded: number } {
		return {
			queueSize: this.len,
			queueLengthMs: this.totalLengthMs,
			totalDiscarded: this.totalDiscarded,
		};
	}

	clear(): void {
		const end = this.tail;
		for (let i = this.head; i < end; i++) {
			this.frames[i]!.close();
		}
		this.frames.length = 0;
		this.head = 0;
		this.len = 0;
		this.totalLengthMs = 0;
		this.totalDiscarded = 0;
	}

	private maybeCompact(): void {
		if (this.head > 0 && this.head > this.frames.length / 2) {
			this.frames = this.frames.slice(this.head, this.tail);
			this.head = 0;
		}
	}
}
