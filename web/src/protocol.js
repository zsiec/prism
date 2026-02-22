/** Two-byte magic number identifying the Prism binary caption format. */
const CAPTION_MAGIC = 0xCC02;
function rgbToHex(r, g, b) {
    const hex = "0123456789abcdef";
    return hex[r >> 4] + hex[r & 0x0F] + hex[g >> 4] + hex[g & 0x0F] + hex[b >> 4] + hex[b & 0x0F];
}
/**
 * Decode a binary caption payload into structured CaptionData. Handles both
 * the Prism binary caption format (identified by CAPTION_MAGIC) and legacy
 * plain-text payloads where the first byte is the channel number.
 */
export function parseCaptionData(data) {
    if (data.length < 2) {
        return { channel: data.length > 0 ? data[0] : 0, text: "", regions: [] };
    }
    const magic = (data[0] << 8) | data[1];
    if (magic !== CAPTION_MAGIC) {
        const channel = data[0];
        const text = new TextDecoder().decode(data.subarray(1));
        return { channel, text, regions: [] };
    }
    if (data.length < 5) {
        return { channel: 0, text: "", regions: [] };
    }
    const version = data[2];
    const channel = data[3];
    const regionCount = data[4];
    const regions = [];
    let pos = 5;
    for (let i = 0; i < regionCount && pos < data.length; i++) {
        if (pos + 3 > data.length)
            break;
        const id = data[pos++];
        const flags = data[pos++];
        const justify = flags & 0x03;
        const scrollDirection = (flags >> 2) & 0x03;
        const printDirection = (flags >> 4) & 0x03;
        const wordWrap = (flags & 0x40) !== 0;
        const relativeToggle = (flags & 0x80) !== 0;
        const fillFlags = data[pos++];
        const fillOpacity = (fillFlags >> 6) & 0x03;
        const borderType = (fillFlags >> 3) & 0x07;
        const priority = fillFlags & 0x07;
        let fillColor = "000000";
        let borderColor = "000000";
        let anchorV = 0;
        let anchorH = 0;
        let anchorID = 0;
        if (version >= 2) {
            if (pos + 9 > data.length)
                break;
            fillColor = rgbToHex(data[pos], data[pos + 1], data[pos + 2]);
            pos += 3;
            borderColor = rgbToHex(data[pos], data[pos + 1], data[pos + 2]);
            pos += 3;
            anchorV = data[pos++];
            anchorH = data[pos++];
            anchorID = data[pos++];
        }
        if (pos + 2 > data.length)
            break;
        const rowCount = (data[pos] << 8) | data[pos + 1];
        pos += 2;
        const rows = [];
        for (let r = 0; r < rowCount && pos < data.length; r++) {
            if (pos + 2 > data.length)
                break;
            const rowIdx = data[pos++];
            const spanCount = data[pos++];
            const spans = [];
            for (let s = 0; s < spanCount && pos < data.length; s++) {
                if (pos + 2 > data.length)
                    break;
                const textLen = (data[pos] << 8) | data[pos + 1];
                pos += 2;
                if (pos + textLen > data.length)
                    break;
                const text = new TextDecoder().decode(data.subarray(pos, pos + textLen));
                pos += textLen;
                if (pos + 9 > data.length)
                    break;
                const fgColor = rgbToHex(data[pos], data[pos + 1], data[pos + 2]);
                pos += 3;
                const bgColor = rgbToHex(data[pos], data[pos + 1], data[pos + 2]);
                pos += 3;
                const attr0 = data[pos++];
                const fgOpacity = (attr0 >> 6) & 0x03;
                const bgOpacity = (attr0 >> 4) & 0x03;
                const italic = (attr0 & 0x08) !== 0;
                const underline = (attr0 & 0x04) !== 0;
                const flash = (attr0 & 0x02) !== 0;
                let penSize = (attr0 & 0x01) << 1;
                const attr1 = data[pos++];
                penSize |= (attr1 >> 7) & 0x01;
                const fontTag = (attr1 >> 4) & 0x07;
                const offset = (attr1 >> 2) & 0x03;
                let edgeType = attr1 & 0x03;
                const attr2 = data[pos++];
                edgeType |= ((attr2 >> 7) & 0x01) << 2;
                if (pos + 3 > data.length)
                    break;
                const edgeColor = rgbToHex(data[pos], data[pos + 1], data[pos + 2]);
                pos += 3;
                spans.push({
                    text, fgColor, bgColor, fgOpacity, bgOpacity,
                    italic, underline, flash, penSize, fontTag,
                    offset, edgeType, edgeColor,
                });
            }
            rows.push({ row: rowIdx, spans });
        }
        regions.push({
            id, justify, scrollDirection, printDirection,
            wordWrap, fillColor, fillOpacity, borderColor, borderType,
            anchorV, anchorH, anchorID, relativeToggle, priority, rows,
        });
    }
    let plainText = "";
    for (const reg of regions) {
        for (const row of reg.rows) {
            let line = "";
            for (const span of row.spans) {
                line += span.text;
            }
            if (line) {
                if (plainText)
                    plainText += "\n";
                plainText += line;
            }
        }
    }
    return { channel, text: plainText, regions };
}
