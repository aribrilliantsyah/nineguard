// Quota Reset Schedule helpers (ADR 0010). Pure functions only, no DOM: the key modal
// (views/keyquota.js) and the Quotas page dialog (views/quotas.js) share them, and the
// rules mirror the server's validation (traffic.ResetSchedule.Validate).

export const WEEKDAYS = Object.freeze([
  [1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [7, 'Sunday'],
]);

const PERIOD_ORDER = ['daily', 'weekly', 'monthly'];
const weekdayName = (n) => (WEEKDAYS.find(([k]) => k === Number(n)) || WEEKDAYS[0])[1];

// Quota Periods of the quotas a key currently enforces. In model mode only the Model Quota
// rows count; in key mode only the Key Quota period. 'none' means no quota. Resetting periods
// come first in a fixed order, then 'total'.
export function usedPeriods({ mode, keyPeriod, rows = [] }) {
  const raw = mode === 'model' ? rows.map((r) => r.period) : [keyPeriod];
  const set = new Set(raw.filter((p) => p && p !== 'none'));
  return [...PERIOD_ORDER.filter((p) => set.has(p)), ...(set.has('total') ? ['total'] : [])];
}

// Which schedule inputs the periods in use need. Nothing resets for none/total, so the whole
// block stays hidden then.
export function neededFields(periods) {
  const set = new Set(periods);
  const show = PERIOD_ORDER.some((p) => set.has(p));
  return { show, time: show, weekday: set.has('weekly'), day: set.has('monthly') };
}

// A key with no schedule of its own follows the global one.
export const scheduleChoice = (own) => (own ? 'custom' : 'follow');

// Body value for quota_reset_schedule: null = follow global; an object = custom (a UTC
// schedule is an object too, it pins the key to UTC).
export function buildSchedulePayload(choice, s) {
  if (choice !== 'custom') return null;
  return { timezone: String(s.timezone || '').trim(), time: String(s.time || ''), weekday: Number(s.weekday), day: Number(s.day) };
}

// First problem with a custom schedule, or '' when fine. Mirrors the server.
export function validateSchedule(s) {
  const tz = String(s.timezone || '').trim();
  if (!tz || tz === 'Local' || tz.length > 64) return 'Reset schedule: choose a timezone.';
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(String(s.time || ''))) return 'Reset schedule: time must be HH:MM (00:00 to 23:59).';
  const wd = Number(s.weekday);
  if (!Number.isInteger(wd) || wd < 1 || wd > 7) return 'Reset schedule: weekday must be Monday to Sunday.';
  const day = Number(s.day);
  if (!Number.isInteger(day) || day < 1 || day > 31) return 'Reset schedule: day of month must be 1 to 31.';
  return '';
}

// Compact text for a schedule, showing only the fields the periods in use need.
export function describeSchedule(s, periods) {
  const sched = s || { timezone: 'UTC', time: '00:00', weekday: 1, day: 1 };
  const need = neededFields(periods);
  const parts = [];
  if (need.weekday) parts.push(weekdayName(sched.weekday));
  if (need.day) parts.push(`day ${sched.day}`);
  const when = parts.length === 0 ? sched.time : `${parts.join(', ')}${need.day ? ' at' : ''} ${sched.time}`.replace(', at', ' at');
  return `${sched.timezone}, ${when}`;
}

// Where an effective schedule comes from, as a label.
export function scheduleSourceLabel(source, effective) {
  if (source === 'key') return 'Custom schedule';
  if (source === 'global') return `Global schedule: ${effective ? `${effective.timezone}, ${effective.time}` : 'set'}`;
  return 'UTC default (no global schedule set)';
}

// Starting values for a new custom schedule: the global one if there is one, otherwise the
// viewer's timezone at midnight on Monday / the 1st. Always a copy.
export function defaultSchedule(global, viewerTz) {
  if (global) return { timezone: global.timezone, time: global.time, weekday: global.weekday, day: global.day };
  return { timezone: viewerTz || 'UTC', time: '00:00', weekday: 1, day: 1 };
}

// An absolute instant as "Mon, 02 Nov 2026 14:00 WIB" in the given zone ('' when unusable).
// The date is English; the zone name uses the id-ID locale like the rest of the UI (ui.js
// tzLabel), which gives WIB for Jakarta, the same label the server puts in its 429 message.
export function nextResetText(iso, timezone) {
  const ms = Date.parse(iso);
  if (!iso || Number.isNaN(ms)) return '';
  const d = new Date(ms);
  const fmt = (tz) => {
    const parts = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
      timeZone: tz, weekday: 'short', day: '2-digit', month: 'short', year: 'numeric',
      hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
    }).formatToParts(d).map((p) => [p.type, p.value]));
    const zone = new Intl.DateTimeFormat('id-ID', { timeZone: tz, timeZoneName: 'short' })
      .formatToParts(d).find((p) => p.type === 'timeZoneName').value;
    return `${parts.weekday}, ${parts.day} ${parts.month} ${parts.year} ${parts.hour}:${parts.minute} ${zone}`;
  };
  try {
    return fmt(timezone || 'UTC');
  } catch {
    return fmt('UTC');
  }
}
