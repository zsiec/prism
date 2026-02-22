/**
 * UI component for selecting between multiple audio tracks in a stream.
 * Renders a dropdown button in the player chrome that lets viewers switch
 * audio languages or commentary tracks.
 */
export class AudioTrackSelector {
    wrapperEl;
    btnEl;
    menuOpen = false;
    activeTrack;
    loudnessMode = false;
    tracks;
    onSelect;
    onLoudness;
    onExitLoudness;
    playerUI;
    _onDocClick;
    constructor(tracks, activeTrack, onSelect, playerUI, onLoudness, onExitLoudness) {
        this.tracks = tracks;
        this.activeTrack = activeTrack;
        this.onSelect = onSelect;
        this.onLoudness = onLoudness ?? null;
        this.onExitLoudness = onExitLoudness ?? null;
        this.playerUI = playerUI ?? null;
        this.wrapperEl = document.createElement("div");
        this.wrapperEl.style.position = "relative";
        this.btnEl = document.createElement("button");
        this.btnEl.className = "audio-btn";
        this.btnEl.textContent = this.buttonLabel();
        this.btnEl.title = "Audio Track";
        this.btnEl.addEventListener("click", (e) => {
            e.stopPropagation();
            this.toggleMenu();
        });
        this.wrapperEl.appendChild(this.btnEl);
        if (playerUI) {
            playerUI.addControlRight(this.wrapperEl);
        }
        this._onDocClick = () => {
            if (this.menuOpen)
                this.closeMenu();
        };
        document.addEventListener("click", this._onDocClick);
    }
    setLoudnessMode(active) {
        this.loudnessMode = active;
        this.btnEl.textContent = this.buttonLabel();
    }
    setActiveTrack(trackIndex) {
        this.activeTrack = trackIndex;
        this.btnEl.textContent = this.buttonLabel();
    }
    buttonLabel() {
        const track = this.tracks.find(t => t.trackIndex === this.activeTrack);
        const trackName = track ? (track.label || "Audio " + (track.trackIndex + 1)) : "Audio";
        if (this.loudnessMode) {
            return `◧ ${trackName}`;
        }
        return `♪ ${trackName}`;
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
        const existing = this.wrapperEl.querySelector(".audio-menu");
        if (existing)
            existing.remove();
        const menu = document.createElement("div");
        menu.className = "audio-menu";
        if (this.playerUI) {
            menu.style.zIndex = String(this.playerUI.getMenuZIndex());
        }
        menu.addEventListener("click", (e) => e.stopPropagation());
        if (this.onLoudness) {
            const loudItem = document.createElement("div");
            loudItem.className = "audio-menu-item audio-menu-loudness" + (this.loudnessMode ? " active" : "");
            loudItem.textContent = "◧ Loudness";
            loudItem.addEventListener("click", () => {
                if (this.loudnessMode) {
                    this.loudnessMode = false;
                    if (this.onExitLoudness)
                        this.onExitLoudness();
                }
                else {
                    this.loudnessMode = true;
                    this.onLoudness();
                }
                this.btnEl.textContent = this.buttonLabel();
                this.closeMenu();
            });
            menu.appendChild(loudItem);
            const sep = document.createElement("div");
            sep.className = "audio-menu-sep";
            menu.appendChild(sep);
        }
        for (const track of this.tracks) {
            const item = document.createElement("div");
            const isActive = this.activeTrack === track.trackIndex;
            item.className = "audio-menu-item" + (isActive ? " active" : "");
            item.textContent = track.label || `Audio ${track.trackIndex + 1}`;
            item.addEventListener("click", () => {
                this.activeTrack = track.trackIndex;
                this.onSelect(track.trackIndex);
                this.btnEl.textContent = this.buttonLabel();
                this.closeMenu();
            });
            menu.appendChild(item);
        }
        this.wrapperEl.insertBefore(menu, this.btnEl);
        this.menuOpen = true;
        if (this.playerUI)
            this.playerUI.notifyMenuOpen();
    }
    closeMenu() {
        const menu = this.wrapperEl.querySelector(".audio-menu");
        if (menu)
            menu.remove();
        this.menuOpen = false;
        if (this.playerUI)
            this.playerUI.notifyMenuClose();
    }
    destroy() {
        document.removeEventListener("click", this._onDocClick);
        if (this.wrapperEl.parentElement) {
            this.wrapperEl.parentElement.removeChild(this.wrapperEl);
        }
        this.menuOpen = false;
    }
}
