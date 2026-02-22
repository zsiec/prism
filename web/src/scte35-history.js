const MAX_EVENTS = 500;
const CHANNEL_NAMES = {
    0x30: "Provider Ad Start",
    0x31: "Provider Ad End",
    0x32: "Distributor Ad Start",
    0x33: "Distributor Ad End",
    0x34: "Provider PO Start",
    0x35: "Provider PO End",
    0x36: "Distributor PO Start",
    0x37: "Distributor PO End",
    0x40: "Unscheduled Event Start",
    0x41: "Unscheduled Event End",
};
export class SCTE35HistoryPanel {
    el;
    listEl;
    countBadge;
    toggleBtn;
    events = [];
    expanded = false;
    filterKey = null;
    constructor() {
        this.el = document.createElement("div");
        this.el.style.position = "relative";
        this.el.style.width = "0px";
        this.el.style.transition = "width 0.2s ease";
        this.el.style.overflow = "hidden";
        this.el.style.flexShrink = "0";
        this.el.style.display = "flex";
        this.el.style.flexDirection = "column";
        this.el.style.background = "#0f0f0f";
        this.el.style.borderLeft = "1px solid #1a1a1a";
        this.toggleBtn = document.createElement("button");
        this.toggleBtn.style.position = "absolute";
        this.toggleBtn.style.left = "-36px";
        this.toggleBtn.style.top = "8px";
        this.toggleBtn.style.width = "32px";
        this.toggleBtn.style.height = "32px";
        this.toggleBtn.style.background = "rgba(168, 85, 247, 0.2)";
        this.toggleBtn.style.border = "1px solid rgba(168, 85, 247, 0.4)";
        this.toggleBtn.style.borderRadius = "3px 0 0 3px";
        this.toggleBtn.style.color = "#c084fc";
        this.toggleBtn.style.cursor = "pointer";
        this.toggleBtn.style.fontSize = "10px";
        this.toggleBtn.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        this.toggleBtn.style.fontWeight = "700";
        this.toggleBtn.style.letterSpacing = "0.02em";
        this.toggleBtn.style.display = "flex";
        this.toggleBtn.style.alignItems = "center";
        this.toggleBtn.style.justifyContent = "center";
        this.toggleBtn.style.zIndex = "30";
        this.toggleBtn.title = "SCTE-35 History (H)";
        this.toggleBtn.textContent = "S35";
        this.toggleBtn.addEventListener("click", () => this.toggle());
        this.toggleBtn.addEventListener("mouseenter", () => {
            this.toggleBtn.style.background = "rgba(168, 85, 247, 0.4)";
        });
        this.toggleBtn.addEventListener("mouseleave", () => {
            this.toggleBtn.style.background = "rgba(168, 85, 247, 0.2)";
        });
        this.el.appendChild(this.toggleBtn);
        this.countBadge = document.createElement("span");
        this.countBadge.style.position = "absolute";
        this.countBadge.style.top = "-4px";
        this.countBadge.style.right = "-4px";
        this.countBadge.style.background = "#a855f7";
        this.countBadge.style.color = "#fff";
        this.countBadge.style.fontSize = "9px";
        this.countBadge.style.fontWeight = "700";
        this.countBadge.style.borderRadius = "3px";
        this.countBadge.style.padding = "1px 5px";
        this.countBadge.style.minWidth = "14px";
        this.countBadge.style.textAlign = "center";
        this.countBadge.style.display = "none";
        this.toggleBtn.appendChild(this.countBadge);
        const header = document.createElement("div");
        header.style.padding = "12px 12px 8px";
        header.style.borderBottom = "1px solid #1a1a1a";
        header.style.flexShrink = "0";
        const titleRow = document.createElement("div");
        titleRow.style.display = "flex";
        titleRow.style.alignItems = "center";
        titleRow.style.justifyContent = "space-between";
        const title = document.createElement("span");
        title.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        title.style.fontSize = "12px";
        title.style.fontWeight = "700";
        title.style.color = "#c084fc";
        title.style.letterSpacing = "0.04em";
        title.textContent = "SCTE-35 HISTORY";
        titleRow.appendChild(title);
        const clearBtn = document.createElement("button");
        clearBtn.style.background = "none";
        clearBtn.style.border = "none";
        clearBtn.style.color = "#64748b";
        clearBtn.style.fontSize = "10px";
        clearBtn.style.cursor = "pointer";
        clearBtn.style.padding = "2px 6px";
        clearBtn.textContent = "Clear";
        clearBtn.addEventListener("click", () => this.clear());
        clearBtn.addEventListener("mouseenter", () => { clearBtn.style.color = "#94a3b8"; });
        clearBtn.addEventListener("mouseleave", () => { clearBtn.style.color = "#64748b"; });
        titleRow.appendChild(clearBtn);
        header.appendChild(titleRow);
        this.el.appendChild(header);
        this.listEl = document.createElement("div");
        this.listEl.style.flex = "1";
        this.listEl.style.overflowY = "auto";
        this.listEl.style.padding = "4px 0";
        this.listEl.style.fontFamily = "'SF Mono', 'Menlo', monospace";
        this.listEl.style.fontSize = "10px";
        this.el.appendChild(this.listEl);
    }
    getElement() {
        return this.el;
    }
    toggle() {
        this.expanded = !this.expanded;
        this.el.style.width = this.expanded ? "280px" : "0px";
    }
    addEvent(streamKey, event) {
        const id = `${event.receivedAt}-${event.commandType}-${event.eventId ?? 0}-${streamKey}`;
        const entry = { streamKey, event, id };
        this.events.unshift(entry);
        if (this.events.length > MAX_EVENTS) {
            this.events.length = MAX_EVENTS;
        }
        this.updateBadge();
        this.prependRow(entry);
    }
    clear() {
        this.events = [];
        this.listEl.innerHTML = "";
        this.updateBadge();
    }
    updateBadge() {
        const count = this.events.length;
        if (count > 0) {
            this.countBadge.style.display = "";
            this.countBadge.textContent = count > 99 ? "99+" : String(count);
        }
        else {
            this.countBadge.style.display = "none";
        }
    }
    prependRow(entry) {
        if (this.filterKey && entry.streamKey !== this.filterKey)
            return;
        const row = this.buildRow(entry);
        if (this.listEl.firstChild) {
            this.listEl.insertBefore(row, this.listEl.firstChild);
        }
        else {
            this.listEl.appendChild(row);
        }
        // Flash animation
        row.style.background = "rgba(168, 85, 247, 0.3)";
        setTimeout(() => { row.style.background = ""; }, 600);
        while (this.listEl.children.length > MAX_EVENTS) {
            this.listEl.removeChild(this.listEl.lastChild);
        }
    }
    buildRow(entry) {
        const row = document.createElement("div");
        row.style.padding = "6px 12px";
        row.style.borderBottom = "1px solid #1a1a1a";
        row.style.transition = "background 0.3s ease";
        row.style.cursor = "default";
        row.addEventListener("mouseenter", () => {
            row.style.background = "rgba(168, 85, 247, 0.1)";
        });
        row.addEventListener("mouseleave", () => {
            row.style.background = "";
        });
        const topLine = document.createElement("div");
        topLine.style.display = "flex";
        topLine.style.justifyContent = "space-between";
        topLine.style.alignItems = "center";
        topLine.style.marginBottom = "2px";
        const streamTag = document.createElement("span");
        streamTag.style.color = "#60a5fa";
        streamTag.style.fontWeight = "600";
        streamTag.style.fontSize = "10px";
        streamTag.textContent = entry.streamKey;
        topLine.appendChild(streamTag);
        const time = document.createElement("span");
        time.style.color = "#64748b";
        time.style.fontSize = "9px";
        const d = new Date(entry.event.receivedAt);
        time.textContent = d.toLocaleTimeString([], {
            hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
        });
        topLine.appendChild(time);
        row.appendChild(topLine);
        const descLine = document.createElement("div");
        descLine.style.display = "flex";
        descLine.style.gap = "6px";
        descLine.style.alignItems = "center";
        const typeTag = document.createElement("span");
        typeTag.style.color = "#c084fc";
        typeTag.style.fontWeight = "600";
        typeTag.textContent = entry.event.commandType;
        descLine.appendChild(typeTag);
        const desc = document.createElement("span");
        desc.style.color = "#94a3b8";
        desc.textContent = entry.event.description || this.segTypeLabel(entry.event.segmentationTypeId);
        descLine.appendChild(desc);
        row.appendChild(descLine);
        if (entry.event.duration && entry.event.duration > 0) {
            const meta = document.createElement("div");
            meta.style.color = "#64748b";
            meta.style.fontSize = "9px";
            meta.style.marginTop = "2px";
            const parts = [];
            parts.push(`dur: ${entry.event.duration.toFixed(1)}s`);
            if (entry.event.eventId !== undefined)
                parts.push(`id: ${entry.event.eventId}`);
            if (entry.event.outOfNetwork)
                parts.push("OON");
            meta.textContent = parts.join(" | ");
            row.appendChild(meta);
        }
        return row;
    }
    segTypeLabel(typeId) {
        if (typeId === undefined)
            return "";
        return CHANNEL_NAMES[typeId] ?? `0x${typeId.toString(16)}`;
    }
}
