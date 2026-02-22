// MoQ Transport draft-15 message type IDs.
export const MOQ_MSG_SUBSCRIBE = 0x03;
export const MOQ_MSG_SUBSCRIBE_OK = 0x04;
export const MOQ_MSG_SUBSCRIBE_ERROR = 0x05;
export const MOQ_MSG_UNSUBSCRIBE = 0x0a;
export const MOQ_MSG_GOAWAY = 0x10;
export const MOQ_MSG_MAX_REQUEST_ID = 0x15;
export const MOQ_MSG_CLIENT_SETUP = 0x20;
export const MOQ_MSG_SERVER_SETUP = 0x21;
// MoQ Transport version: draft-15.
export const MOQ_VERSION = 0xff00000f;
// Setup parameter keys (draft-15 section 6.2).
export const MOQ_PARAM_PATH = 0x01; // odd = length-prefixed byte string
export const MOQ_PARAM_MAX_REQUEST_ID = 0x02; // even = varint value
// Subscribe filter types (draft-15 section 6.6).
export const MOQ_FILTER_NEXT_GROUP_START = 0x01;
// Group order values.
export const MOQ_GROUP_ORDER_DESCENDING = 0x02;
// MoQ stream type for subgroup with SID extension.
export const MOQ_STREAM_TYPE_SUBGROUP_SID_EXT = 0x0d;
// LOC header extension IDs (draft-ietf-moq-loc-01).
export const LOC_EXT_CAPTURE_TIMESTAMP = 2;
export const LOC_EXT_VIDEO_FRAME_MARKING = 4;
export const LOC_EXT_VIDEO_CONFIG = 13;
// RFC 9626 Video Frame Marking flags.
export const VFM_KEYFRAME = 0xe0;
// --- QUIC Varint codec (RFC 9000 section 16) ---
/** Synchronously read a QUIC varint from buf at offset. Returns { value, bytesRead }. */
export function readVarint(buf, offset) {
    if (offset >= buf.length)
        throw new Error("varint: buffer underflow");
    const first = buf[offset];
    const prefix = first >> 6;
    const length = 1 << prefix; // 1, 2, 4, or 8
    if (offset + length > buf.length)
        throw new Error("varint: buffer underflow");
    let value = first & 0x3f;
    for (let i = 1; i < length; i++) {
        value = value * 256 + buf[offset + i];
    }
    return { value, bytesRead: length };
}
/** Encode a number as a QUIC varint. */
export function writeVarint(value) {
    if (value <= 0x3f) {
        return new Uint8Array([value]);
    }
    else if (value <= 0x3fff) {
        return new Uint8Array([(value >> 8) | 0x40, value & 0xff]);
    }
    else if (value <= 0x3fffffff) {
        return new Uint8Array([
            ((value >> 24) & 0x3f) | 0x80,
            (value >> 16) & 0xff,
            (value >> 8) & 0xff,
            value & 0xff,
        ]);
    }
    else {
        // 8-byte encoding
        const buf = new Uint8Array(8);
        // For JS safe integers (up to 2^53), use division-based approach
        const hi = Math.floor(value / 0x100000000);
        const lo = value >>> 0;
        buf[0] = ((hi >> 24) & 0x3f) | 0xc0;
        buf[1] = (hi >> 16) & 0xff;
        buf[2] = (hi >> 8) & 0xff;
        buf[3] = hi & 0xff;
        buf[4] = (lo >> 24) & 0xff;
        buf[5] = (lo >> 16) & 0xff;
        buf[6] = (lo >> 8) & 0xff;
        buf[7] = lo & 0xff;
        return buf;
    }
}
/** Async read a QUIC varint from a StreamBuffer. Returns null on EOF. */
export async function readVarintFromBuffer(buffer) {
    const firstByte = await buffer.read(1);
    if (!firstByte)
        return null;
    const prefix = firstByte[0] >> 6;
    const length = 1 << prefix; // 1, 2, 4, or 8
    let value = firstByte[0] & 0x3f;
    if (length > 1) {
        const rest = await buffer.read(length - 1);
        if (!rest)
            return null;
        for (let i = 0; i < rest.length; i++) {
            value = value * 256 + rest[i];
        }
    }
    return value;
}
/** Async read a varint-length-prefixed byte string from a StreamBuffer. */
export async function readVarIntBytesFromBuffer(buffer) {
    const len = await readVarintFromBuffer(buffer);
    if (len === null)
        return null;
    if (len === 0)
        return new Uint8Array(0);
    return buffer.read(len);
}
// --- Control message framing ---
/** Write a MoQ control message: [varint type][uint16 BE len][payload]. */
export async function writeControlMsg(writer, msgType, payload) {
    const typeBytes = writeVarint(msgType);
    const lenBuf = new Uint8Array(2);
    new DataView(lenBuf.buffer).setUint16(0, payload.length, false);
    const msg = new Uint8Array(typeBytes.length + 2 + payload.length);
    msg.set(typeBytes, 0);
    msg.set(lenBuf, typeBytes.length);
    msg.set(payload, typeBytes.length + 2);
    await writer.write(msg);
}
/** Async read a MoQ control message from a StreamBuffer. Returns null on EOF. */
export async function readControlMsgFromBuffer(buffer) {
    const msgType = await readVarintFromBuffer(buffer);
    if (msgType === null)
        return null;
    const lenBytes = await buffer.read(2);
    if (!lenBytes)
        return null;
    const length = new DataView(lenBytes.buffer, lenBytes.byteOffset, 2).getUint16(0, false);
    let payload;
    if (length === 0) {
        payload = new Uint8Array(0);
    }
    else {
        const p = await buffer.read(length);
        if (!p)
            return null;
        payload = p;
    }
    return { type: msgType, payload };
}
// --- Serialization helpers ---
function appendVarint(buf, value) {
    buf.push(writeVarint(value));
}
function appendVarIntBytes(buf, data) {
    buf.push(writeVarint(data.length));
    buf.push(data);
}
function appendNamespaceTuple(buf, parts) {
    appendVarint(buf, parts.length);
    for (const p of parts) {
        appendVarIntBytes(buf, new TextEncoder().encode(p));
    }
}
function concatBuffers(parts) {
    let total = 0;
    for (const p of parts)
        total += p.length;
    const result = new Uint8Array(total);
    let offset = 0;
    for (const p of parts) {
        result.set(p, offset);
        offset += p.length;
    }
    return result;
}
/** Serialize a CLIENT_SETUP message payload. */
export function serializeClientSetup(versions, path, maxRequestID) {
    const parts = [];
    // Versions
    appendVarint(parts, versions.length);
    for (const v of versions)
        appendVarint(parts, v);
    // Params
    let numParams = 0;
    if (path)
        numParams++;
    if (maxRequestID > 0)
        numParams++;
    appendVarint(parts, numParams);
    if (path) {
        appendVarint(parts, MOQ_PARAM_PATH);
        appendVarIntBytes(parts, new TextEncoder().encode(path));
    }
    if (maxRequestID > 0) {
        appendVarint(parts, MOQ_PARAM_MAX_REQUEST_ID);
        appendVarint(parts, maxRequestID);
    }
    return concatBuffers(parts);
}
/** Serialize a SUBSCRIBE message payload. */
export function serializeSubscribe(requestID, namespace, trackName, priority, filterType) {
    const parts = [];
    appendVarint(parts, requestID);
    appendNamespaceTuple(parts, namespace);
    appendVarIntBytes(parts, new TextEncoder().encode(trackName));
    parts.push(new Uint8Array([priority])); // priority (1 byte)
    parts.push(new Uint8Array([MOQ_GROUP_ORDER_DESCENDING])); // group order
    parts.push(new Uint8Array([0])); // forward
    appendVarint(parts, filterType);
    appendVarint(parts, 0); // NumParams = 0
    return concatBuffers(parts);
}
/** Serialize an UNSUBSCRIBE message payload. */
export function serializeUnsubscribe(requestID) {
    return writeVarint(requestID);
}
/** Parse a SUBSCRIBE_OK payload. */
export function parseSubscribeOK(data) {
    let offset = 0;
    const reqID = readVarint(data, offset);
    offset += reqID.bytesRead;
    const alias = readVarint(data, offset);
    offset += alias.bytesRead;
    const expires = readVarint(data, offset);
    offset += expires.bytesRead;
    const groupOrder = data[offset++];
    const exists = data[offset++];
    let largestGroup = 0;
    let largestObj = 0;
    if (exists === 1) {
        const lg = readVarint(data, offset);
        offset += lg.bytesRead;
        largestGroup = lg.value;
        const lo = readVarint(data, offset);
        offset += lo.bytesRead;
        largestObj = lo.value;
    }
    return {
        requestID: reqID.value,
        trackAlias: alias.value,
        expires: expires.value,
        groupOrder,
        contentExists: exists === 1,
        largestGroup,
        largestObj,
    };
}
/** Parse a SUBSCRIBE_ERROR payload. */
export function parseSubscribeError(data) {
    let offset = 0;
    const reqID = readVarint(data, offset);
    offset += reqID.bytesRead;
    const code = readVarint(data, offset);
    offset += code.bytesRead;
    const reasonLen = readVarint(data, offset);
    offset += reasonLen.bytesRead;
    const reason = new TextDecoder().decode(data.subarray(offset, offset + reasonLen.value));
    return {
        requestID: reqID.value,
        errorCode: code.value,
        reasonPhrase: reason,
    };
}
/** Parse a SERVER_SETUP payload. Returns { selectedVersion, maxRequestID }. */
export function parseServerSetup(data) {
    let offset = 0;
    const ver = readVarint(data, offset);
    offset += ver.bytesRead;
    let maxRequestID = 0;
    const numParams = readVarint(data, offset);
    offset += numParams.bytesRead;
    for (let i = 0; i < numParams.value; i++) {
        const key = readVarint(data, offset);
        offset += key.bytesRead;
        if (key.value % 2 === 1) {
            // odd: length-prefixed bytes
            const len = readVarint(data, offset);
            offset += len.bytesRead;
            offset += len.value;
        }
        else {
            // even: varint
            const val = readVarint(data, offset);
            offset += val.bytesRead;
            if (key.value === MOQ_PARAM_MAX_REQUEST_ID) {
                maxRequestID = val.value;
            }
        }
    }
    return { selectedVersion: ver.value, maxRequestID };
}
/** Parse LOC extensions from a byte buffer. */
export function parseExtensions(data) {
    const result = {
        captureTimestamp: 0,
        isKeyframe: false,
        videoConfig: null,
    };
    let offset = 0;
    while (offset < data.length) {
        const id = readVarint(data, offset);
        offset += id.bytesRead;
        if (id.value % 2 === 1) {
            // Odd ID: length-prefixed bytes
            const len = readVarint(data, offset);
            offset += len.bytesRead;
            const bytes = data.subarray(offset, offset + len.value);
            offset += len.value;
            if (id.value === LOC_EXT_VIDEO_CONFIG) {
                result.videoConfig = bytes;
            }
        }
        else {
            // Even ID: varint value
            const val = readVarint(data, offset);
            offset += val.bytesRead;
            if (id.value === LOC_EXT_CAPTURE_TIMESTAMP) {
                result.captureTimestamp = val.value;
            }
            else if (id.value === LOC_EXT_VIDEO_FRAME_MARKING) {
                result.isKeyframe = (val.value & 0x20) !== 0; // I bit
            }
        }
    }
    return result;
}
