import type { PlayerUI } from "./player-ui";

/**
 * Fullscreen toggle button for the single-stream player control bar.
 * Uses the Fullscreen API on the player container so the video, HUD,
 * captions, and control bar all fill the screen.
 */
export class FullscreenButton {
	private btnEl: HTMLButtonElement;
	private container: HTMLElement;
	private _onFSChange: () => void;

	constructor(playerUI: PlayerUI) {
		this.container = playerUI.getContainer();

		this.btnEl = document.createElement("button");
		this.btnEl.className = "cc-btn";
		this.btnEl.title = "Fullscreen";
		this.btnEl.textContent = "⛶";
		this.btnEl.style.fontSize = "16px";
		this.btnEl.style.lineHeight = "1";
		this.btnEl.addEventListener("click", (e) => {
			e.stopPropagation();
			this.toggle();
		});

		playerUI.addControlRight(this.btnEl);

		this._onFSChange = () => this.updateIcon();
		document.addEventListener("fullscreenchange", this._onFSChange);
	}

	private toggle(): void {
		if (document.fullscreenElement === this.container) {
			document.exitFullscreen();
		} else {
			this.container.requestFullscreen();
		}
	}

	private updateIcon(): void {
		const isFS = document.fullscreenElement === this.container;
		this.btnEl.textContent = isFS ? "⛶" : "⛶";
		this.btnEl.title = isFS ? "Exit Fullscreen" : "Fullscreen";
		if (isFS) {
			this.btnEl.style.color = "#fff";
			this.btnEl.style.borderColor = "#fff";
		} else {
			this.btnEl.style.color = "";
			this.btnEl.style.borderColor = "";
		}
	}

	destroy(): void {
		document.removeEventListener("fullscreenchange", this._onFSChange);
		if (this.btnEl.parentElement) {
			this.btnEl.parentElement.removeChild(this.btnEl);
		}
	}
}
