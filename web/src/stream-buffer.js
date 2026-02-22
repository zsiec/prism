/**
 * Buffered reader for a ReadableStream that supports exact-length reads.
 * Accumulates chunks from the underlying stream and returns contiguous
 * byte arrays of the requested size.
 */
export class StreamBuffer {
    reader;
    chunks = [];
    totalBytes = 0;
    constructor(reader) {
        this.reader = reader;
    }
    async read(n) {
        while (this.totalBytes < n) {
            const { value, done } = await this.reader.read();
            if (done || !value) {
                if (this.totalBytes === 0)
                    return null;
                break;
            }
            this.chunks.push(value);
            this.totalBytes += value.length;
        }
        if (this.totalBytes < n)
            return null;
        const result = new Uint8Array(n);
        let offset = 0;
        let remaining = n;
        while (remaining > 0 && this.chunks.length > 0) {
            const chunk = this.chunks[0];
            const take = Math.min(chunk.length, remaining);
            result.set(chunk.subarray(0, take), offset);
            offset += take;
            remaining -= take;
            if (take < chunk.length) {
                this.chunks[0] = chunk.subarray(take);
            }
            else {
                this.chunks.shift();
            }
            this.totalBytes -= take;
        }
        return result;
    }
}
