const IDLE_TIMEOUT_MS = 3000;
const CONTROL_BAR_HEIGHT = 44;
const GRADIENT_HEIGHT = 80;
const Z_VIDEO = 0;
const Z_VU_METER = 1;
const Z_CAPTIONS = 2;
const Z_STATS = 3;
const Z_GRADIENT = 4;
const Z_CONTROLS = 5;
const Z_PANEL = 6;
const Z_MENU = 10;
export class PlayerUI {
    container;
    controlBar;
    controlLeft;
    controlCenter;
    controlRight;
    gradient;
    hudContainer;
    idle = false;
    idleTimer = null;
    menuOpen = false;
    panelOpen = false;
    forceVisible = false;
    onCaptionsChanged = null;
    onPanelToggle = null;
    listeners = [];
    _externallyDriven = false;
    constructor(els) {
        this.container = els.container;
        this.applyContainerStyles();
        this.applyLayerStyles(els.videoCanvas, Z_VIDEO);
        els.videoCanvas.style.position = "relative";
        els.videoCanvas.style.display = "block";
        els.videoCanvas.style.width = "100%";
        els.videoCanvas.style.background = "#111";
        els.videoCanvas.style.borderRadius = "4px";
        this.applyOverlayStyles(els.captionsEl, Z_CAPTIONS);
        els.captionsEl.style.pointerEvents = "none";
        this.applyOverlayStyles(els.vuCanvas, Z_VU_METER);
        els.vuCanvas.style.background = "transparent";
        els.statsEl.style.display = "none";
        this.hudContainer = document.createElement("div");
        this.applyLayerStyles(this.hudContainer, Z_STATS);
        this.hudContainer.style.position = "absolute";
        this.hudContainer.style.top = "8px";
        this.hudContainer.style.left = "8px";
        this.hudContainer.style.right = "8px";
        this.hudContainer.style.pointerEvents = "auto";
        this.container.appendChild(this.hudContainer);
        this.gradient = document.createElement("div");
        this.applyLayerStyles(this.gradient, Z_GRADIENT);
        this.gradient.style.position = "absolute";
        this.gradient.style.left = "0";
        this.gradient.style.right = "0";
        this.gradient.style.bottom = "0";
        this.gradient.style.height = `${GRADIENT_HEIGHT}px`;
        this.gradient.style.background = "linear-gradient(to top, rgba(0,0,0,0.7) 0%, transparent 100%)";
        this.gradient.style.borderRadius = "0 0 4px 4px";
        this.gradient.style.pointerEvents = "none";
        this.gradient.style.transition = "opacity 0.3s ease";
        this.container.appendChild(this.gradient);
        this.controlBar = document.createElement("div");
        this.applyLayerStyles(this.controlBar, Z_CONTROLS);
        this.controlBar.style.position = "absolute";
        this.controlBar.style.left = "0";
        this.controlBar.style.right = "0";
        this.controlBar.style.bottom = "0";
        this.controlBar.style.height = `${CONTROL_BAR_HEIGHT}px`;
        this.controlBar.style.display = "flex";
        this.controlBar.style.alignItems = "center";
        this.controlBar.style.padding = "0 12px";
        this.controlBar.style.gap = "8px";
        this.controlBar.style.transition = "opacity 0.3s ease";
        this.container.appendChild(this.controlBar);
        this.controlLeft = document.createElement("div");
        this.controlLeft.style.display = "flex";
        this.controlLeft.style.alignItems = "center";
        this.controlLeft.style.gap = "6px";
        this.controlBar.appendChild(this.controlLeft);
        this.controlCenter = document.createElement("div");
        this.controlCenter.style.flex = "1";
        this.controlBar.appendChild(this.controlCenter);
        this.controlRight = document.createElement("div");
        this.controlRight.style.display = "flex";
        this.controlRight.style.alignItems = "center";
        this.controlRight.style.gap = "6px";
        this.controlBar.appendChild(this.controlRight);
        this.setupIdleDetection();
    }
    set externallyDriven(v) {
        this._externallyDriven = v;
        if (v) {
            this.controlBar.style.display = "none";
            this.gradient.style.display = "none";
        }
        else {
            this.controlBar.style.display = "flex";
            this.gradient.style.display = "";
        }
    }
    addControlRight(el) {
        this.controlRight.appendChild(el);
    }
    getMenuZIndex() {
        return Z_MENU;
    }
    notifyMenuOpen() {
        this.menuOpen = true;
        this.showControls();
    }
    notifyMenuClose() {
        this.menuOpen = false;
        this.resetIdleTimer();
    }
    setForceVisible(force) {
        this.forceVisible = force;
        if (force) {
            this.showControls();
        }
        else {
            this.resetIdleTimer();
        }
    }
    getControlBarHeight() {
        return CONTROL_BAR_HEIGHT;
    }
    notifyCaptionsVisible(active) {
        if (this.onCaptionsChanged) {
            this.onCaptionsChanged(active);
        }
    }
    setOnCaptionsChanged(cb) {
        this.onCaptionsChanged = cb;
    }
    getHUDContainer() {
        return this.hudContainer;
    }
    setHUDLeftOffset(px) {
        this.hudContainer.style.left = `${px + 8}px`;
    }
    getPanelZIndex() {
        return Z_PANEL;
    }
    getContainer() {
        return this.container;
    }
    notifyPanelOpen(key) {
        this.panelOpen = true;
        this.showControls();
        if (this.onPanelToggle)
            this.onPanelToggle(key);
    }
    notifyPanelClose() {
        this.panelOpen = false;
        this.resetIdleTimer();
        if (this.onPanelToggle)
            this.onPanelToggle(null);
    }
    destroy() {
        for (const cleanup of this.listeners) {
            cleanup();
        }
        this.listeners = [];
        if (this.idleTimer) {
            clearTimeout(this.idleTimer);
            this.idleTimer = null;
        }
    }
    applyContainerStyles() {
        this.container.style.position = "relative";
        this.container.style.overflow = "hidden";
        this.container.style.borderRadius = "4px";
    }
    applyLayerStyles(el, zIndex) {
        el.style.zIndex = String(zIndex);
    }
    applyOverlayStyles(el, zIndex) {
        el.style.position = "absolute";
        el.style.top = "0";
        el.style.left = "0";
        el.style.width = "100%";
        el.style.height = "100%";
        el.style.zIndex = String(zIndex);
    }
    setupIdleDetection() {
        const onActivity = () => {
            if (this.idle) {
                this.showControls();
            }
            this.resetIdleTimer();
        };
        const onLeave = () => {
            if (!this.menuOpen && !this.forceVisible) {
                this.hideControls();
            }
        };
        this.container.addEventListener("mousemove", onActivity);
        this.container.addEventListener("mouseenter", onActivity);
        this.container.addEventListener("mouseleave", onLeave);
        this.container.addEventListener("click", onActivity);
        this.listeners.push(() => this.container.removeEventListener("mousemove", onActivity), () => this.container.removeEventListener("mouseenter", onActivity), () => this.container.removeEventListener("mouseleave", onLeave), () => this.container.removeEventListener("click", onActivity));
        this.resetIdleTimer();
    }
    resetIdleTimer() {
        if (this.idleTimer) {
            clearTimeout(this.idleTimer);
        }
        if (this.menuOpen || this.forceVisible || this.panelOpen)
            return;
        this.idleTimer = setTimeout(() => {
            this.hideControls();
        }, IDLE_TIMEOUT_MS);
    }
    showControls() {
        if (this._externallyDriven)
            return;
        this.idle = false;
        this.controlBar.style.opacity = "1";
        this.gradient.style.opacity = "1";
        this.container.style.cursor = "";
    }
    hideControls() {
        if (this.menuOpen || this.forceVisible || this.panelOpen)
            return;
        this.idle = true;
        this.controlBar.style.opacity = "0";
        this.gradient.style.opacity = "0";
        this.container.style.cursor = "none";
    }
}
