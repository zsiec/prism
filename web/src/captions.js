const CHANNEL_NAMES = {
    1: "CC1",
    2: "CC2",
    3: "CC3",
    4: "CC4",
    7: "Service 1",
    8: "Service 2",
    9: "Service 3",
    10: "Service 4",
    11: "Service 5",
    12: "Service 6",
};
const FONT_FAMILIES = {
    0: "monospace",
    1: "'Courier New', monospace, serif",
    2: "'Times New Roman', serif",
    3: "'Courier New', monospace, sans-serif",
    4: "'Arial', sans-serif",
    5: "'Comic Sans MS', cursive",
    6: "'Brush Script MT', cursive",
    7: "'Engravers Gothic', monospace, small-caps",
};
const OPACITY_VALUES = {
    0: 1.0,
    1: 1.0,
    2: 0.5,
    3: 0.0,
};
const JUSTIFY_MAP = {
    0: "left",
    1: "right",
    2: "center",
    3: "justify",
};
const BORDER_STYLES = {
    0: () => "none",
    1: (c) => `2px ridge #${c}`,
    2: (c) => `2px groove #${c}`,
    3: (c) => `2px solid #${c}`,
    4: (c) => `2px outset #${c}`,
    5: (c) => `2px inset #${c}`,
};
const CEA608_ROW_COUNT = 15;
const CEA708_SAFE_AREA_PERCENT = 90;
/**
 * Renders CEA-608 and CEA-708 closed captions into an HTML overlay.
 * Handles channel selection, auto-hide timers, and a popup menu for
 * choosing between available caption channels. Supports both legacy
 * 608 row-based positioning and 708 region-based window placement
 * with full pen attribute rendering (color, font, edge effects).
 */
export class CaptionRenderer {
    el;
    wrapperEl;
    btnEl;
    playerUI = null;
    hideTimer = null;
    activeChannel = 0;
    channels = new Set();
    menuOpen = false;
    suppressAutoSelect = false;
    _onDocClick;
    constructor(captionEl, playerUI, suppressAutoSelect) {
        this.suppressAutoSelect = suppressAutoSelect ?? false;
        this.el = captionEl;
        this.wrapperEl = document.createElement("div");
        this.wrapperEl.style.position = "relative";
        this.btnEl = document.createElement("button");
        this.btnEl.className = "cc-btn";
        this.btnEl.textContent = "CC";
        this.btnEl.title = "Closed Captions";
        this.btnEl.addEventListener("click", (e) => {
            e.stopPropagation();
            this.toggleMenu();
        });
        this.wrapperEl.appendChild(this.btnEl);
        if (playerUI) {
            this.playerUI = playerUI;
            playerUI.addControlRight(this.wrapperEl);
        }
        this._onDocClick = () => {
            if (this.menuOpen) {
                this.closeMenu();
            }
        };
        document.addEventListener("click", this._onDocClick);
    }
    show(caption) {
        const channel = caption.channel;
        if (!this.channels.has(channel)) {
            this.channels.add(channel);
            if (this.activeChannel === 0 && !this.suppressAutoSelect) {
                this.activeChannel = channel;
                this.updateButtonState();
            }
        }
        if (channel !== this.activeChannel) {
            return;
        }
        if (this.hideTimer) {
            clearTimeout(this.hideTimer);
            this.hideTimer = null;
        }
        if (!caption.text && caption.regions.length === 0) {
            this.hide();
            return;
        }
        this.el.innerHTML = "";
        if (caption.regions.length > 0) {
            const is608 = channel >= 1 && channel <= 4;
            if (is608) {
                this.render608(caption.regions);
            }
            else {
                this.render708(caption.regions);
            }
        }
        else {
            this.el.textContent = caption.text;
        }
        this.el.style.display = "block";
        if (this.playerUI)
            this.playerUI.notifyCaptionsVisible(true);
        this.hideTimer = setTimeout(() => {
            this.hide();
        }, 5000);
    }
    hide() {
        this.el.innerHTML = "";
        this.el.style.display = "none";
        if (this.playerUI)
            this.playerUI.notifyCaptionsVisible(false);
        if (this.hideTimer) {
            clearTimeout(this.hideTimer);
            this.hideTimer = null;
        }
    }
    render608(regions) {
        for (const region of regions) {
            for (const row of region.rows) {
                const lineEl = document.createElement("div");
                lineEl.className = "cc-line cc-608-line";
                const safeTop = 5;
                const safeBottom = 5;
                const safeHeight = 100 - safeTop - safeBottom;
                const rowFraction = row.row / (CEA608_ROW_COUNT - 1);
                lineEl.style.position = "absolute";
                lineEl.style.left = "5%";
                lineEl.style.right = "5%";
                if (rowFraction > 0.5) {
                    const bottomPercent = safeBottom + (1 - rowFraction) * safeHeight;
                    lineEl.style.bottom = `${bottomPercent}%`;
                }
                else {
                    const topPercent = safeTop + rowFraction * safeHeight;
                    lineEl.style.top = `${topPercent}%`;
                }
                lineEl.style.textAlign = JUSTIFY_MAP[region.justify] || "center";
                for (const span of row.spans) {
                    lineEl.appendChild(this.createSpanElement(span));
                }
                this.el.appendChild(lineEl);
            }
        }
    }
    render708(regions) {
        const sorted = [...regions].sort((a, b) => a.priority - b.priority);
        for (const region of sorted) {
            const regionEl = document.createElement("div");
            regionEl.className = "cc-region cc-708-region";
            regionEl.style.zIndex = String(region.priority);
            regionEl.style.textAlign = JUSTIFY_MAP[region.justify] || "left";
            this.applyRegionPosition(regionEl, region);
            this.applyRegionFill(regionEl, region);
            for (const row of region.rows) {
                const lineEl = document.createElement("div");
                lineEl.className = "cc-line";
                for (const span of row.spans) {
                    lineEl.appendChild(this.createSpanElement(span));
                }
                regionEl.appendChild(lineEl);
            }
            this.el.appendChild(regionEl);
        }
    }
    applyRegionPosition(el, region) {
        let yPercent;
        let xPercent;
        if (region.relativeToggle) {
            yPercent = region.anchorV;
            xPercent = region.anchorH;
        }
        else {
            yPercent = (region.anchorV / 74) * 100;
            xPercent = (region.anchorH / 209) * 100;
        }
        yPercent = Math.max(0, Math.min(100, yPercent));
        xPercent = Math.max(0, Math.min(100, xPercent));
        const safeMargin = (100 - CEA708_SAFE_AREA_PERCENT) / 2;
        const safeY = safeMargin + (yPercent / 100) * CEA708_SAFE_AREA_PERCENT;
        const safeX = safeMargin + (xPercent / 100) * CEA708_SAFE_AREA_PERCENT;
        const vAnchor = Math.floor(region.anchorID / 3);
        const hAnchor = region.anchorID % 3;
        if (vAnchor === 2) {
            el.style.bottom = `${100 - safeY}%`;
        }
        else if (vAnchor === 1) {
            el.style.top = `${safeY}%`;
            el.style.transform = "translateY(-50%)";
        }
        else {
            el.style.top = `${safeY}%`;
        }
        if (hAnchor === 0) {
            el.style.left = `${safeX}%`;
            el.style.right = `${safeMargin}%`;
        }
        else if (hAnchor === 1) {
            el.style.left = `${safeX}%`;
            el.style.maxWidth = `${CEA708_SAFE_AREA_PERCENT}%`;
            el.style.transform = el.style.transform
                ? `${el.style.transform} translateX(-50%)`
                : "translateX(-50%)";
        }
        else {
            el.style.right = `${100 - safeX}%`;
            el.style.left = `${safeMargin}%`;
        }
    }
    applyRegionFill(el, region) {
        const fillColor = region.fillColor || "000000";
        const opacity = OPACITY_VALUES[region.fillOpacity] ?? 1.0;
        if (opacity > 0) {
            const r = parseInt(fillColor.substring(0, 2), 16);
            const g = parseInt(fillColor.substring(2, 4), 16);
            const b = parseInt(fillColor.substring(4, 6), 16);
            el.style.backgroundColor = `rgba(${r}, ${g}, ${b}, ${opacity})`;
        }
        if (region.borderType > 0 && region.borderColor) {
            const borderFn = BORDER_STYLES[region.borderType];
            if (borderFn) {
                el.style.border = borderFn(region.borderColor);
            }
        }
    }
    createSpanElement(span) {
        const el = document.createElement("span");
        el.textContent = span.text;
        el.style.color = `#${span.fgColor}`;
        const bgOpacity = OPACITY_VALUES[span.bgOpacity] ?? 1.0;
        if (bgOpacity > 0) {
            const r = parseInt(span.bgColor.substring(0, 2), 16);
            const g = parseInt(span.bgColor.substring(2, 4), 16);
            const b = parseInt(span.bgColor.substring(4, 6), 16);
            el.style.backgroundColor = `rgba(${r}, ${g}, ${b}, ${bgOpacity})`;
        }
        const fgOpacity = OPACITY_VALUES[span.fgOpacity] ?? 1.0;
        if (fgOpacity < 1.0) {
            el.style.opacity = String(fgOpacity);
        }
        if (span.italic) {
            el.style.fontStyle = "italic";
        }
        const decorations = [];
        if (span.underline)
            decorations.push("underline");
        if (decorations.length > 0) {
            el.style.textDecoration = decorations.join(" ");
        }
        if (span.flash) {
            el.classList.add("cc-flash");
        }
        if (span.fontTag > 0) {
            el.style.fontFamily = FONT_FAMILIES[span.fontTag] || "monospace";
        }
        if (span.penSize === 0) {
            el.style.fontSize = "0.8em";
        }
        else if (span.penSize === 2) {
            el.style.fontSize = "1.2em";
        }
        if (span.offset === 0) {
            el.style.verticalAlign = "sub";
            el.style.fontSize = "0.7em";
        }
        else if (span.offset === 2) {
            el.style.verticalAlign = "super";
            el.style.fontSize = "0.7em";
        }
        if (span.edgeType > 0 && span.edgeColor) {
            const ec = `#${span.edgeColor}`;
            switch (span.edgeType) {
                case 1:
                    el.style.textShadow = `1px 1px 0 ${ec}, -1px -1px 0 ${ec}`;
                    break;
                case 2:
                    el.style.textShadow = `-1px -1px 0 ${ec}, 1px 1px 0 ${ec}`;
                    break;
                case 3:
                    el.style.textShadow = `0 0 2px ${ec}, 0 0 2px ${ec}`;
                    break;
                case 4:
                    el.style.textShadow = `2px 2px 2px ${ec}`;
                    break;
                case 5:
                    el.style.textShadow = `-2px 2px 2px ${ec}`;
                    break;
            }
        }
        return el;
    }
    getActiveChannel() {
        return this.activeChannel;
    }
    getAvailableChannels() {
        return Array.from(this.channels).sort();
    }
    setChannel(channel) {
        if (channel === 0 || this.channels.has(channel)) {
            this.activeChannel = channel;
            this.updateButtonState();
            if (channel === 0)
                this.hide();
        }
    }
    cycleChannel() {
        const sorted = this.getAvailableChannels();
        if (sorted.length === 0)
            return 0;
        // off -> first -> second -> ... -> off
        if (this.activeChannel === 0) {
            this.setChannel(sorted[0]);
            return sorted[0];
        }
        const idx = sorted.indexOf(this.activeChannel);
        if (idx < 0 || idx >= sorted.length - 1) {
            this.setChannel(0);
            return 0;
        }
        this.setChannel(sorted[idx + 1]);
        return sorted[idx + 1];
    }
    toggleMenu() {
        if (this.menuOpen) {
            this.closeMenu();
        }
        else {
            this.openMenu();
        }
    }
    openMenu() {
        const existing = this.wrapperEl.querySelector(".cc-menu");
        if (existing)
            existing.remove();
        const menu = document.createElement("div");
        menu.className = "cc-menu";
        if (this.playerUI) {
            menu.style.zIndex = String(this.playerUI.getMenuZIndex());
        }
        menu.addEventListener("click", (e) => e.stopPropagation());
        const offItem = document.createElement("div");
        offItem.className = "cc-menu-item" + (this.activeChannel === 0 ? " active" : "");
        offItem.textContent = "Off";
        offItem.addEventListener("click", () => {
            this.activeChannel = 0;
            this.hide();
            this.closeMenu();
            this.updateButtonState();
        });
        menu.appendChild(offItem);
        const sortedChannels = Array.from(this.channels).sort();
        for (const ch of sortedChannels) {
            const item = document.createElement("div");
            item.className = "cc-menu-item" + (this.activeChannel === ch ? " active" : "");
            item.textContent = CHANNEL_NAMES[ch] || `CC${ch}`;
            item.addEventListener("click", () => {
                this.activeChannel = ch;
                this.closeMenu();
                this.updateButtonState();
            });
            menu.appendChild(item);
        }
        this.wrapperEl.insertBefore(menu, this.btnEl);
        this.menuOpen = true;
        if (this.playerUI)
            this.playerUI.notifyMenuOpen();
    }
    closeMenu() {
        const menu = this.wrapperEl.querySelector(".cc-menu");
        if (menu)
            menu.remove();
        this.menuOpen = false;
        if (this.playerUI)
            this.playerUI.notifyMenuClose();
    }
    destroy() {
        document.removeEventListener("click", this._onDocClick);
        this.hide();
        this.closeMenu();
        this.wrapperEl.remove();
    }
    updateButtonState() {
        if (this.activeChannel > 0) {
            this.btnEl.classList.add("cc-active");
            this.btnEl.title = `Captions: ${CHANNEL_NAMES[this.activeChannel] || "CC" + this.activeChannel}`;
        }
        else {
            this.btnEl.classList.remove("cc-active");
            this.btnEl.title = "Closed Captions: Off";
            this.hide();
        }
    }
}
