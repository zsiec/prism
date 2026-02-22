/** Minimum displayed decibel level, representing silence. */
export const DB_MIN = -60;

/** Maximum displayed decibel level, representing clipping. */
export const DB_MAX = 6;

/** Total dB range used for normalization. */
export const DB_RANGE = DB_MAX - DB_MIN;

/** Converts a linear audio level (0..1+) to decibels, clamped to [DB_MIN, DB_MAX]. */
export function linearToDb(level: number): number {
	if (level <= 0.00001) return DB_MIN;
	const db = 20 * Math.log10(level);
	return Math.max(DB_MIN, Math.min(DB_MAX, db));
}

/** Converts a dB value to a 0..1 fraction within the displayed range. */
export function dbToFraction(db: number): number {
	return Math.max(0, Math.min(1, (db - DB_MIN) / DB_RANGE));
}
