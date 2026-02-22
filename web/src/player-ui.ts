import type { BadgeKey } from "./hud";

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

interface PlayerUIElements {
	container: HTMLElement;
	videoCanvas: HTMLCanvasElement;
	vuCanvas: HTMLCanvasElement;
	captionsEl: HTMLElement;
	statsEl: HTMLElement;
}

export class PlayerUI {
	private container: HTMLElement;
	private controlBar: HTMLElement;
	private controlLeft: HTMLElement;
	private controlCenter: HTMLElement;
	private controlRight: HTMLElement;
	private gradient: HTMLElement;

	private hudContainer: HTMLElement;

	private idle = false;
	private idleTimer: ReturnType<typeof setTimeout> | null = null;
	private menuOpen = false;
	private panelOpen = false;
	private forceVisible = false;
	private onCaptionsChanged: ((active: boolean) => void) | null = null;
	private onPanelToggle: ((key: BadgeKey | null) => void) | null = null;
	private listeners: (() => void)[] = [];
	private _externallyDriven = false;

	constructor(els: PlayerUIElements) {
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

	set externallyDriven(v: boolean) {
		this._externallyDriven = v;
		if (v) {
			this.controlBar.style.display = "none";
			this.gradient.style.display = "none";
		} else {
			this.controlBar.style.display = "flex";
			this.gradient.style.display = "";
		}
	}

	addControlRight(el: HTMLElement): void {
		this.controlRight.appendChild(el);
	}

	getMenuZIndex(): number {
		return Z_MENU;
	}

	notifyMenuOpen(): void {
		this.menuOpen = true;
		this.showControls();
	}

	notifyMenuClose(): void {
		this.menuOpen = false;
		this.resetIdleTimer();
	}

	setForceVisible(force: boolean): void {
		this.forceVisible = force;
		if (force) {
			this.showControls();
		} else {
			this.resetIdleTimer();
		}
	}

	getControlBarHeight(): number {
		return CONTROL_BAR_HEIGHT;
	}

	notifyCaptionsVisible(active: boolean): void {
		if (this.onCaptionsChanged) {
			this.onCaptionsChanged(active);
		}
	}

	setOnCaptionsChanged(cb: (active: boolean) => void): void {
		this.onCaptionsChanged = cb;
	}

	getHUDContainer(): HTMLElement {
		return this.hudContainer;
	}

	setHUDLeftOffset(px: number): void {
		this.hudContainer.style.left = `${px + 8}px`;
	}

	getPanelZIndex(): number {
		return Z_PANEL;
	}

	getContainer(): HTMLElement {
		return this.container;
	}

	notifyPanelOpen(key: BadgeKey): void {
		this.panelOpen = true;
		this.showControls();
		if (this.onPanelToggle) this.onPanelToggle(key);
	}

	notifyPanelClose(): void {
		this.panelOpen = false;
		this.resetIdleTimer();
		if (this.onPanelToggle) this.onPanelToggle(null);
	}

	destroy(): void {
		for (const cleanup of this.listeners) {
			cleanup();
		}
		this.listeners = [];
		if (this.idleTimer) {
			clearTimeout(this.idleTimer);
			this.idleTimer = null;
		}
	}

	private applyContainerStyles(): void {
		this.container.style.position = "relative";
		this.container.style.overflow = "hidden";
		this.container.style.borderRadius = "4px";
	}

	private applyLayerStyles(el: HTMLElement | HTMLCanvasElement, zIndex: number): void {
		el.style.zIndex = String(zIndex);
	}

	private applyOverlayStyles(el: HTMLElement | HTMLCanvasElement, zIndex: number): void {
		el.style.position = "absolute";
		el.style.top = "0";
		el.style.left = "0";
		el.style.width = "100%";
		el.style.height = "100%";
		el.style.zIndex = String(zIndex);
	}

	private setupIdleDetection(): void {
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

		this.listeners.push(
			() => this.container.removeEventListener("mousemove", onActivity),
			() => this.container.removeEventListener("mouseenter", onActivity),
			() => this.container.removeEventListener("mouseleave", onLeave),
			() => this.container.removeEventListener("click", onActivity),
		);

		this.resetIdleTimer();
	}

	private resetIdleTimer(): void {
		if (this.idleTimer) {
			clearTimeout(this.idleTimer);
		}
		if (this.menuOpen || this.forceVisible || this.panelOpen) return;

		this.idleTimer = setTimeout(() => {
			this.hideControls();
		}, IDLE_TIMEOUT_MS);
	}

	private showControls(): void {
		if (this._externallyDriven) return;
		this.idle = false;
		this.controlBar.style.opacity = "1";
		this.gradient.style.opacity = "1";
		this.container.style.cursor = "";
	}

	private hideControls(): void {
		if (this.menuOpen || this.forceVisible || this.panelOpen) return;
		this.idle = true;
		this.controlBar.style.opacity = "0";
		this.gradient.style.opacity = "0";
		this.container.style.cursor = "none";
	}
}
