/**
 * Stream Inspector orchestrator. Creates and manages the metrics strip
 * and full dashboard overlay. Handles the 'D' keyboard shortcut.
 */

import type { MetricsStore } from "./metrics-store";
import { InspectorStrip } from "./inspector-strip";
import { InspectorDashboard } from "./inspector-dashboard";

export class StreamInspector {
	private strip: InspectorStrip;
	private dashboard: InspectorDashboard;
	private keyHandler: (e: KeyboardEvent) => void;
	private active = false;

	constructor(mount: HTMLElement, store: MetricsStore) {
		this.strip = new InspectorStrip(mount, store);
		this.dashboard = new InspectorDashboard(mount, store);

		this.strip.setOnToggleDashboard(() => this.toggleDashboard());
		this.dashboard.setOnClose(() => this.toggleDashboard());

		this.keyHandler = (e: KeyboardEvent) => {
			if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
			if (e.key === "d" || e.key === "D") {
				if (!this.active) return;
				this.toggleDashboard();
			}
		};
		document.addEventListener("keydown", this.keyHandler);
	}

	show(): void {
		this.active = true;
		this.strip.show();
	}

	hide(): void {
		this.active = false;
		this.strip.hide();
		if (this.dashboard.isVisible()) {
			this.dashboard.hide();
			this.strip.setDashboardOpen(false);
		}
	}

	toggleDashboard(): void {
		if (this.dashboard.isVisible()) {
			this.dashboard.hide();
			this.strip.setDashboardOpen(false);
		} else {
			this.dashboard.show();
			this.strip.setDashboardOpen(true);
		}
	}

	destroy(): void {
		document.removeEventListener("keydown", this.keyHandler);
		this.strip.destroy();
		this.dashboard.destroy();
	}
}
